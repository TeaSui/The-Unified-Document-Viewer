# Feature Research

**Domain:** Backend API aggregator / document-indexing BFF for automotive dealership operations — fans out by VIN across mocked Sales + Service systems, returns a source-tagged, partial-failure-tolerant consolidated document list. Take-home submission targeting Keyloop's Operate domain.
**Researched:** 2026-05-03
**Confidence:** HIGH (table stakes, anti-features), MEDIUM-HIGH (differentiators — judgment calls on which to ship)

## Feature Landscape

### Table Stakes (Users Expect These)

Features that must exist for the submission to read as a credible, production-minded aggregator. Reviewers will penalize their absence (explicitly for rubric dimensions Problem Solving and Technical Execution); their presence earns no points but establishes baseline credibility.

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| VIN validation at entry boundary (17-char, ISO 3779 check-digit, character set excluding I/O/Q) | Canonical dealership key — invalid input must fail fast with 400, not fan out | LOW | Pre-flight check before upstream calls; return structured error per patterns.md |
| Parallel upstream fetch (concurrent Sales + Service calls) | Sequential fan-out doubles p95 latency and reads as naive; parallel is the engineering core of the problem | LOW | `errgroup` (Go) / `CompletableFuture.allOf` (Java) / `asyncio.gather` (Python) |
| Per-source aggregated response with source tag on every document | Core Value: "source of each document clearly identified"; dealership staff MUST know which system a doc came from for workflow + system of record | LOW | `{ "source": "sales" | "service", ... }` on each doc |
| Per-source error reporting (partial success envelope) | Explicit acceptance criterion: "return what succeeded + surface which source failed" | LOW | Response envelope: `{ data: { documents: [...] }, meta: { sources: [{name, status, error_code?, latency_ms}] } }` |
| Per-source timeout (hard deadline per upstream call) | Slow upstream must not starve the aggregated request; deadline propagation is basic resilience | LOW | Typical 2–5s per source; total request budget ~6–8s |
| Health endpoint (`/healthz` liveness, `/readyz` readiness) | Standard for any containerized service; readiness should reflect DB connectivity | LOW | Kubernetes/Compose convention; readiness ≠ liveness |
| OpenAPI 3.x spec checked into repo | Stubbed client is delivered via OpenAPI per PROJECT.md scope decision; also doubles as living API contract for reviewer | LOW | `openapi.yaml` at repo root; generate client stubs for demo |
| Structured logs with correlation/request ID | Observability is an explicit requirement; unstructured logs are a red flag in 2026 | LOW | JSON logs; propagate `X-Request-ID` header; include `vin_hash`, not raw VIN (privacy) |
| Basic metrics (request count, latency histogram, per-upstream success rate) | Explicit requirement from PROJECT.md ("latency, error rate, upstream success rate") | LOW | Prometheus `/metrics` endpoint; RED method (Rate, Errors, Duration) |
| Unit + integration tests covering aggregation, partial failure, VIN validation, source tagging | Explicit test suite requirement; partial-failure path is the highest-value test | MEDIUM | Integration tests spin up mock upstreams; use table-driven tests for VIN edge cases |
| Persistent database | Explicit challenge brief requirement; used for audit log + stale-on-failure cache | LOW | Postgres or SQLite; repository pattern per patterns.md |
| Dockerfile + docker-compose for one-command demo | "Local-runnable via Docker Compose is sufficient" per PROJECT.md constraints | LOW | `docker compose up` → aggregator + mock-sales + mock-service + DB |
| README with build/run/test + AI Collaboration Narrative | Explicit deliverable; Communication rubric dimension | LOW | Follows the brief's four-dimension framing |
| System Design Document with architecture diagram + GenAI-in-design section | Explicit deliverable | MEDIUM | Sequence diagram of fan-out; component diagram; failure-mode table |

### Differentiators (Competitive Advantage)

Features that signal senior engineering judgment. Ship the ones that align with the Core Value (reliable partial-failure aggregation) and the rubric's AI Engineering + Technical Execution dimensions. Do NOT ship all — picking the right subset IS the signal.

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| Per-source circuit breaker (closed/open/half-open) | Under sustained upstream failure, stop sending doomed requests — protects latency budget and upstream recovery | MEDIUM | `gobreaker` / `resilience4j`; thresholds: 5 failures in 10s → open for 30s |
| Retry with exponential backoff + full jitter | Transient upstream blips shouldn't surface as partial failures; jitter avoids thundering herd | LOW | Max 2 retries per source; only on idempotent GET + 5xx/timeout; NEVER on 4xx |
| Request audit log persisted to DB | Satisfies "persistent DB" requirement with genuine ops value — dealership compliance + debugging | LOW | Table: `request_audit(request_id, vin_hash, timestamp, sources_queried, sources_succeeded, total_latency_ms, correlation_id)`; never store raw document PII |
| Stale-while-revalidate cache of last-known-good upstream response | When a source fails, serve its last successful response with a `stale: true` flag — dealership gets SOMETHING instead of nothing | MEDIUM | Biggest Core-Value multiplier; DB-backed (no Redis per anti-features); TTL 5-15min for freshness, indefinite for stale-fallback |
| Mock upstream services as standalone HTTP with configurable latency/failure injection | Explicit requirement + unlocks all resilience testing; headers like `X-Mock-Latency-Ms`, `X-Mock-Fail-Rate` | LOW | Separate container per mock; deterministic seeded fixtures for reproducibility |
| OpenTelemetry distributed tracing across the fan-out | Fan-out is the exact case where tracing shines; visible demo value in Jaeger | MEDIUM | Spans: `aggregate` (parent) → `fetch_sales`, `fetch_service`, `db_cache_write`; propagate `traceparent` header |
| Docker Compose demo with Jaeger + Prometheus + Grafana | Makes observability tangible in 30 seconds of demo — high signal-per-effort for reviewer | MEDIUM | One `docker compose up` shows traces + metrics live; pre-loaded Grafana dashboard |
| k6 load test harness | Demonstrates performance thinking; shipping the script IS the evidence | LOW | Target: sustained 100 RPS, p99 < 500ms with healthy upstreams; include failure-injection scenario |
| Pagination + filtering on document list | Real dealership vehicles have 50+ docs (full history); unpaged responses break under load | MEDIUM | Cursor-based; filter by `source`, `doc_type`, `date_range` |
| Content-type-aware document metadata (`mime_type`, `size_bytes`, `doc_type` enum) | Lets client render/sort/filter meaningfully without fetching bodies | LOW | Enum: `invoice`, `work_order`, `inspection`, `warranty_claim`, `contract` |
| Sorting (by date desc default, by source, by doc_type) | Vehicle history is temporal — date-desc is the obvious default; configurable sort shows care | LOW | Stable sort for determinism |
| Contract tests validating responses against OpenAPI | Prevents spec drift; cheap signal of API-first thinking | LOW | `schemathesis` / `dredd` / Pact; run in CI |
| Graceful shutdown (SIGTERM → drain in-flight → close DB pool) | Production hygiene; 10 lines of code; huge signal-to-effort ratio | LOW | Context cancellation propagation to upstream calls |
| Rate limiting (per-IP token bucket) | Defensive depth; protects mocked upstreams from accidental self-DDoS during load tests | LOW | In-memory token bucket; 10 req/s/IP default |
| Deterministic seeded mock data | Reviewer re-running demo gets identical results → trust; enables snapshot testing | LOW | Seed from VIN hash so "same VIN = same documents" |
| GenAI-in-design artifacts (prompts used, verification log, AI-suggested-then-rejected decisions) | Directly addresses rubric's AI Engineering & Verification dimension — hardest to fake, highest differentiation | MEDIUM | `docs/ai-collaboration/` with prompt library, rejected suggestions with reasoning |

### Anti-Features (Commonly Requested, Often Problematic)

Features that would sink a take-home: over-scoped, wrong-layer, or fake-production signals. Document WHY they're out so reviewers see the judgment.

| Feature | Why Requested | Why Problematic | Alternative |
|---------|---------------|-----------------|-------------|
| Full-text search of document content | "Unified viewer" sounds like Google-for-docs | Requires document body fetch + indexing (ES/OpenSearch) — entire other system; contradicts "metadata listing, not rendering" scope in PROJECT.md | Filter/sort on structured metadata only; full-text is v2+ with separate indexer service |
| In-API document rendering (PDF/image preview) | "Viewer" in the name | PROJECT.md explicitly scopes out; rendering belongs in client, not aggregator API | Return `download_url` pointing back to source system; client fetches + renders |
| Authentication / authorization | "Production-ready" instinct | Not in challenge acceptance criteria; adds days of work (OAuth flow, JWT verification, scopes) with no rubric payoff | Note as future work in design doc; show awareness without implementation |
| Multi-tenancy (dealership-scoped isolation) | Dealerships are tenants in real Keyloop | Explicit "out of scope" in PROJECT.md; forces tenant ID through every layer | Single-tenant v1; `tenant_id` column in audit table as forward-compatibility hint |
| Kafka event streaming for document-change events | "Event-driven architecture" cargo cult | Aggregator is READ-ONLY per PROJECT.md; no events to publish; adds broker + consumer infrastructure for zero value | Webhook-out-on-cache-refresh noted as v2+ in design doc |
| Kubernetes manifests / Helm charts | Signals "production deployment" | PROJECT.md explicitly says local-runnable Docker Compose is sufficient; k8s adds surface area reviewer has to evaluate, distracts from core | Docker Compose + note that k8s is trivial follow-on (stateless service, 12-factor config) |
| Redis cache tier | "Caching is production-ready" | Single-node take-home; adds a service for no latency win; Postgres cache is fast enough at this scale and unifies persistence story | DB-backed stale-while-revalidate (see Differentiators) |
| Service mesh (Istio/Linkerd) | "Observability + resilience out-of-the-box" | Massive complexity for a 2-service fan-out; circuit breaker + timeout in-code is clearer and auditable | In-process resilience (circuit breaker, retry, timeout) |
| Admin UI / dashboard in the same service | "Operators need visibility" | Couples UI to API; Grafana already covers ops visibility; scope creep into frontend track that was explicitly not chosen | Grafana dashboard for ops; API remains headless |
| Write/update/delete endpoints on aggregated documents | "Complete CRUD" instinct | PROJECT.md: "Write operations on upstream systems — read-only aggregation only"; writes back to two sources are a distributed-transaction nightmare | Read-only; document in SDD why write-fan-out is a distinct future design |
| GraphQL layer over REST | "Flexibility for clients" | Stubbed client via OpenAPI per PROJECT.md scope; GraphQL adds schema + resolver layer with no client to benefit | REST with OpenAPI; note GraphQL as future option if client variety emerges |
| ML-based document classification / auto-tagging | AI-era reflex | Upstream systems already have `doc_type`; inferring it degrades quality; confuses AI-for-coding (rubric) with AI-in-product (not asked for) | Trust upstream-provided `doc_type`; use AI in the DEVELOPMENT process per rubric's AI Engineering dimension |

## Feature Dependencies

```
VIN validation ──gates──> Parallel upstream fetch
                              ├──requires──> Per-source timeout
                              ├──requires──> Mock upstream services (dev)
                              └──enhances──> OpenTelemetry tracing (spans per source)

Parallel upstream fetch ──requires──> Per-source error reporting
                                          └──enables──> Partial-success response envelope

Per-source error reporting ──enables──> Per-source circuit breaker
                                 └──enables──> Stale-while-revalidate cache
                                                    └──requires──> Persistent DB
                                                                        └──shares──> Request audit log

Retry with jitter ──requires──> Per-source timeout (bounded total time)
                 └──conflicts──> Aggressive circuit breaker thresholds (double-counting failures)

Structured logs ──requires──> Correlation/request ID
                 └──enables──> OpenTelemetry tracing (log-trace correlation)

Basic metrics ──enables──> k6 load test harness (validates SLOs)
             └──enables──> Grafana dashboard

OpenAPI spec ──enables──> Contract tests
             └──enables──> Stubbed client (per PROJECT.md scope)

Docker Compose ──bundles──> Aggregator + mocks + DB + Jaeger + Prometheus + Grafana
                 └──enables──> One-command reviewer demo

Pagination ──requires──> Deterministic sort order (stable sort by date desc, id asc)

Rate limiting ──conflicts──> k6 load test harness (must be disable-able via env var)
```

### Dependency Notes

- **VIN validation gates upstream fetch:** Invalid VIN → 400 immediately, zero upstream traffic. This is both correctness (no garbage to upstreams) and cost (no wasted fan-out).
- **Partial-success envelope enables differentiators:** Once the response shape admits per-source status, circuit breaker, stale cache, and retry decisions all become observable through the same envelope. This is the highest-leverage early decision.
- **Stale-while-revalidate requires persistent DB + audit log share the same store:** The audit table and cache table live in the same Postgres instance, satisfying the "persistent DB" brief requirement twice for the price of one.
- **Retry with jitter conflicts with aggressive circuit breaker:** Two retries × 5-failures-in-10s breaker = breaker trips on one logical request. Coordinate: breaker counts LOGICAL request failures (post-retry), not raw attempt failures.
- **Rate limiting conflicts with load testing:** k6 harness would be rate-limited. Gate rate limiting behind `ENABLE_RATE_LIMIT` env var (default on; off in load-test compose profile).
- **OpenTelemetry enhances tracing but layers on logs:** Don't pick one — correlation ID in logs, trace ID in spans, inject trace ID into log context so Jaeger ↔ Loki/CloudWatch cross-link works.

## MVP Definition

### Launch With (v1 — the submission)

Minimum viable submission that scores credibly on all four rubric dimensions.

- [ ] VIN validation (17-char, check-digit, charset) — credibility on input hygiene
- [ ] Parallel upstream fetch with per-source timeout — the engineering core
- [ ] Source-tagged aggregated response with partial-success envelope — the Core Value
- [ ] Per-source error reporting with structured error codes — acceptance criterion
- [ ] Persistent DB with request audit log + stale-while-revalidate cache — brief requirement + reliability multiplier
- [ ] Mock Sales + Service services (standalone HTTP, configurable latency/failure, deterministic seed) — per PROJECT.md key decision
- [ ] Structured JSON logs with correlation ID + OpenTelemetry tracing across fan-out — explicit observability requirement
- [ ] Prometheus `/metrics` with RED method metrics — explicit observability requirement
- [ ] Health endpoints (`/healthz`, `/readyz`)
- [ ] OpenAPI 3.x spec + generated stub client — satisfies "stubbed client" scope decision
- [ ] Unit + integration tests: aggregation, partial failure, VIN validation, source tagging, timeout, stale-cache fallback
- [ ] Docker Compose with aggregator + mocks + DB + Jaeger + Prometheus + Grafana — one-command demo
- [ ] Graceful shutdown
- [ ] System Design Document (architecture, data flow, failure modes, tech choices, observability, GenAI-in-design)
- [ ] README with build/run/test + AI Collaboration Narrative
- [ ] GenAI-in-design artifacts (prompt log, verification evidence, rejected suggestions) — rubric's AI Engineering dimension

### Add After Validation (v1.x — if time permits, or first follow-up PR)

- [ ] Per-source circuit breaker — trigger: sustained-failure load test shows retry storms
- [ ] Retry with exponential backoff + jitter — trigger: intermittent-failure injection shows transient errors reaching clients
- [ ] Pagination + filtering + sorting on document list — trigger: mock fixture grows past ~20 docs per VIN
- [ ] Contract tests against OpenAPI — trigger: second endpoint added and spec drift risk appears
- [ ] k6 load test harness — trigger: need to quantify p95/p99 claims in SDD
- [ ] Rate limiting (env-gated) — trigger: deployment to any shared environment

### Future Consideration (v2+ — noted in SDD, not built)

- [ ] Authentication/authorization (OAuth2 + dealership-scoped JWT claims) — defer: not in acceptance criteria; design for it (auth middleware seam) without building
- [ ] Multi-tenancy — defer: PROJECT.md explicit out-of-scope; `tenant_id` column as forward-compatibility
- [ ] Additional upstream sources (Parts, Finance, Warranty) — defer: proves the abstraction; adapter interface design should make this a 1-day task
- [ ] Webhook-on-cache-refresh for downstream subscribers — defer: no current consumer; event-driven is anti-feature until there's an event consumer
- [ ] Full-text search over document content — defer: separate indexer service, separate design
- [ ] Real document body proxy/streaming — defer: separate concern from metadata aggregation
- [ ] Kubernetes deployment manifests — defer: trivial follow-on; Compose meets the brief

## Feature Prioritization Matrix

| Feature | User Value | Implementation Cost | Priority |
|---------|------------|---------------------|----------|
| Parallel fan-out with per-source timeout | HIGH | LOW | P1 |
| Source-tagged partial-success envelope | HIGH | LOW | P1 |
| VIN validation | HIGH | LOW | P1 |
| Mock upstreams with failure injection | HIGH | LOW | P1 |
| Persistent DB (audit + stale cache) | HIGH | MEDIUM | P1 |
| Structured logs + correlation ID | HIGH | LOW | P1 |
| OpenTelemetry tracing | HIGH | MEDIUM | P1 |
| Prometheus metrics | HIGH | LOW | P1 |
| Health endpoints | MEDIUM | LOW | P1 |
| OpenAPI spec + stubbed client | HIGH | LOW | P1 |
| Unit + integration tests | HIGH | MEDIUM | P1 |
| Docker Compose demo | HIGH | LOW | P1 |
| Graceful shutdown | MEDIUM | LOW | P1 |
| SDD + README + AI Narrative | HIGH | MEDIUM | P1 |
| Stale-while-revalidate cache | HIGH | MEDIUM | P1 |
| Circuit breaker | MEDIUM | MEDIUM | P2 |
| Retry with jitter | MEDIUM | LOW | P2 |
| Pagination/filter/sort | MEDIUM | MEDIUM | P2 |
| Contract tests | MEDIUM | LOW | P2 |
| k6 load harness | MEDIUM | LOW | P2 |
| Rate limiting | LOW | LOW | P2 |
| Auth / multi-tenancy | HIGH (real-world) | HIGH | P3 |
| Additional upstream sources | HIGH (real-world) | MEDIUM | P3 |
| Full-text search | MEDIUM | HIGH | P3 |

**Priority key:**
- P1: Must have for submission — missing = credibility hit
- P2: Strong differentiator — ship if time; otherwise named + justified in SDD
- P3: Out of scope for v1 — discussed in SDD "Future Work" only

## Competitor Feature Analysis

Direct "unified document viewer" products in the Operate-domain space are largely proprietary/closed, but the pattern maps to well-known API-aggregator archetypes.

| Feature | Netflix Zuul / Spring Cloud Gateway (BFF pattern) | AWS AppSync (GraphQL federation) | Our Approach |
|---------|--------------|--------------|--------------|
| Fan-out to multiple sources | Route-per-source, aggregate filter | Resolvers per data source, parallel by default | Explicit parallel goroutines/futures with per-source spans; simpler + auditable |
| Partial failure | Per-route circuit breaker (Resilience4j) | Per-resolver errors alongside partial data | Explicit `sources` meta array in every response envelope |
| Caching | Gateway-level Redis | AppSync caching (per-resolver TTL) | DB-backed stale-while-revalidate — no Redis dep, single persistence layer |
| Observability | Sleuth/Zipkin tracing, Micrometer metrics | CloudWatch + X-Ray | OpenTelemetry (vendor-neutral) + Prometheus + Jaeger — self-contained demo |
| Schema/contract | OpenAPI (optional) | GraphQL schema (mandatory) | OpenAPI 3.x + contract tests — lighter weight, fits REST |
| Auth | Gateway filter chain | Cognito/IAM | Deferred to v2 per PROJECT.md; auth middleware seam documented |

| Feature | Typical enterprise "document index" (SharePoint-style) | Our Approach |
|---------|------|------|
| Full-text search | Core feature (FTS index) | Anti-feature v1 — metadata only per scope |
| Document rendering | In-app preview | Anti-feature — client concern, not aggregator |
| Source system identity | Often lost in unified index | First-class: every document carries `source` tag |
| Ops reliability under source failure | Usually all-or-nothing | Partial-success envelope is the headline capability |

## Sources

- PROJECT.md (`/Users/tungnguyen/TYME/The-Unified-Document-Viewer/The-Unified-Document-Viewer/.planning/PROJECT.md`) — authoritative scope, constraints, Core Value, and out-of-scope list
- Challenge rubric dimensions (Problem Solving & System Design, Technical Execution, AI Engineering & Verification, Communication & Presentation) — as referenced in PROJECT.md context
- Industry patterns: BFF (Backend-for-Frontend) by Sam Newman; API Gateway aggregation pattern (Chris Richardson, Microservices.io); Release It! (Michael Nygard) — circuit breaker, timeout, bulkhead patterns
- Observability standards: OpenTelemetry semantic conventions for HTTP client spans; RED method (Tom Wilkie, Grafana Labs); Google SRE book — correlation ID and structured logging conventions
- Resilience libraries referenced as exemplars (not dependencies): resilience4j, gobreaker, Polly — circuit-breaker state-machine semantics
- patterns.md (~/.claude/rules/patterns.md) — error/response envelope shape; repository pattern; DTO validation at boundaries
- security.md (~/.claude/rules/security.md) — VIN privacy handling (hash in logs), no PII in logs, no secrets in code

## Confidence by Bucket

- **Table stakes:** HIGH — directly traceable to PROJECT.md Active requirements and challenge brief deliverables; every item is either explicitly required or universally expected for a 2026-era backend service.
- **Differentiators:** MEDIUM-HIGH — the LIST is high-confidence (these are the standard senior-signal features for aggregators); which SUBSET to ship is MEDIUM confidence and depends on time budget. Strongest bets for rubric impact: stale-while-revalidate cache, OTel tracing, Docker Compose observability stack, GenAI-in-design artifacts.
- **Anti-features:** HIGH — each anti-feature is either explicitly out-of-scope in PROJECT.md, contradicts the read-only/metadata-only scope, or is a well-known take-home over-scoping failure mode. The discipline of naming them in the SDD is itself a rubric signal under Communication.

---
*Feature research for: Unified Document Viewer aggregator API (Keyloop Operate domain take-home)*
*Researched: 2026-05-03*
*Milestone: greenfield*
