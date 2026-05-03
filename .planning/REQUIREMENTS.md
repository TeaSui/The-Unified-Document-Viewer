# Requirements: The Unified Document Viewer

**Defined:** 2026-05-03
**Core Value:** One search by VIN returns every document for that vehicle across all source systems, with the source of each document clearly identified — even when one upstream system is slow or failing.

## v1 Requirements

Requirements for submission. Each maps to roadmap phases.

### Search & Input

- [ ] **SRCH-01**: User can search for vehicle documents by entering a 17-character VIN
- [ ] **SRCH-02**: System validates VIN format (17 chars, alphanumeric excluding I/O/Q) and returns 400 with structured error on invalid input

### Aggregation

- [ ] **AGGR-01**: Backend fetches documents from Sales System API and Service System API in parallel (not sequentially)
- [ ] **AGGR-02**: Each document in the response is tagged with its source system ("sales" or "service")
- [ ] **AGGR-03**: Response contains a per-source status array indicating success, failure, timeout, or stale for each upstream
- [ ] **AGGR-04**: When one upstream fails/times out, documents from the successful upstream are still returned (partial success)
- [ ] **AGGR-05**: When both upstreams fail and no cache exists, system returns 502 with structured error

### Persistence

- [ ] **PERS-01**: System persists a request audit log to a database (request_id, VIN, timestamp, per-source outcomes)
- [ ] **PERS-02**: System caches last-known-good upstream responses per (VIN, source) in the database
- [ ] **PERS-03**: When an upstream fails, system serves cached response with status "stale" and includes `fetched_at` timestamp

### Resiliency

- [ ] **RESL-01**: Each upstream call has a per-source timeout (configurable, default 800ms)
- [ ] **RESL-02**: Each upstream has an independent circuit breaker that opens on sustained failures
- [ ] **RESL-03**: Failed upstream calls are retried once with exponential backoff + jitter (idempotent GET only)
- [ ] **RESL-04**: Overall request has a hard deadline (configurable, default 1500ms) independent of per-source timeouts

### Observability

- [ ] **OBSV-01**: All logs are structured JSON with request_id, masked VIN (last 6 chars), trace_id
- [ ] **OBSV-02**: OpenTelemetry tracing produces parent span for request with child spans per upstream call
- [ ] **OBSV-03**: Prometheus metrics expose RED method: request rate, error rate, duration histogram per endpoint
- [ ] **OBSV-04**: Per-upstream metrics: latency histogram, success/failure/timeout counts, circuit breaker state

### API Contract

- [ ] **API-01**: RESTful endpoint `GET /vehicles/{vin}/documents` returns JSON envelope with `data` and `meta`
- [ ] **API-02**: Health endpoints: `GET /healthz` (liveness) and `GET /readyz` (readiness, reflects DB connectivity)
- [ ] **API-03**: OpenAPI 3.x specification checked into repo, matching actual response schema

### Mock Upstreams

- [ ] **MOCK-01**: Two standalone mock HTTP services (Sales, Service) return representative document payloads
- [ ] **MOCK-02**: Mocks support configurable latency and failure injection (via environment variables or admin API)
- [ ] **MOCK-03**: Mock data is deterministic (seeded by VIN for reproducibility)

### Infrastructure

- [ ] **INFR-01**: Single `docker compose up` starts entire system (app + mocks + DB + observability stack)
- [ ] **INFR-02**: Docker Compose uses healthchecks and `depends_on: condition: service_healthy` to prevent race conditions
- [ ] **INFR-03**: Application performs graceful shutdown on SIGTERM (drain in-flight, close DB pool)

### Testing

- [ ] **TEST-01**: Unit tests cover VIN validation, document merge/sort, response envelope construction
- [ ] **TEST-02**: Integration tests cover: both-succeed, one-timeout, one-5xx, both-fail-no-cache, both-fail-with-cache
- [ ] **TEST-03**: Integration tests use real HTTP (mock services) and real Postgres (testcontainers or Compose)

### Documentation

- [ ] **DOCS-01**: System Design Document with architecture diagram, component roles, data flow, tech choices + justifications
- [ ] **DOCS-02**: SDD includes observability strategy section
- [ ] **DOCS-03**: SDD includes GenAI-in-design section describing how AI assisted the design phase
- [ ] **DOCS-04**: README with clear build/run/test instructions (working demo in < 5 minutes)
- [ ] **DOCS-05**: README includes AI Collaboration Narrative (strategy, verification, quality ownership)

## v2 Requirements

Deferred to future release. Tracked but not in current roadmap.

### Enhanced Query

- **QUERY-01**: User can paginate document results (cursor-based)
- **QUERY-02**: User can filter documents by source, document type, or date range
- **QUERY-03**: User can sort documents by date, source, or type

### Observability Enhancement

- **OBSV-05**: Pre-provisioned Grafana dashboard with upstream success rate, p95 latency, circuit breaker state panels
- **OBSV-06**: k6 load test harness demonstrating latency percentiles under failure injection

### Contract Testing

- **TEST-04**: Contract tests validating API responses against OpenAPI spec (schemathesis or Pact)

### Rate Limiting

- **RATE-01**: Per-IP token bucket rate limiting (env-gated, disabled in load-test profile)

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
|---------|--------|
| Authentication / authorization | Not in challenge acceptance criteria; noted as future work in SDD |
| Multi-tenancy (dealership-scoped isolation) | Explicit PROJECT.md exclusion; single-tenant sufficient for demo |
| Full frontend implementation | Backend track chosen; client stubbed via OpenAPI + cURL |
| Document content rendering (PDF/image preview) | "Viewer" means metadata listing, not in-browser rendering |
| Write/update/delete operations | Read-only aggregation per PROJECT.md |
| Kafka / message brokers | Synchronous request-response BFF; no events to publish |
| Kubernetes / Helm | Local Docker Compose sufficient per challenge brief |
| Service mesh (Istio/Linkerd) | In-process resiliency (breaker + timeout) is clearer and auditable |
| Redis cache tier | Postgres-backed cache unifies persistence story; no latency win at take-home scale |
| Full-text search of document content | Requires separate indexer; scope is metadata aggregation only |
| GraphQL | Single REST endpoint sufficient; no client variety to justify |

## Traceability

Which phases cover which requirements. Updated during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| SRCH-01 | Phase 2: Core Aggregation & Search | Pending |
| SRCH-02 | Phase 2: Core Aggregation & Search | Pending |
| AGGR-01 | Phase 2: Core Aggregation & Search | Pending |
| AGGR-02 | Phase 2: Core Aggregation & Search | Pending |
| AGGR-03 | Phase 2: Core Aggregation & Search | Pending |
| AGGR-04 | Phase 2: Core Aggregation & Search | Pending |
| AGGR-05 | Phase 2: Core Aggregation & Search | Pending |
| PERS-01 | Phase 3: Persistence & Caching | Pending |
| PERS-02 | Phase 3: Persistence & Caching | Pending |
| PERS-03 | Phase 3: Persistence & Caching | Pending |
| RESL-01 | Phase 4: Resiliency | Pending |
| RESL-02 | Phase 4: Resiliency | Pending |
| RESL-03 | Phase 4: Resiliency | Pending |
| RESL-04 | Phase 4: Resiliency | Pending |
| OBSV-01 | Phase 5: Observability | Pending |
| OBSV-02 | Phase 5: Observability | Pending |
| OBSV-03 | Phase 5: Observability | Pending |
| OBSV-04 | Phase 5: Observability | Pending |
| API-01 | Phase 2: Core Aggregation & Search | Pending |
| API-02 | Phase 1: Project Foundation & Mock Upstreams | Pending |
| API-03 | Phase 1: Project Foundation & Mock Upstreams | Pending |
| MOCK-01 | Phase 1: Project Foundation & Mock Upstreams | Pending |
| MOCK-02 | Phase 1: Project Foundation & Mock Upstreams | Pending |
| MOCK-03 | Phase 1: Project Foundation & Mock Upstreams | Pending |
| INFR-01 | Phase 1: Project Foundation & Mock Upstreams | Pending |
| INFR-02 | Phase 1: Project Foundation & Mock Upstreams | Pending |
| INFR-03 | Phase 6: Testing & Quality | Pending |
| TEST-01 | Phase 6: Testing & Quality | Pending |
| TEST-02 | Phase 6: Testing & Quality | Pending |
| TEST-03 | Phase 6: Testing & Quality | Pending |
| DOCS-01 | Phase 7: Documentation & Delivery | Pending |
| DOCS-02 | Phase 7: Documentation & Delivery | Pending |
| DOCS-03 | Phase 7: Documentation & Delivery | Pending |
| DOCS-04 | Phase 7: Documentation & Delivery | Pending |
| DOCS-05 | Phase 7: Documentation & Delivery | Pending |

**Coverage:**
- v1 requirements: 35 total
- Mapped to phases: 35
- Unmapped: 0

---
*Requirements defined: 2026-05-03*
*Last updated: 2026-05-03 after roadmap creation — all requirements mapped to phases*
