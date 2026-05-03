# Project Research Summary

**Project:** The Unified Document Viewer
**Domain:** REST API aggregator / BFF with parallel upstream fan-out, partial-failure tolerance, persistent caching, OTel observability
**Researched:** 2026-05-03
**Confidence:** HIGH

## Executive Summary

This is a take-home backend service that aggregates vehicle documents from two mocked dealership systems (Sales + Service) via VIN lookup. The engineering core is the parallel fan-out with partial-failure handling — when one upstream fails, the other's documents must still be returned with clear source attribution. The project is evaluated on four dimensions: system design, technical execution, AI engineering, and communication.

The recommended approach is Go 1.24 with chi router, Postgres 16, and full OpenTelemetry observability (Jaeger + Prometheus), all orchestrated via Docker Compose for a one-command reviewer demo. The stack choice prioritizes readability of the concurrency pattern (Go's `WaitGroup` + channels for non-cancelling fan-out) and fast boot time for reviewer experience.

Key risks are over-scoping (adding infrastructure that doesn't serve the core demo), compose race conditions (broken first-run experience), and incorrect timeout/retry math that makes the resiliency story hollow. Mitigation is disciplined scoping, healthcheck-gated compose, and explicit timeout budget assertions.

## Key Findings

### Recommended Stack

Go 1.24 with chi v5 for routing, pgx v5 + sqlc for type-safe database access, goose for migrations, and OpenTelemetry Go SDK for tracing + metrics. Docker Compose bundles: app, Postgres, two WireMock instances (Sales + Service mocks), OTel Collector, Jaeger, and Prometheus.

**Core technologies:**
- **Go 1.24 + chi v5**: HTTP server + routing — `errgroup`-family concurrency makes fan-out pattern visible in ~10 lines
- **PostgreSQL 16 + pgx v5 + sqlc**: Persistent DB — audit log + stale-on-failure cache; sqlc gives type-safe SQL without ORM magic
- **OpenTelemetry Go SDK + Jaeger + Prometheus**: Observability — fan-out spans visible in Jaeger; RED metrics in Prometheus
- **WireMock 3.10**: Mock upstreams — configurable latency/failure injection via admin API
- **Docker Compose v2**: Orchestration — one `docker compose up` for entire demo
- **gobreaker**: Per-source circuit breaker — small, zero-dep, well-understood

### Expected Features

**Must have (table stakes):**
- VIN validation (17-char, ISO 3779 charset, check-digit)
- Parallel upstream fetch with per-source timeout
- Source-tagged documents in a partial-success response envelope
- Per-source error reporting (`ok`/`failed`/`timeout`/`stale`)
- Persistent DB (audit log + stale cache)
- Structured JSON logs + OTel tracing + Prometheus metrics
- Health endpoints (`/healthz`, `/readyz`)
- OpenAPI 3.x spec + stubbed client
- Unit + integration tests covering partial failure
- Docker Compose one-command demo
- System Design Document + README + AI Collaboration Narrative

**Should have (competitive differentiators):**
- Stale-while-revalidate cache (serve last-known-good on failure)
- Per-source circuit breaker (gobreaker)
- Retry with exponential backoff + jitter (1 retry max)
- Grafana dashboard (pre-provisioned, high visual impact)
- Request audit log persisted to DB
- Graceful shutdown

**Defer (v2+):**
- Authentication / authorization
- Pagination / filtering / sorting
- Additional upstream sources
- Full-text search
- Kubernetes manifests

### Architecture Approach

Layered single-service architecture: HTTP edge (router + middleware) → handler (DTO validation) → aggregation service (fan-out orchestration) → upstream clients (per-source, policy-wrapped) → repository (audit + cache). The aggregation service uses non-cancelling parallel fan-out to collect all upstream results independently, then merges + sorts + builds the response envelope with per-source status.

**Major components:**
1. **HTTP Server + Middleware**: Request-ID injection, logging, OTel root span, error mapping
2. **Aggregation Service**: Parallel fan-out, merge + sort, envelope construction, stale-cache fallback
3. **Upstream Clients (per source)**: Typed HTTP client with timeout → retry → circuit breaker pipeline
4. **Repository Layer**: Audit append + cached_response upsert (Postgres)
5. **Mock Upstreams**: Standalone WireMock services with failure injection
6. **Observability Stack**: OTel Collector → Jaeger (traces) + Prometheus (metrics)

### Critical Pitfalls

1. **Fail-fast fan-out** — Using `errgroup.WithContext` cancels siblings; use non-cancelling `WaitGroup + channel` instead
2. **Compose race conditions** — App starts before DB/migrations ready; use `depends_on: condition: service_healthy`
3. **Timeout math doesn't add up** — Ensure `per_call + retries × backoff < overall_deadline`; document the budget
4. **Over-scoping** — Adding Kafka/K8s/auth kills the demo; one service, one compose, < 8 containers
5. **Tests mock the wrong layer** — Integration tests must hit real HTTP + real Postgres, not in-process fakes

## Implications for Roadmap

Based on research, suggested phase structure:

### Phase 1: Project Foundation
**Rationale:** Skeleton must work end-to-end before adding business logic; compose race conditions are the #1 first-impression killer
**Delivers:** Buildable Go service, Docker Compose with healthchecks, OpenAPI spec, CI-ready structure
**Addresses:** Project structure, Docker Compose, OpenAPI contract
**Avoids:** Compose race conditions (Pitfall 2)

### Phase 2: Core Aggregation
**Rationale:** The fan-out pattern IS the challenge; must be implemented with non-cancelling semantics from day one
**Delivers:** Working `GET /vehicles/{vin}/documents` with parallel fan-out, source tagging, partial-success envelope
**Addresses:** VIN validation, parallel fetch, source tagging, partial-success response
**Avoids:** Fail-fast fan-out (Pitfall 1)

### Phase 3: Persistence & Caching
**Rationale:** Brief requires persistent DB; stale-on-failure cache is the highest-value differentiator
**Delivers:** Audit log, stale-while-revalidate cache, enhanced partial-failure experience
**Addresses:** Persistent DB requirement, graceful degradation
**Avoids:** Stale cache without staleness indicator (Pitfall 5)

### Phase 4: Resiliency
**Rationale:** Circuit breaker + retry + timeout budget must be layered correctly after the base fan-out works
**Delivers:** Per-source circuit breaker, retry with jitter, timeout budget validation
**Addresses:** Production-readiness signals, "build for the future"
**Avoids:** Breaker/retry double-counting (Pitfall 3), timeout math (Pitfall 9)

### Phase 5: Observability
**Rationale:** Explicit requirement; must be selective (not noisy) and demonstrate fan-out in Jaeger
**Delivers:** OTel tracing with fan-out child spans, Prometheus metrics, structured logs, Jaeger + Prometheus in Compose
**Addresses:** All observability requirements
**Avoids:** OTel noise (Pitfall 7), VIN in logs (Pitfall 4)

### Phase 6: Testing & Quality
**Rationale:** Tests validate the entire story; must hit real deps, not mocks of mocks
**Delivers:** Unit tests (VIN, merge, envelope), integration tests (real HTTP + real DB), contract tests against OpenAPI
**Addresses:** Test suite requirement
**Avoids:** Testing wrong layer (Pitfall 8)

### Phase 7: Documentation & Delivery
**Rationale:** SDD + README + AI Narrative are explicit deliverables; must reflect actual implementation
**Delivers:** System Design Document, README with build/run/test + AI Narrative, polished demo experience
**Addresses:** All documentation requirements, Communication rubric dimension

### Phase Ordering Rationale

- Phases 1-2 deliver a working demo (the minimum viable submission)
- Phase 3 adds the reliability story (biggest differentiator)
- Phase 4 adds production-readiness signals (circuit breaker, retry)
- Phase 5 makes the observability tangible (Jaeger screenshots for SDD)
- Phase 6 validates everything (tests reference real behavior)
- Phase 7 wraps the narrative (SDD + README reference working artifacts)
- Each phase builds on the previous; no phase requires rework of earlier phases

### Research Flags

Phases likely needing deeper research during planning:
- **Phase 4:** Retry/breaker interaction math — validate budget with specific values
- **Phase 5:** OTel Go SDK wiring for chi middleware + pgx — check latest contrib API

Phases with standard patterns (skip research-phase):
- **Phase 1:** Standard Go project layout + Docker Compose
- **Phase 2:** Well-documented fan-out pattern (in STACK.md)
- **Phase 3:** Standard repository + upsert pattern
- **Phase 6:** Standard Go testing patterns
- **Phase 7:** Documentation — no research needed

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | Go + Postgres + OTel is the default 2026 choice for this problem shape |
| Features | HIGH | Directly traceable to PROJECT.md requirements and challenge brief |
| Architecture | HIGH | Scatter-gather BFF is a well-understood pattern with clear Go idioms |
| Pitfalls | HIGH | Each pitfall is either documented in industry literature or common in take-home reviews |

**Overall confidence:** HIGH

### Gaps to Address

- **Exact OTel contrib versions**: Verify `otelhttp` and `otelpgx` compatibility with Go 1.24 during Phase 5 planning
- **WireMock admin API for dynamic failure injection**: Verify request-level header-based latency injection during Phase 1 planning
- **sqlc + pgx v5 JSONB handling**: Verify type mapping for `outcomes JSONB` column during Phase 3 planning

## Sources

### Primary (HIGH confidence)
- `/go-chi/chi` (Context7) — chi v5 middleware and routing patterns
- `/open-telemetry/opentelemetry-go` (Context7) — OTel Go SDK instrumentation patterns
- `/jackc/pgx` (Context7) — pgx v5 pool and tracing hooks
- Docker Compose specification — healthcheck, depends_on conditions
- WireMock documentation — fault injection, response templating

### Secondary (MEDIUM confidence)
- Netflix/resilience4j patterns — circuit breaker state machine (applied to gobreaker)
- AWS Builder's Library — timeout budget math, jitter strategies
- Michael Nygard, *Release It!* — bulkhead, breaker, timeout composition

### Tertiary (LOW confidence)
- Keyloop engineering culture signals — inferred from job postings (Go + Java, automotive domain)

---
*Research completed: 2026-05-03*
*Ready for roadmap: yes*
