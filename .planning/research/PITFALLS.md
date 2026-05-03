# Pitfalls Research

**Domain:** REST API aggregator / BFF with parallel upstream fan-out, partial-failure tolerance, stale-on-failure cache, OTel observability — take-home submission
**Researched:** 2026-05-03
**Confidence:** HIGH

## Critical Pitfalls

### Pitfall 1: Fail-fast fan-out kills partial success

**What goes wrong:**
Using `errgroup.WithContext` (Go) or `Promise.all` (Node) cancels all in-flight upstream calls when the first one fails. The response contains zero documents even though one source succeeded.

**Why it happens:**
Developers default to the most common concurrency primitive without considering partial-success semantics. Go's `errgroup` documentation leads with the context-cancellation variant.

**How to avoid:**
Use non-cancelling fan-out: `sync.WaitGroup` + channel (Go), `Promise.allSettled` (Node), `asyncio.gather(return_exceptions=True)` (Python). Each upstream result is independent — collect all outcomes, then build the envelope.

**Warning signs:**
- Single integration test for "both succeed" but none for "one fails"
- Any use of `errgroup.WithContext` in the aggregator

**Phase to address:** Phase 2 (core aggregation implementation)

---

### Pitfall 2: Docker Compose health-check race conditions

**What goes wrong:**
`docker compose up` starts all services simultaneously. The app starts before Postgres accepts connections or before migrations complete. The reviewer's first request fails with a DB connection error, leaving a bad first impression.

**Why it happens:**
Default Compose `depends_on` only waits for container start, not readiness. Migrations run as a separate service that may not complete before the app starts.

**How to avoid:**
- Use `depends_on: condition: service_healthy` on the app service
- Postgres healthcheck: `pg_isready -U postgres`
- Migration service: run as init container with `service_completed_successfully` condition
- WireMock mocks: healthcheck on `/__admin/mappings`
- App service: wait for both migration completion AND Postgres readiness

**Warning signs:**
- `depends_on` without `condition` keys
- No `healthcheck` blocks in docker-compose.yml
- Intermittent "connection refused" on first `docker compose up`

**Phase to address:** Phase 1 (project skeleton + Docker Compose)

---

### Pitfall 3: Circuit breaker + retry double-counting failures

**What goes wrong:**
Each retry attempt counts as a separate failure toward the circuit breaker threshold. With 2 retries and a 5-failure threshold, a single bad request (1 original + 2 retries = 3 failures) trips the breaker in under 2 requests.

**Why it happens:**
Retry and circuit breaker are composed without considering their interaction. The breaker observes raw attempt outcomes, not logical request outcomes.

**How to avoid:**
Layer correctly: `breaker → retry → timeout → http`. The breaker wraps the *entire* retry sequence, counting only the final outcome (succeeded-after-retry = success, exhausted-retries = one failure). Alternatively: breaker counts LOGICAL requests (post-retry final outcome only).

**Warning signs:**
- Breaker opens with very few logical requests
- Retry + breaker configured at the same layer without explicit ordering
- No integration test combining retry + breaker behavior

**Phase to address:** Phase 4 (resiliency layer)

---

### Pitfall 4: Logging raw VIN violates privacy principles

**What goes wrong:**
VIN is logged in plaintext in structured logs, trace attributes, and audit tables. VIN is PII-adjacent (linked to vehicle owner). If logs leak, vehicle history is exposed.

**Why it happens:**
VIN feels like "just a vehicle ID" — developers don't consider it sensitive. But it uniquely identifies a vehicle tied to a person.

**How to avoid:**
- Logs: mask to last 6 chars (`***********004352`)
- Trace attributes: use `vin_suffix` or hash
- Audit table: store full VIN (necessary for query) but don't export it to external log systems
- Never log full VIN in stdout/JSON logs

**Warning signs:**
- `slog.String("vin", vin)` without masking
- Trace attributes containing full VIN
- grep of logs showing 17-char VIN strings

**Phase to address:** Phase 5 (observability wiring)

---

### Pitfall 5: Stale cache served without staleness indicator

**What goes wrong:**
When a source fails and cached data is served, the response looks identical to a fresh success. The client (and dealership staff) can't distinguish "this data is from 3 hours ago" from "this data is live."

**Why it happens:**
Cache-hit path reuses the same response builder as the happy path without flagging the difference.

**How to avoid:**
- Source status must be `"stale"` (not `"ok"`) when serving cached data
- Include `fetched_at` timestamp in the source status so clients see the data age
- Response envelope: `{ "name": "sales", "status": "stale", "fetched_at": "2026-05-03T10:00:00Z", "error": "upstream_timeout" }`

**Warning signs:**
- Cache tests only verify documents are returned, not that status is "stale"
- No test for the response metadata when cached data is served

**Phase to address:** Phase 3 (persistence + stale cache)

---

### Pitfall 6: Over-engineering the take-home (scope explosion)

**What goes wrong:**
Adding Kubernetes, Kafka, service mesh, full auth, GraphQL, or multiple microservices. The reviewer sees an incomplete, hard-to-run project that signals "doesn't know how to scope."

**Why it happens:**
Desire to impress with breadth instead of depth. Confusing "build for the future" with "build everything now." Take-home anxiety.

**How to avoid:**
- One service, one repo, one `docker compose up`
- "Build for the future" means clean interfaces + documented extension points, NOT implementing the extensions
- The brief says "consider" scalability — note it in the SDD, don't build it
- Rule of thumb: if it adds a container that isn't directly observed in the demo, cut it

**Warning signs:**
- Docker Compose with 12+ services
- README longer than 3 pages of setup instructions
- Unfinished features committed (half-built auth, stub Kafka)
- `docker compose up` takes > 60 seconds

**Phase to address:** All phases (continuous discipline)

---

### Pitfall 7: OTel instrumentation noise drowns signal

**What goes wrong:**
Auto-instrumenting everything produces hundreds of spans per request (middleware → router → handler → each DB query → each HTTP call → each retry). Jaeger trace view becomes unreadable. Reviewer can't find the fan-out pattern.

**Why it happens:**
OTel auto-instrumentation is on-by-default. Developers enable all interceptors without filtering.

**How to avoid:**
- Instrument selectively: root span (request), one child span per upstream call, one span for DB write
- Skip spans for: middleware traversal, config reads, validation logic
- Name spans meaningfully: `aggregate`, `upstream.sales`, `upstream.service`, `db.audit_write`
- Target: 3-5 spans per request in the happy path

**Warning signs:**
- More than 10 spans per single request in Jaeger
- Span names like `net/http.Handler` or `pgx.Query` without business context
- Jaeger UI requires scrolling to see the trace

**Phase to address:** Phase 5 (observability wiring)

---

### Pitfall 8: Tests mock the wrong layer

**What goes wrong:**
Unit tests mock the HTTP client (never exercise serialization, timeout, or circuit breaker). Integration tests mock the database (never exercise real SQL). The test suite passes but the system doesn't work end-to-end.

**Why it happens:**
Mocking is easier than spinning up real dependencies. Speed over accuracy tradeoff made too aggressively.

**How to avoid:**
- Unit tests: mock at the *upstream interface boundary* (the port/interface), not the HTTP library
- Integration tests: use real Postgres via testcontainers, real WireMock instances
- The ONE critical test: "one upstream returns 500, one returns 200 → response has documents from the 200 AND surfaces the failure" — this MUST hit real HTTP
- Mock only what you don't own (external APIs), never your own DB

**Warning signs:**
- `httpmock` in integration tests
- No `testcontainers` or equivalent in the test suite
- Tests pass but `docker compose up && curl` returns unexpected errors

**Phase to address:** Phase 6 (test suite)

---

### Pitfall 9: Timeout math doesn't add up

**What goes wrong:**
Per-source timeout (800ms) + retry (2 × 400ms backoff cap) = 1600ms. Overall request deadline is 1500ms. The deadline fires before retries complete, making retries useless.

**Why it happens:**
Timeout and retry values are configured independently without checking their sum against the overall budget.

**How to avoid:**
- Establish the invariant: `per_call_timeout + (max_retries × max_backoff) < overall_deadline`
- With 1500ms overall: per-call 500ms + 1 retry × 300ms backoff = 800ms total ✓
- Document the math in a comment or config file
- Or: simply cap retries to 1 with a 600ms per-call timeout

**Warning signs:**
- Independent timeout/retry config without a budget assertion
- Retries that never complete before the overall context cancellation
- Metric showing 0 successful retries ever

**Phase to address:** Phase 4 (resiliency layer)

## Technical Debt Patterns

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|----------------|-----------------|
| Synchronous audit write in request path | Simpler code, no async queue | DB hiccup blocks response; coupling availability | Take-home only (document as best-effort, log on failure, never fail the response) |
| In-memory circuit breaker state | No shared state store needed | Resets on restart; inconsistent across instances | Single-instance take-home; note in SDD that Redis/shared state needed for horizontal scale |
| Hardcoded mock fixture data | Fast to implement | Can't test pagination, edge cases easily | Take-home if fixtures are representative and deterministic |
| Single Compose file for everything | One-command demo | Hard to manage for 8+ services; slow rebuilds | Always acceptable for take-home; split only if you have dev vs test profiles |

## Integration Gotchas

| Integration | Common Mistake | Correct Approach |
|-------------|----------------|------------------|
| WireMock fault injection | Setting latency on the mapping itself (static) | Use `__admin/settings` or response template with `{{request.headers.X-Mock-Latency}}` for dynamic injection |
| OTel Collector → Jaeger | Configuring legacy Jaeger exporter instead of OTLP | Jaeger 2.x accepts OTLP natively — export via `otlp/grpc` to `jaeger:4317`, not the legacy 14268 port |
| Postgres + goose migrations | Running migrations from the app process (race with readiness) | Separate `migrate` service in Compose with `service_completed_successfully` dependency |
| pgx connection pool | Using `pgx.Connect` (single conn) instead of `pgxpool.New` | Always use `pgxpool` — single connections break under concurrency |

## Performance Traps

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|----------------|
| Unbounded goroutines per request | Memory spike under load; OOM kill | Bulkhead semaphore per upstream (max 16 concurrent) | >50 concurrent requests without bulkhead |
| N+1 cache writes (one per document) | Latency spike proportional to document count | Batch upsert: one JSONB payload per (vin, source) | >20 documents per source |
| JSON marshal/unmarshal of large JSONB on every request | CPU spike, GC pressure | Cache deserialized structs in-memory for hot VINs (only if measured) | >1000 RPS with large payloads |
| OTel span creation per document (loop span) | Thousands of child spans; Jaeger chokes | One span per upstream call, not per document in the response | >50 documents per VIN |

## Security Mistakes

| Mistake | Risk | Prevention |
|---------|------|------------|
| Logging full VIN in structured logs | PII exposure if logs ship to external systems | Mask to last 6 chars in all log fields; full VIN only in encrypted DB |
| Exposing WireMock `/__admin` endpoint in non-dev environments | Attacker can reconfigure mock responses | WireMock admin only in `dev`/`test` profiles; bind to localhost only |
| No input validation on VIN path parameter | Injection via crafted VIN string (SQL, log injection) | Validate 17-char alphanumeric (excluding I/O/Q) at handler boundary before any processing |
| Storing raw upstream responses in audit table without size limit | Storage bomb from malicious/broken upstream | Cap stored payload size (e.g., 1MB); truncate with flag |

## UX Pitfalls

| Pitfall | User Impact | Better Approach |
|---------|-------------|-----------------|
| Returning empty `documents: []` on partial failure without explanation | Dealership staff thinks no documents exist | Always include `sources[]` with status — staff sees "Service System unavailable" |
| Sorting documents by insertion order (non-deterministic) | Same VIN returns differently ordered results each time | Stable sort: `date DESC, source ASC, id ASC` |
| Generic error messages ("Internal Server Error") | Staff can't report useful info to support | Structured error with `code`, `message`, `request_id` for support correlation |

## "Looks Done But Isn't" Checklist

- [ ] **Docker demo:** Runs on first `docker compose up` without manual steps — verify on a clean clone (no local volumes)
- [ ] **Partial failure test:** Test that one-source-timeout returns the OTHER source's documents + failure metadata — not just "doesn't crash"
- [ ] **Stale cache:** Test that after a cache hit, the response `status` field says "stale" and includes `fetched_at`
- [ ] **Observability:** Jaeger shows the fan-out as child spans (not flat/disconnected spans)
- [ ] **README instructions:** A new developer can build + run + see results in < 5 minutes following only the README
- [ ] **OpenAPI spec:** Matches actual response shape (run contract test, or manually verify after adding stale/partial fields)
- [ ] **Graceful shutdown:** `docker compose down` doesn't leave zombie connections or unfinished audit writes

## Recovery Strategies

| Pitfall | Recovery Cost | Recovery Steps |
|---------|---------------|----------------|
| Fail-fast fan-out | LOW | Replace `errgroup.WithContext` with `WaitGroup + channel`; ~30 min refactor |
| Compose race conditions | LOW | Add `healthcheck` blocks + `condition: service_healthy`; 15 min |
| Breaker/retry math wrong | LOW | Recalculate budget; adjust config; 15 min |
| VIN in logs | MEDIUM | Grep + replace all log calls; add masking helper; 1 hour |
| Over-scoped project | HIGH | Triage and delete; painful because of sunk time; 2-4 hours |
| Tests mock wrong layer | HIGH | Rewrite integration tests with real deps; 4+ hours |

## Pitfall-to-Phase Mapping

| Pitfall | Prevention Phase | Verification |
|---------|------------------|--------------|
| Fail-fast fan-out | Phase 2 (aggregation) | Integration test: one source 500 → response contains other source's docs |
| Compose race conditions | Phase 1 (skeleton) | Clean `docker compose up` succeeds first try on empty machine |
| Breaker/retry math | Phase 4 (resiliency) | Assert: budget invariant holds in config; integration test with slow mock |
| VIN privacy | Phase 5 (observability) | Grep all log calls for raw VIN; zero matches |
| Stale cache indicator | Phase 3 (persistence) | Test: cached response has `status: "stale"` and `fetched_at` |
| Over-engineering | All phases | `docker compose config --services` ≤ 8; README demo < 5 min |
| OTel noise | Phase 5 (observability) | Jaeger trace for happy path has ≤ 5 spans |
| Wrong test layer | Phase 6 (tests) | Integration tests spin real Postgres + real HTTP mocks |
| Timeout math | Phase 4 (resiliency) | Config assertion: per-call + retries < overall deadline |

## Sources

- AWS Builder's Library, "Timeouts, retries, and backoff with jitter" — timeout budget math
- Michael Nygard, *Release It!* — breaker/retry interaction, bulkhead patterns
- Docker Compose specification — `depends_on.condition`, `service_healthy`, `service_completed_successfully`
- OpenTelemetry best practices — selective instrumentation, span naming conventions
- GDPR/PDPA guidance on vehicle identification — VIN as PII-adjacent
- Common take-home reviewer feedback (Keyloop engineering blog, Glassdoor)
- PROJECT.md and research docs in this repo

---
*Pitfalls research for: REST aggregator / BFF with parallel fan-out — take-home submission*
*Researched: 2026-05-03*
