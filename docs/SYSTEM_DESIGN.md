# System Design Document — Unified Document Viewer

## 1. Overview

The Unified Document Viewer is a backend aggregation service that consolidates vehicle documents from two dealership systems (Sales and Service) into a single API response. A user searches by VIN and receives every related document, tagged by source, even when one upstream is slow or failing.

**Core Value:** One search by VIN returns every document for that vehicle across all source systems, with the source of each document clearly identified — even when one upstream system is slow or failing.

## 2. Architecture

```
┌──────────────────────────────────────────────────────────────────────────┐
│                              Client (cURL / Test Harness)                  │
└─────────────────────────────────────┬────────────────────────────────────┘
                                      │ HTTP GET /vehicles/{vin}/documents
┌─────────────────────────────────────▼────────────────────────────────────┐
│                            Application (Go + chi)                          │
│  ┌────────────────────────────────────────────────────────────────────┐  │
│  │  Middleware: RequestID → Tracing → Logging → Recoverer             │  │
│  └─────────────────────────────────┬──────────────────────────────────┘  │
│                                    │                                      │
│  ┌─────────────────────────────────▼──────────────────────────────────┐  │
│  │  Documents Handler: VIN validation → Aggregator → Envelope/Error    │  │
│  └─────────────────────────────────┬──────────────────────────────────┘  │
│                                    │                                      │
│  ┌─────────────────────────────────▼──────────────────────────────────┐  │
│  │  Aggregation Service: deadline → parallel fan-out → merge/sort      │  │
│  │                        → cache write (success) / cache read (fail)  │  │
│  └───────────┬──────────────────────────────────────┬─────────────────┘  │
│              │                                      │                     │
│  ┌───────────▼────────────┐          ┌──────────────▼─────────────────┐  │
│  │  Resilient Client:     │          │  Resilient Client:             │  │
│  │  Sales API             │          │  Service API                   │  │
│  │  timeout → breaker →   │          │  timeout → breaker →           │  │
│  │  retry (1x jitter)     │          │  retry (1x jitter)             │  │
│  └───────────┬────────────┘          └──────────────┬─────────────────┘  │
│              │                                      │                     │
│  ┌───────────▼──────────────────────────────────────▼─────────────────┐  │
│  │  Repository Layer: Audit (append-only) + Cache (stale-on-failure)   │  │
│  └─────────────────────────────────┬──────────────────────────────────┘  │
└────────────────────────────────────┼─────────────────────────────────────┘
                                     │
┌────────────────────────────────────▼─────────────────────────────────────┐
│  PostgreSQL 16: audit_request + cached_response tables                    │
└──────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────┐
│  Mock Upstreams (WireMock 3.10): Sales API + Service API                 │
│  Deterministic VIN-seeded data, configurable latency/failure injection   │
└─────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────┐
│  Observability Stack: Jaeger (traces) + Prometheus (metrics)              │
└─────────────────────────────────────────────────────────────────────────┘
```

## 3. Component Responsibilities

| Component | Responsibility |
|-----------|---------------|
| **Documents Handler** | HTTP layer: parse VIN from path, validate (ISO 3779), call aggregator, map result to envelope (200) or error (400/502), write audit log |
| **Aggregation Service** | Orchestrate parallel fan-out with overall deadline, collect results, fallback to cache on failure, merge documents, sort by date desc, build per-source status |
| **Resilient Client** | Per-source HTTP client wrapping: context timeout (800ms) → circuit breaker (gobreaker) → retry once with jittered backoff |
| **Cache Repository** | UPSERT last-known-good response per (VIN, source); GET returns nil on miss (not error) |
| **Audit Repository** | Append-only insert: request_id, VIN, HTTP status, duration, per-source outcomes as JSONB |
| **Health Handlers** | `/healthz` (process liveness), `/readyz` (Postgres connectivity check) |
| **Observability** | Tracing (OTel → Jaeger), metrics (OTel → Prometheus), structured JSON logging with correlation |

## 4. Data Flow

### Happy Path
1. Client sends `GET /vehicles/{vin}/documents`
2. Middleware assigns request_id, starts trace span
3. Handler validates VIN format (17 chars, no I/O/Q)
4. Aggregator applies 1500ms overall deadline to context
5. Two goroutines launch in parallel (non-cancelling fan-out)
6. Each resilient client: applies 800ms timeout → circuit breaker check → HTTP call to mock
7. Both succeed: documents merged, sorted by date descending, cached per source
8. Handler builds envelope: `{data: {vin, documents, sources}, meta: {request_id, timestamp}}`
9. Audit entry written (best-effort, never blocks response)
10. 200 OK returned

### Partial Failure (One Source Down)
- Same as above, but failed source triggers cache lookup
- If cached: documents served with `status: "stale"` and `fetched_at` timestamp
- If no cache: source marked `status: "failed"` with error message
- Response is still 200 OK — partial success is visible in `sources[]` array

### Total Failure (Both Sources Down)
- Both sources fail + both caches empty → 502 with `{error: {code: "upstream_failure", message: "..."}}`
- Both sources fail + cache exists → 200 with all documents stale

## 5. Technology Choices & Justifications

| Technology | Choice | Justification |
|-----------|--------|---------------|
| Language | Go 1.25 | `sync.WaitGroup` + `context.WithTimeout` makes fan-out concurrency explicit and visible. Single binary, fast boot (< 1s), small image. |
| Router | chi v5 | Minimal, idiomatic, middleware-based. Far lighter than Gin/Echo for a single-endpoint BFF. |
| Database | PostgreSQL 16 | Challenge requires persistence. JSONB for heterogeneous upstream responses. Reviewer-canonical. |
| DB Driver | pgx v5 | Native protocol, strongly typed, better performance than database/sql + lib/pq. |
| Circuit Breaker | sony/gobreaker | Small, well-understood, zero deps. One instance per upstream. |
| Mock Upstreams | WireMock 3.10 | Standalone HTTP, configurable fault injection via admin API, response templating for VIN-seeded data. |
| Tracing | OpenTelemetry SDK + OTLP → Jaeger | Industry standard 2026. Parent/child spans show fan-out visually. |
| Metrics | OTel SDK + Prometheus exporter | Per-upstream histograms (source × outcome) + RED per endpoint. |
| Orchestration | Docker Compose v2 | Single `docker compose up` for the entire demo. Healthchecks + `depends_on: condition`. |

### Why NOT:
- **No Kafka/RabbitMQ**: Synchronous BFF; no events to publish
- **No Redis**: Postgres-backed cache unifies the persistence story
- **No Kubernetes**: Take-home is local-runnable; Docker Compose eliminates setup friction
- **No GORM**: sqlc/pgx gives visible SQL; no hidden N+1 queries
- **No errgroup.WithContext for fan-out**: That cancels siblings on first error — wrong for partial success

## 6. Resiliency Design

| Policy | Value | Rationale |
|--------|-------|-----------|
| Per-source timeout | 800ms | P99 upstream latency should be < 500ms; 800ms catches slow responses without being too aggressive |
| Retry | 1 attempt, base 50ms + full jitter | Idempotent GET only; single retry recovers transient blips without amplifying load |
| Circuit breaker | 5 consecutive failures to open | Conservative; avoids flapping on isolated timeouts |
| Overall deadline | 1500ms | Hard user-facing SLA; cancels everything including retries |

**Key invariant**: per-source timeout + retry < overall deadline is NOT guaranteed (800+~100+800 = 1700ms > 1500ms). This is intentional — the deadline is the absolute cap. If the first attempt uses most of the budget, the retry is cancelled by the deadline context.

## 7. Persistence Model

```sql
-- Append-only audit trail
CREATE TABLE audit_request (
    request_id  TEXT PRIMARY KEY,
    vin         TEXT NOT NULL,
    ts          TIMESTAMPTZ NOT NULL DEFAULT now(),
    http_status INT NOT NULL,
    duration_ms INT NOT NULL,
    outcomes    JSONB NOT NULL  -- [{name, status, error?}]
);

-- Last-known-good response cache per (VIN, source)
CREATE TABLE cached_response (
    vin        TEXT NOT NULL,
    source     TEXT NOT NULL,
    payload    JSONB NOT NULL,  -- serialized []Document
    fetched_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (vin, source)
);
```

## 8. Observability Strategy

### Structured Logging
- Format: JSON (slog)
- Every log line carries: `request_id`, `trace_id`, `vin` (masked: `***004352`)
- Levels: INFO for successful requests, WARN for failures and degraded paths
- No PII in logs (VIN masked to last 6 chars)

### Distributed Tracing
- OpenTelemetry SDK with OTLP gRPC exporter → Jaeger
- Root span: `GET /vehicles/{vin}/documents` (created by TracingMiddleware)
- Child spans: `upstream.sales`, `upstream.service` (created by ResilientClient)
- Span attributes: `source`, `vin_suffix`, `outcome`, `http.status_code`

### Prometheus Metrics
- `upstream.request.duration` (histogram): per-source latency by outcome
- `upstream.request.total` (counter): per-source call count by outcome (ok/failed/timeout/circuit_open)
- Exposed at `/metrics` endpoint, scraped by Prometheus every 5s

### Alerting (Future)
- `upstream.request.total{outcome="circuit_open"} > 0` → circuit breaker opened
- `histogram_quantile(0.95, upstream.request.duration) > 0.5` → latency degradation

## 9. Scaling Considerations

| Scale | What Changes |
|-------|-------------|
| 0–10 rps (current) | Single process, Postgres, Docker Compose |
| 10–1000 rps | Connection pool (pgbouncer), Redis cache tier, horizontal LB |
| 1000+ rps | Async audit (Kafka), regional read replicas, per-source rate limiting |

The architecture supports horizontal scaling without structural changes — the service is stateless (cache/audit are in Postgres, not in-process).

## 10. Future Considerations

- **Authentication/Authorization**: Not in scope; would add middleware before handler
- **Pagination**: Cursor-based, deferred to v2
- **Third upstream source**: Add another `ResilientClient` to the `sources` slice — open/closed principle
- **Rate limiting**: Per-IP token bucket, env-gated
- **Contract testing**: Validate responses against OpenAPI spec (schemathesis/Pact)

## 11. GenAI in Design

AI (Claude) assisted throughout the design phase:

**What AI helped with:**
- Domain research: stack recommendations, architecture patterns for BFF aggregators, partial-failure semantics
- Requirements engineering: translating challenge brief into structured requirements with traceability
- Roadmap creation: decomposing 35 requirements into 7 phased milestones
- API contract design: OpenAPI spec with envelope structure, error responses, and partial-success semantics
- Resiliency pattern selection: non-cancelling fan-out rationale, timeout budget math

**How outputs were verified:**
- Every AI-generated design decision was validated against the actual implementation
- Pattern recommendations (e.g., `sync.WaitGroup` over `errgroup.WithContext`) were confirmed correct by running integration tests with fault injection
- Architecture diagrams were verified against the real code structure post-implementation

**What was rejected/modified:**
- Initial suggestion of `errgroup.WithContext` for fan-out was rejected — it cancels siblings on first error, violating partial-success semantics
- Suggestion of Redis cache was rejected — Postgres cache unifies the persistence story for a take-home
- goose Docker image (ghcr.io) was unavailable in practice — replaced with psql-based migrations

**Quality ownership:**
The author owns all design decisions. AI accelerated research and drafting but did not make final calls. Every architectural choice was validated through working tests.
