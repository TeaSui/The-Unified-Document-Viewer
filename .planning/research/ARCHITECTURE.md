# Architecture Research

**Domain:** Aggregator / Backend-for-Frontend API (parallel upstream fan-out, normalization, partial-failure tolerance, audit + cache, rich observability)
**Researched:** 2026-05-03
**Confidence:** HIGH

## Standard Architecture

### System Overview

```
┌──────────────────────────────────────────────────────────────────────┐
│                          Client / Test Harness                        │
│                   (cURL, OpenAPI-driven, integration tests)           │
└────────────────────────────────┬─────────────────────────────────────┘
                                 │  HTTP (JSON, OpenAPI-described)
┌────────────────────────────────▼─────────────────────────────────────┐
│                         Edge / HTTP Server                            │
│    (router, middleware: request-id, auth-stub, rate-limit, CORS)      │
├──────────────────────────────────────────────────────────────────────┤
│                           Request Handler                             │
│        (DTO validation → VIN validation → call aggregation)           │
├──────────────────────────────────────────────────────────────────────┤
│                        Aggregation Service                            │
│   ┌────────────────────────────────────────────────────────────┐     │
│   │  parallel fan-out  │  overall deadline  │  merge + sort    │     │
│   │  per-source policy │  tag source        │  build envelope  │     │
│   └────────────────────────────────────────────────────────────┘     │
├───────────────┬─────────────────────────┬────────────────────────────┤
│  Upstream     │  Upstream               │  Repository / Cache        │
│  Client:      │  Client:                │  Layer                     │
│  Sales API    │  Service API            │  (audit + cached_response) │
│  (timeout,    │  (timeout,              │  (idempotent writes,       │
│   breaker,    │   breaker,              │   stale-on-failure reads)  │
│   retry,      │   retry,                │                            │
│   bulkhead)   │   bulkhead)             │                            │
├───────────────┴─────────────────────────┴────────────────────────────┤
│                      Observability Layer                              │
│   OTel Tracer │ Metrics (Prometheus-style) │ Structured Logger        │
├──────────────────────────────────────────────────────────────────────┤
│                            Persistence                                │
│     ┌──────────────────┐          ┌─────────────────────────┐         │
│     │ audit_request    │          │ cached_response         │         │
│     │ (id, vin, ts,    │          │ (vin, source, payload,  │         │
│     │  per-source      │          │  fetched_at, ttl)       │         │
│     │  outcomes)       │          │                         │         │
│     └──────────────────┘          └─────────────────────────┘         │
└──────────────────────────────────────────────────────────────────────┘

           ┌────────────── Test / Dev Only ──────────────┐
           │   Mock Sales API        Mock Service API    │
           │   (configurable latency, failure injection) │
           └─────────────────────────────────────────────┘
```

### Component Responsibilities

| Component | Responsibility | Typical Implementation |
|-----------|----------------|------------------------|
| HTTP Server / Router | TLS termination (in prod), routing, middleware chain, request-id injection | Fastify / Express (Node), chi / echo (Go), Spring Boot MVC (Java), FastAPI (Python) |
| Request Handler | Parse + validate DTO, enforce VIN format (ISO 3779), map to service call, map result to HTTP envelope | Thin controller; no business logic |
| Aggregation Service | Orchestrate fan-out, apply overall deadline, merge + normalize + sort documents, attach source status, trigger audit persistence | Pure domain service; no HTTP or DB libs directly imported |
| Upstream Client (per source) | Typed client for one upstream (Sales, Service); applies per-call timeout, circuit breaker, retry-with-jitter, bulkhead semaphore, tracing span | One class/struct per upstream, sharing a `HttpExecutor` with resiliency policies |
| Repository / Cache Layer | Audit writes (append-only), optional cached_response reads/writes, stale-on-failure lookup | Repository pattern; one concrete impl per store (Postgres / SQLite for take-home) |
| Persistent DB | Durable audit trail + short-TTL cached responses | Postgres (prod), SQLite (take-home local) — both satisfy "persistent DB" |
| Observability Layer | Trace context propagation, metric emission, structured JSON logs with correlation ids | OpenTelemetry SDK + OTLP exporter; Prometheus scrape for metrics; JSON logger (pino / zap / slf4j+logback / structlog) |
| Mock Upstreams | Standalone HTTP services returning representative payloads; support configurable latency/error injection | Separate tiny service or same codebase behind a feature flag; run via Docker Compose |

## Recommended Project Structure

```
src/
├── api/                  # HTTP edge — routing, middleware, OpenAPI
│   ├── server.ts         # bootstrap + middleware chain
│   ├── routes/
│   │   └── documents.ts  # GET /vehicles/{vin}/documents
│   ├── middleware/       # request-id, logging, error-mapper
│   └── openapi.yaml      # source of truth for contract
├── handlers/             # thin controllers
│   └── documents.handler.ts
├── services/             # business logic (testable without HTTP/DB)
│   ├── aggregation.service.ts
│   └── vin.validator.ts
├── upstream/             # one folder per upstream
│   ├── sales/
│   │   ├── sales.client.ts
│   │   └── sales.types.ts
│   ├── service/
│   │   ├── service.client.ts
│   │   └── service.types.ts
│   └── shared/
│       ├── http.executor.ts       # timeout + retry + breaker + bulkhead
│       └── resiliency.policies.ts
├── repository/           # persistence boundary
│   ├── audit.repository.ts
│   ├── cache.repository.ts
│   └── migrations/
├── observability/        # wiring only; no business logic
│   ├── tracer.ts
│   ├── metrics.ts
│   └── logger.ts
├── config/               # env-driven config; zero hardcoded values
│   └── config.ts
└── domain/               # shared types (Document, SourceStatus, Envelope)
    └── models.ts

mocks/
├── sales-mock/           # standalone mock upstream
└── service-mock/

tests/
├── unit/
├── integration/          # spins real DB + mocks in-proc / testcontainers
├── contract/             # validates responses against openapi.yaml
└── load/                 # k6 / autocannon scripts

docker-compose.yml        # app + mocks + db + (optional) otel-collector
```

### Structure Rationale

- **`api/` vs `handlers/` vs `services/`:** Classic handler → service → client layering. The handler is HTTP-aware; the service is not. This makes aggregation logic unit-testable without spinning the HTTP server.
- **`upstream/<source>/`:** One folder per upstream. Each upstream has its own types, client, and resiliency config. Adding a third source (e.g., Finance) is a new sibling folder — no modification of existing ones (open-closed).
- **`upstream/shared/http.executor.ts`:** A single shared executor composes timeout + retry + breaker + bulkhead so every client gets identical resiliency semantics. Policies differ by *config*, not by *code*.
- **`repository/`:** Repository pattern isolates SQL. Swapping SQLite → Postgres is a config change.
- **`observability/`:** Wiring lives in one place; business code takes a logger/tracer via DI and never imports vendor SDKs directly.
- **`mocks/` as siblings, not in-process:** Exercises real network timeouts and real failure modes. Worth the extra `docker compose up` cost.
- **`tests/contract/`:** OpenAPI is the contract — contract tests guard against drift between spec and implementation.

## Architectural Patterns

### Pattern 1: Scatter-Gather with `allSettled` semantics

**What:** Fan out N independent upstream calls in parallel, wait for all of them with an overall deadline, and build a response that reports per-source status. Never fail-fast on a single upstream error.
**When to use:** Read-side aggregation where partial results are more valuable than no results (this project's Core Value).
**Trade-offs:**
- (+) Maximum information returned to the caller.
- (+) Latency = max(sources), not sum(sources).
- (−) Response envelope becomes richer (clients must read `sources[]`).
- (−) Makes "did this succeed?" a multi-valued question — mitigated by the envelope.

**Example (Node, chosen concurrency primitive):**
```typescript
const results = await Promise.allSettled([
  salesClient.listDocuments(vin, ctx),    // each has own timeout + breaker
  serviceClient.listDocuments(vin, ctx),
]);
const sources = results.map((r, i) => toSourceStatus(r, NAMES[i]));
const documents = results
  .flatMap((r, i) => r.status === "fulfilled" ? tag(r.value, NAMES[i]) : [])
  .sort(byDateDesc);
return { documents, sources };
```

### Pattern 2: Per-source resiliency policies composed in a shared executor

**What:** Every upstream call goes through a pipeline: `bulkhead → breaker → retry(jitter) → timeout → http`. Each stage is configurable per source via config, not code.
**When to use:** Any system calling 2+ unreliable upstreams. Essential here.
**Trade-offs:**
- (+) Uniform behavior and metrics shape across sources.
- (+) Breakers prevent one slow upstream from exhausting the service.
- (−) More moving parts to explain in the SDD — worth it.

**Example (pseudocode, policy layering):**
```typescript
const executor = httpExecutor({
  timeout: 800,               // per attempt
  retries: { max: 2, jitter: "full", retryOn: [502, 503, 504, "ETIMEDOUT"] },
  breaker: { failureThreshold: 0.5, windowMs: 10_000, cooldownMs: 15_000 },
  bulkhead: { maxConcurrent: 16, maxQueued: 32 },
});
```

### Pattern 3: Stale-on-failure read-through cache

**What:** On a successful upstream fetch, persist `(vin, source, payload, fetched_at)`. On a subsequent failure, if a cached row exists and is within a stale-tolerance window, serve it with `source.status = "stale"` instead of `"failed"`.
**When to use:** Read aggregators where stale is strictly better than nothing for end-user workflow. Fits the dealership use case (staff still want to see *something*).
**Trade-offs:**
- (+) Elevates graceful-degradation story from "return failure" to "return last-known-good".
- (+) Gives the persistent DB a genuine reliability role (not just audit).
- (−) Cache invalidation discipline required (document TTL clearly).

### Pattern 4: Envelope response with per-source status

**What:** Single top-level response carries both data and operational status.
**Example contract:**
```json
{
  "data": {
    "vin": "1HGCM82633A004352",
    "documents": [ { "id": "...", "source": "sales",   "type": "...", "date": "..." } ],
    "sources":   [ { "name": "sales", "status": "ok" },
                   { "name": "service", "status": "timeout", "error": "upstream exceeded 800ms" } ]
  },
  "meta": { "request_id": "...", "timestamp": "2026-05-03T..." }
}
```

## Data Flow

### Request Flow — `GET /vehicles/{vin}/documents`

```
Client
  │  GET /vehicles/{vin}/documents
  ▼
Router / Middleware
  │  attach request_id, start root trace span, start request timer
  ▼
Handler
  │  validate VIN (length 17, charset, check digit optional)
  │  build AggregationContext{ vin, request_id, deadline = now + 1500ms }
  ▼
Aggregation Service
  │  fan-out (parallel, not fail-fast):
  │     ├── SalesClient.list(vin, ctx)       [child span, per-call timeout 800ms, breaker, retry, bulkhead]
  │     └── ServiceClient.list(vin, ctx)     [child span, same policies, own breaker]
  │  await all with overall deadline (Promise.allSettled + race-against-deadline)
  │  for each result:
  │     fulfilled → tag source, normalize to Document shape
  │     rejected  → map to SourceStatus{ ok|failed|timeout|stale, error }
  │     rejected + cache hit within stale window → serve cached, status="stale"
  │  merge documents, sort by date desc, stable-tiebreak on (source, id)
  │  build envelope { data: { vin, documents, sources }, meta }
  ▼
Repository (async, non-blocking of response if possible)
  │  insert audit_request(request_id, vin, ts, outcomes[])
  │  upsert cached_response rows for each fulfilled source
  ▼
Handler → HTTP 200 + JSON envelope
  │  end root span, record metrics:
  │    http_request_duration_seconds{route,status}
  │    upstream_request_duration_seconds{source,outcome}
  │    upstream_circuit_state{source}
  │  structured log { level, request_id, vin, duration_ms, per_source_outcomes }
  ▼
Client
```

### Partial-Failure Contract — Decision

Three options considered:

| Option | Status code | Body shape | Verdict |
|--------|-------------|------------|---------|
| `200 OK` + `sources[]` in envelope | 200 | `{ data: { documents, sources }, meta }` | **Recommended** |
| `207 Multi-Status` | 207 | Per-resource status in body | Designed for WebDAV; clients + toolchains rarely handle it well |
| `problem+json` per RFC 7807 on any failure | 4xx/5xx | `{ type, title, status, detail }` | Loses the partial success — wrong shape for "1 of 2 succeeded" |

**Recommendation:** `200 OK` with a `sources[]` array. Use `problem+json` **only** for total failures where *no* upstream produced usable data (both failed and no cache) — return `502 Bad Gateway` + problem+json. This gives clients one clear predicate: `status === 200 ⇒ read documents + sources`, `status >= 400 ⇒ read problem+json`. Rationale: aligns with the Core Value ("return what succeeded"), is ergonomic for cURL / browser consumers, and keeps the envelope grepable in logs.

### Key Data Flows

1. **Happy path:** both sources return in < 500ms → envelope has 2 `ok` statuses, documents sorted across both.
2. **One-source timeout:** Service times out at 800ms → Sales documents returned, `service.status="timeout"`, 200 OK, audit row records mixed outcome.
3. **One-source breaker open:** Service breaker in OPEN state → call short-circuits in < 1ms, `service.status="failed"`, error="circuit_open", returns Sales data immediately.
4. **Both fail + cache hit:** serve cached payload with `status="stale"` and `fetched_at` in source entry; 200 OK.
5. **Both fail + no cache:** 502 + problem+json.

## Scaling Considerations

| Scale | Architecture Adjustments |
|-------|--------------------------|
| Take-home / 0–10 rps | Single process, SQLite, in-proc mocks orchestrated via Docker Compose. This is the target for the submission. |
| 10–1,000 rps | Postgres with connection pool (pgbouncer), Redis cache in front of `cached_response`, horizontal scale of stateless service behind an LB, shared breaker state optional |
| 1,000+ rps | Split upstream clients into sidecars or a dedicated aggregation tier; move audit writes to an async queue (Kafka) to decouple from request path; regional read replicas |

### Scaling Priorities

1. **First bottleneck:** upstream latency variance. Fix: tighter per-source timeouts, aggressive bulkheads, and stale-on-failure cache hit rate targets.
2. **Second bottleneck:** audit write amplification. Fix: batch audit inserts or push to an append-only log (Kafka) consumed by a writer.
3. **Third bottleneck:** DB connection saturation under breaker-trip scenarios (retry storms hitting the audit table). Fix: audit write is best-effort + bounded queue; never blocks the response.

## Concurrency Model by Language

| Language | Primitive | Notes |
|----------|-----------|-------|
| Go | `errgroup.Group` with `context.WithTimeout` per call; collect errors without cancelling siblings | Use `WaitGroup` + channel if you explicitly want "don't cancel on first error"; errgroup cancels siblings on error unless you swallow the error locally |
| Java 21+ | Virtual threads + `StructuredTaskScope.ShutdownOnFailure`-avoiding variant; or `CompletableFuture.allOf` with individual exception handlers | Prefer `StructuredTaskScope` with custom policy that collects all outcomes |
| Node.js | `Promise.allSettled` | Native, zero-dep, exactly matches "don't fail-fast" |
| Python | `asyncio.gather(*, return_exceptions=True)` | The `return_exceptions=True` flag is the `allSettled` equivalent |

**Recommendation:** Use the `Promise.allSettled` equivalent in whichever language is chosen. Fail-fast (`Promise.all`, plain `errgroup`, `asyncio.gather` default) is wrong for this domain — a single upstream failure would discard the other upstream's successful response.

## Resiliency Design

| Concern | Policy | Value (starting point) |
|---------|--------|------------------------|
| Per-source timeout | Cancel that single upstream call | 800 ms |
| Overall request deadline | Hard cap on aggregator response | 1500 ms |
| Circuit breaker (per source) | Open on rolling-window failure rate | threshold 50%, window 10s, cooldown 15s, half-open probes=1 |
| Retry | Exponential backoff with full jitter, idempotent GET only | max 2 retries, base 50ms, cap 400ms |
| Bulkhead | Semaphore per source | 16 concurrent, 32 queued, reject-fast beyond |
| Shed-load | Reject with 503 when overall in-flight > cap | cap 128 (tune by load test) |

Key invariant: **per-source timeout < overall deadline − (retries × backoff_cap)**. Math check: 800 + 2×400 = 1600ms, which already exceeds 1500 — so either cap retries at 1, or lower per-call timeout to 600ms. Document this in the SDD.

## Persistence Model

```sql
-- audit_request: append-only, one row per inbound request
CREATE TABLE audit_request (
  request_id   UUID PRIMARY KEY,
  vin          TEXT NOT NULL,
  ts           TIMESTAMPTZ NOT NULL DEFAULT now(),
  http_status  INT NOT NULL,
  duration_ms  INT NOT NULL,
  outcomes     JSONB NOT NULL   -- [{source, status, latency_ms, error?}]
);
CREATE INDEX idx_audit_vin_ts ON audit_request(vin, ts DESC);

-- cached_response: last-known-good per (vin, source)
CREATE TABLE cached_response (
  vin          TEXT NOT NULL,
  source       TEXT NOT NULL,    -- 'sales' | 'service'
  payload      JSONB NOT NULL,
  fetched_at   TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (vin, source)
);
CREATE INDEX idx_cached_fetched ON cached_response(fetched_at);
```

- `audit_request` satisfies the "persistent DB" brief in a way that genuinely serves operations (debugging, SLO reports).
- `cached_response` turns the DB into a resiliency asset (stale-on-failure).
- No document-body storage — aligns with out-of-scope "document rendering".

## Observability Wiring

- **Tracing:** OpenTelemetry. Root span `documents.aggregate` is started in middleware. Each upstream call is a child span named `upstream.<source>.list` with attributes `{vin (hashed or masked), source, attempt, outcome, http.status_code}`. Export via OTLP to a local collector in Docker Compose; viewable in Jaeger for the submission.
- **Metrics (Prometheus-style):**
  - `http_request_duration_seconds{route,method,status}` (histogram)
  - `upstream_request_duration_seconds{source,outcome}` (histogram; outcome ∈ ok|failed|timeout|short_circuited|stale)
  - `upstream_circuit_state{source}` (gauge: 0 closed, 1 half-open, 2 open)
  - `aggregation_partial_failures_total{failed_source}` (counter)
  - `cache_hit_total{source,freshness}` (counter)
- **Structured logs (JSON):** every line carries `request_id`, `vin` (masked to last 6 chars for PII safety), `trace_id`, `span_id`. One log per inbound request summarising per-source outcomes; upstream clients log only on error or retry.
- **Correlation:** `request_id` is generated in middleware, injected into `AggregationContext`, included in all child logs, echoed in response header `x-request-id`, and stored in `audit_request.request_id`.

## Anti-Patterns

### Anti-Pattern 1: Fail-fast fan-out with `Promise.all` / `errgroup` default

**What people do:** Use the default "abort on first error" parallel primitive and propagate the first upstream failure as the HTTP response.
**Why it's wrong:** Discards the successful upstream's documents, violating the Core Value of partial success. A 1-in-100 flake on one source causes 100% user-visible failures.
**Do this instead:** Use `allSettled` semantics and build a per-source status array. Map total-failure (every source failed with no cache) to 502, everything else to 200.

### Anti-Pattern 2: One big try/catch swallowing everything

**What people do:** Wrap the whole handler in a single try/catch and return a generic "something went wrong".
**Why it's wrong:** Hides which source failed, makes the breaker blind, and produces useless audit rows.
**Do this instead:** Try/catch at boundaries only — inside each upstream client to classify the error (`timeout | 5xx | breaker_open | network | deserialization`) and emit the right metric. The aggregator never sees raw exceptions; it sees typed `SourceResult`.

### Anti-Pattern 3: Shared mutable breaker state across unrelated upstreams

**What people do:** One global breaker for "upstream calls".
**Why it's wrong:** One bad source trips the breaker for the healthy source. Opposite of the bulkhead goal.
**Do this instead:** One breaker *and* one bulkhead instance *per source*. Upstreams are isolated failure domains.

### Anti-Pattern 4: Persisting audit synchronously on the request path with strict consistency

**What people do:** `INSERT INTO audit_request` inline before returning the response; let errors bubble up.
**Why it's wrong:** DB hiccup turns a successful aggregation into a failed response. Couples availability of the read API to the audit DB.
**Do this instead:** Audit write is best-effort (logged on failure, never fails the response). For the take-home, synchronous is acceptable if clearly wrapped as best-effort; call it out in the SDD.

### Anti-Pattern 5: Mocks as in-process fakes

**What people do:** Swap the HTTP client for a hand-rolled fake in tests and "mocks".
**Why it's wrong:** Never exercises real timeouts, real TCP behavior, real serialization, or real observability wiring end-to-end. The demo looks impressive but the resiliency story is unverified.
**Do this instead:** Mocks are **standalone HTTP services** with a toggle for latency and failure injection. Run them in Docker Compose. Keep in-process fakes only for pure unit tests.

## Integration Points

### External Services (Mocked in this project)

| Service | Integration Pattern | Notes |
|---------|---------------------|-------|
| Sales System API | Typed HTTP client, OpenAPI-described, policy-wrapped | Assume REST/JSON; document pagination assumption |
| Service System API | Typed HTTP client, OpenAPI-described, policy-wrapped | Assume higher latency variance — lower timeout if the mock simulates it |

### Internal Boundaries

| Boundary | Communication | Notes |
|----------|---------------|-------|
| Handler ↔ Aggregation Service | Direct method call (DI) | Handler passes a `Context` (request_id, deadline, tracer) |
| Aggregation Service ↔ Upstream Clients | Interface (port), concrete per source | Enables swapping mocks in integration tests |
| Aggregation Service ↔ Repository | Interface | Stub in unit tests, real DB in integration tests |
| Upstream Client ↔ HTTP Executor | Policy pipeline | Shared executor; per-source config injected |
| Any code ↔ Observability | DI of `Logger`, `Tracer`, `Metrics` | No direct vendor SDK imports in domain code |

## Build Order for Solo Author (Take-Home)

Sequenced for reviewable milestones — each step produces a demoable artifact.

1. **HTTP skeleton + OpenAPI + health.** Bootstrap the server, define `openapi.yaml` with `GET /vehicles/{vin}/documents` + `/healthz`, wire one no-op handler returning a stub envelope. Commit `feat(api): http skeleton and openapi contract`.
2. **Mock upstreams + Docker Compose.** Build the two mock services with representative payloads and env-driven latency/failure knobs. `docker-compose.yml` runs app + both mocks + db. Commit `feat(mocks): sales + service mock upstreams with failure injection`.
3. **Aggregation service with parallel fan-out.** Implement upstream clients (no resiliency yet), wire `allSettled`-equivalent fan-out, merge + sort + tag source, build envelope. Integration test: both mocks happy. Commit `feat(aggregation): parallel fan-out with per-source tagging`.
4. **Persistence + audit.** Add repository layer, migrations for `audit_request` and `cached_response`. Wire audit write. Commit `feat(persistence): audit table and repository layer`.
5. **Resiliency.** Add `HttpExecutor` with timeout, retry-with-jitter, per-source circuit breaker, bulkhead. Add stale-on-failure read from `cached_response`. Integration tests that inject latency and errors via the mocks. Commit `feat(resiliency): timeouts, circuit breakers, bulkheads, stale-on-failure`.
6. **Observability wiring.** OpenTelemetry tracer with OTLP exporter, Prometheus metrics endpoint, structured JSON logger with request_id + masked vin. Add otel-collector + Jaeger + Prometheus to Compose (optional but high-signal for reviewers). Commit `feat(observability): otel tracing, prometheus metrics, structured logs`.
7. **Tests.**
   - Unit: VIN validator, merge/sort logic, envelope builder, per-source result classifier.
   - Integration: happy path, one-source-timeout, one-source-5xx, both-fail-no-cache, both-fail-with-cache-stale.
   - Contract: response validates against `openapi.yaml` (use a schema validator in the test suite).
   - Load: a small k6 / autocannon script demonstrating latency percentiles and breaker behavior.
   Commit `test: unit + integration + contract + load suites`.
8. **System Design Document + README + AI Narrative.** SDD covers: architecture diagram (reuse the one above), component roles, data flow, key decisions + trade-offs, observability strategy, scaling considerations, GenAI-in-design section. README covers: build/run/test, how to toggle mock failures, AI Collaboration Narrative describing what AI helped with, how outputs were verified, and how quality was owned. Commit `docs: system design document, readme, ai collaboration narrative`.

Milestones 1–3 deliver a working demo. 4–6 deliver the reliability + operability story. 7–8 deliver the submission polish. If time compresses, defer the otel-collector UI (keep OTel SDK + stdout exporter) and the load tests — never cut resiliency or the SDD.

## Sources

- Michael Nygard, *Release It!* (2nd ed.) — circuit breakers, bulkheads, timeouts, stability patterns.
- Sam Newman, *Building Microservices* (2nd ed.) — BFF / aggregation patterns, per-source isolation.
- Netflix Hystrix design docs (historical) — bulkhead + breaker model still foundational.
- AWS Builder's Library, "Timeouts, retries, and backoff with jitter" — retry math and jitter strategies.
- OpenTelemetry specification — trace context propagation + semantic conventions for HTTP.
- RFC 7807 — `application/problem+json` for total-failure responses.
- MDN / ECMA-262 — `Promise.allSettled` semantics.
- Go `golang.org/x/sync/errgroup` + JEP 453 (Structured Concurrency) — concurrency primitives per language.
- Google SRE Book, chapter on handling overload — load shedding and deadline propagation.
- Milestone: greenfield project (take-home submission).

---
*Architecture research for: Aggregator / BFF API with parallel fan-out, partial-failure tolerance, audit + cache, observability*
*Researched: 2026-05-03*
