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
│  │  Repository Layer                                                   │  │
│  │    Cache: Redis 7 (TTL-based, stale-on-failure reads)               │  │
│  │    Audit: Kafka producer → async (non-blocking request path)        │  │
│  └──────────────┬──────────────────────────────┬─────────────────────┘  │
│                 │                              │                         │
│  ┌──────────────▼────────┐     ┌──────────────▼─────────────────────┐  │
│  │  Kafka Consumer        │     │                                    │  │
│  │  (background goroutine)│     │                                    │  │
│  │  topic → Postgres      │     │                                    │  │
│  └──────────────┬─────────┘     │                                    │  │
└─────────────────┼───────────────┼────────────────────────────────────────┘
                  │               │
┌─────────────────▼───────────────▼────────────────────────────────────────┐
│  Redis 7         │  Kafka (KRaft)   │  PostgreSQL 16                      │
│  (cache layer)   │  (audit events)  │  (audit persistence)                │
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
| **Documents Handler** | HTTP layer: parse VIN from path, validate (ISO 3779), call aggregator, map result to envelope (200) or error (400/502), publish audit event |
| **Aggregation Service** | Orchestrate parallel fan-out with overall deadline, collect results, fallback to cache on failure, merge documents, sort by date desc, build per-source status |
| **Resilient Client** | Per-source HTTP client wrapping: context timeout (800ms) → circuit breaker (gobreaker) → retry once with jittered backoff |
| **Redis Cache** | SET/GET with TTL (1h default) keyed by `cache:{vin}:{source}`; sub-millisecond stale-on-failure reads |
| **Kafka Audit Producer** | Non-blocking publish to `audit-requests` topic; decouples audit from request path latency |
| **Kafka Audit Consumer** | Background goroutine consuming from topic, persisting to Postgres via GORM (structured model with ON CONFLICT DO NOTHING) |
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
7. Both succeed: documents merged, sorted by date descending, cached to Redis (TTL 1h)
8. Handler builds envelope: `{data: {vin, documents, sources}, meta: {request_id, timestamp}}`
9. Audit event published to Kafka `audit-requests` topic (async, non-blocking)
10. 200 OK returned
11. Kafka consumer persists audit entry to Postgres (background, eventually consistent)

### Partial Failure (One Source Down)
- Same as above, but failed source triggers Redis cache lookup
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
| Database | PostgreSQL 16 | Persistent audit trail. JSONB for per-source outcomes. Durable store for the Kafka consumer. |
| DB Driver | pgx v5 + GORM | pgx for connection pooling (health checks, direct queries); GORM for the Kafka consumer's audit persistence (ORM convenience for structured inserts). |
| Cache | Redis 7 | Sub-millisecond reads for stale-on-failure. TTL-based expiry (1h default). Decouples cache tier from persistence tier. |
| Message Broker | Apache Kafka (KRaft) | Decouples audit writes from request path. Async producer is non-blocking — audit never adds latency. Consumer writes to Postgres in background. |
| Circuit Breaker | sony/gobreaker | Small, well-understood, zero deps. One instance per upstream. |
| Mock Upstreams | WireMock 3.10 | Standalone HTTP, configurable fault injection via admin API, response templating for VIN-seeded data. |
| Tracing | OpenTelemetry SDK + OTLP → Jaeger | Industry standard 2026. Parent/child spans show fan-out visually. |
| Metrics | OTel SDK + Prometheus exporter | Per-upstream histograms (source × outcome) + RED per endpoint. |
| Orchestration | Docker Compose v2 | Single `docker compose up` for the entire demo. Healthchecks + `depends_on: condition`. |

### Why These Choices:
- **Redis over Postgres for cache**: Sub-ms reads vs ~1ms from Postgres. TTL expiry is native (no cron job). At scale, Redis handles 100k+ ops/sec without connection pool pressure on the primary DB.
- **Kafka over synchronous audit**: Removes DB write from the hot path. At 1000+ rps, synchronous INSERT would become the bottleneck — Kafka absorbs bursts and the consumer drains at its own pace.
- **Postgres still for audit persistence**: Kafka provides durability and decoupling, but the final queryable store is still Postgres (SQL for analytics, indexing by VIN/timestamp).

### Why NOT:
- **No Kubernetes**: Take-home is local-runnable; Docker Compose eliminates setup friction
- **GORM used selectively**: Kafka consumer uses GORM for audit persistence (structured inserts with ON CONFLICT). Hot-path queries (health check, cache) use pgx directly for control.
- **No errgroup.WithContext for fan-out**: That cancels siblings on first error — wrong for partial success
- **No RabbitMQ**: Kafka's log-based retention gives replay capability; RabbitMQ is fire-and-forget

## 6. Resiliency Design

| Policy | Value | Rationale |
|--------|-------|-----------|
| Per-source timeout | 800ms | P99 upstream latency should be < 500ms; 800ms catches slow responses without being too aggressive |
| Retry | 1 attempt, base 50ms + full jitter | Idempotent GET only; single retry recovers transient blips without amplifying load |
| Circuit breaker | 5 consecutive failures to open | Conservative; avoids flapping on isolated timeouts |
| Overall deadline | 1500ms | Hard user-facing SLA; cancels everything including retries |

**Key invariant**: per-source timeout + retry < overall deadline is NOT guaranteed (800+~100+800 = 1700ms > 1500ms). This is intentional — the deadline is the absolute cap. If the first attempt uses most of the budget, the retry is cancelled by the deadline context.

## 7. Persistence & Messaging Model

### Redis Cache
```
Key:    cache:{vin}:{source}
Value:  JSON { "documents": [...], "fetched_at": "..." }
TTL:    1 hour (configurable via REDIS_CACHE_TTL_MS)
```

### Kafka Topic
```
Topic:      audit-requests
Key:        request_id
Value:      JSON { RequestID, VIN, HTTPStatus, DurationMs, Outcomes }
Partitions: 1 (expandable for throughput)
```

### PostgreSQL (Audit Persistence)
```sql
-- Populated by Kafka consumer (eventually consistent)
CREATE TABLE audit_request (
    request_id  TEXT PRIMARY KEY,
    vin         TEXT NOT NULL,
    ts          TIMESTAMPTZ NOT NULL DEFAULT now(),
    http_status INT NOT NULL,
    duration_ms INT NOT NULL,
    outcomes    JSONB NOT NULL  -- [{name, status, error?}]
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
| 0–10 rps (current) | Single process, Redis cache, Kafka single-partition, Docker Compose |
| 10–1000 rps | Horizontal app instances behind LB, Kafka partition scale-out, Redis cluster mode |
| 1000+ rps | Regional Redis replicas, Kafka multi-partition with consumer groups, Postgres read replicas for audit queries |

The architecture supports horizontal scaling without structural changes:
- **App is stateless** — cache in Redis, audit in Kafka, persistence in Postgres
- **Redis cache** handles 100k+ ops/sec per instance; cluster mode for HA
- **Kafka** absorbs write bursts; add partitions + consumers for throughput
- **Postgres** is the cold-path query store (not on the hot request path)

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
- Initial Postgres-only cache was later replaced with Redis for sub-ms reads and native TTL expiry
- Synchronous audit INSERT was replaced with Kafka async producer to decouple from request path
- goose Docker image (ghcr.io) was unavailable in practice — replaced with psql-based migrations

**Quality ownership:**
The author owns all design decisions. AI accelerated research and drafting but did not make final calls. Every architectural choice was validated through working tests.
