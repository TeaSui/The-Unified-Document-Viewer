# Roadmap: The Unified Document Viewer

## Overview

This roadmap delivers a unified vehicle document aggregator from foundation to submission-ready artifact. The journey starts with project scaffolding and mock upstreams (the test harness everything else depends on), builds the core aggregation logic with VIN search, layers persistence and resiliency for production-grade behavior, adds observability to make the architecture visible, validates everything with comprehensive tests, and finishes with documentation that ties the narrative together for reviewers.

## Phases

**Phase Numbering:**
- Integer phases (1, 2, 3): Planned milestone work
- Decimal phases (2.1, 2.2): Urgent insertions (marked with INSERTED)

Decimal phases appear between their surrounding integers in numeric order.

- [ ] **Phase 1: Project Foundation & Mock Upstreams** - Buildable Go service skeleton, Docker Compose, mock upstream services, and API contract
- [ ] **Phase 2: Core Aggregation & Search** - VIN search endpoint with parallel fan-out, source tagging, and partial-success semantics
- [ ] **Phase 3: Persistence & Caching** - Request audit log, response cache, and stale-on-failure fallback
- [ ] **Phase 4: Resiliency** - Per-source timeouts, circuit breakers, retry with backoff, and overall request deadline
- [ ] **Phase 5: Observability** - Structured logging, OpenTelemetry tracing, Prometheus metrics, and observability stack in Compose
- [ ] **Phase 6: Testing & Quality** - Unit tests, integration tests with real HTTP and Postgres, and graceful shutdown
- [ ] **Phase 7: Documentation & Delivery** - System Design Document, README, AI Collaboration Narrative, and polished demo

## Phase Details

### Phase 1: Project Foundation & Mock Upstreams
**Goal**: A running Go service with health endpoints, two standalone mock upstreams returning representative data, Docker Compose orchestrating everything with healthchecks, and an OpenAPI spec defining the contract.
**Depends on**: Nothing (first phase)
**Requirements**: API-02, API-03, MOCK-01, MOCK-02, MOCK-03, INFR-01, INFR-02
**Success Criteria** (what must be TRUE):
  1. `docker compose up` starts the app, database, and both mock services without manual intervention
  2. `GET /healthz` returns 200 and `GET /readyz` returns 200 reflecting DB connectivity
  3. Mock Sales and Service APIs return deterministic document payloads for a given VIN
  4. Mocks accept configuration for latency injection and failure simulation
  5. OpenAPI 3.x spec file exists in the repo and documents the response contract
**Plans**: TBD

Plans:
- [ ] 01-01: Go project skeleton, Docker Compose with Postgres + healthchecks
- [ ] 01-02: Mock upstream services (WireMock) with deterministic data and failure injection
- [ ] 01-03: Health endpoints and OpenAPI specification

### Phase 2: Core Aggregation & Search
**Goal**: A working `GET /vehicles/{vin}/documents` endpoint that validates the VIN, fetches documents from both mocks in parallel using non-cancelling fan-out, tags each document with its source, and returns a partial-success envelope when one upstream fails.
**Depends on**: Phase 1
**Requirements**: SRCH-01, SRCH-02, AGGR-01, AGGR-02, AGGR-03, AGGR-04, AGGR-05, API-01
**Success Criteria** (what must be TRUE):
  1. User can search by VIN and receive a consolidated document list from both sources
  2. Invalid VIN (wrong length, invalid chars) returns 400 with structured error message
  3. When one upstream is down, documents from the healthy upstream are still returned with per-source status
  4. When both upstreams fail and no cache exists, system returns 502 with structured error
  5. Each document in the response is tagged with its source system
**Plans**: TBD

Plans:
- [ ] 02-01: VIN validation and request routing
- [ ] 02-02: Parallel fan-out aggregation service with partial-success envelope
- [ ] 02-03: Response envelope construction and error mapping

### Phase 3: Persistence & Caching
**Goal**: Every request is audit-logged to Postgres, last-known-good responses are cached per (VIN, source), and when an upstream fails the system serves cached data marked as stale with a `fetched_at` timestamp.
**Depends on**: Phase 2
**Requirements**: PERS-01, PERS-02, PERS-03
**Success Criteria** (what must be TRUE):
  1. After a successful request, an audit log entry exists in the database with request_id, VIN, timestamp, and per-source outcomes
  2. After a successful upstream fetch, the response is cached in the database keyed by (VIN, source)
  3. When an upstream fails and a cached response exists, the cached documents are returned with status "stale" and a `fetched_at` timestamp
**Plans**: TBD

Plans:
- [ ] 03-01: Database schema, migrations, and repository layer (audit + cache)
- [ ] 03-02: Stale-on-failure cache integration into aggregation service

### Phase 4: Resiliency
**Goal**: Each upstream call has a configurable timeout, failed calls are retried once with exponential backoff + jitter, each upstream has an independent circuit breaker, and the overall request has a hard deadline.
**Depends on**: Phase 3
**Requirements**: RESL-01, RESL-02, RESL-03, RESL-04
**Success Criteria** (what must be TRUE):
  1. An upstream call that exceeds the per-source timeout (default 800ms) is cancelled and reported as timeout
  2. A failed upstream call is retried exactly once with jittered backoff before being declared failed
  3. Sustained failures on one upstream cause its circuit breaker to open, short-circuiting subsequent calls
  4. The entire request completes or fails within the hard deadline (default 1500ms) regardless of individual source timeouts
**Plans**: TBD

Plans:
- [ ] 04-01: Per-source timeout, retry with backoff + jitter, and overall request deadline
- [ ] 04-02: Circuit breaker per upstream (gobreaker integration)

### Phase 5: Observability
**Goal**: All logs are structured JSON with request correlation, OpenTelemetry tracing shows parent/child spans across fan-out, Prometheus metrics expose RED method per endpoint and per upstream, and the observability stack runs in Docker Compose.
**Depends on**: Phase 4
**Requirements**: OBSV-01, OBSV-02, OBSV-03, OBSV-04
**Success Criteria** (what must be TRUE):
  1. Application logs are structured JSON containing request_id, masked VIN (last 6 chars), and trace_id
  2. Jaeger displays a parent span for each request with child spans for each upstream call
  3. Prometheus exposes request rate, error rate, and duration histogram per endpoint
  4. Per-upstream metrics (latency histogram, success/failure/timeout counts, circuit breaker state) are queryable in Prometheus
**Plans**: TBD

Plans:
- [ ] 05-01: Structured logging with correlation fields
- [ ] 05-02: OpenTelemetry tracing with fan-out child spans + Jaeger in Compose
- [ ] 05-03: Prometheus metrics (RED + per-upstream) + Prometheus in Compose

### Phase 6: Testing & Quality
**Goal**: Unit tests cover VIN validation, document merge/sort, and envelope construction. Integration tests hit real HTTP (mock services) and real Postgres, covering all partial-failure scenarios. Application performs graceful shutdown.
**Depends on**: Phase 5
**Requirements**: TEST-01, TEST-02, TEST-03, INFR-03
**Success Criteria** (what must be TRUE):
  1. Unit tests pass for VIN validation, document merge/sort, and response envelope construction
  2. Integration tests exercise both-succeed, one-timeout, one-5xx, both-fail-no-cache, and both-fail-with-cache scenarios against real HTTP and Postgres
  3. Application drains in-flight requests and closes the DB pool on SIGTERM without dropping connections
**Plans**: TBD

Plans:
- [ ] 06-01: Unit tests (VIN validation, merge/sort, envelope)
- [ ] 06-02: Integration tests (real HTTP + real Postgres, all failure scenarios)
- [ ] 06-03: Graceful shutdown implementation and verification

### Phase 7: Documentation & Delivery
**Goal**: System Design Document with architecture diagram, component roles, data flow, tech justifications, observability strategy, and GenAI-in-design section. README with clear build/run/test instructions and AI Collaboration Narrative. Demo runs in under 5 minutes.
**Depends on**: Phase 6
**Requirements**: DOCS-01, DOCS-02, DOCS-03, DOCS-04, DOCS-05
**Success Criteria** (what must be TRUE):
  1. System Design Document exists with architecture diagram, component roles, data flow, tech choices + justifications, and observability strategy
  2. SDD includes a GenAI-in-design section describing how AI assisted the design phase
  3. README provides build/run/test instructions that produce a working demo in under 5 minutes
  4. README includes an AI Collaboration Narrative covering strategy, verification, and quality ownership
**Plans**: TBD

Plans:
- [ ] 07-01: System Design Document (architecture, data flow, tech choices, observability, GenAI)
- [ ] 07-02: README (build/run/test instructions + AI Collaboration Narrative)

## Progress

**Execution Order:**
Phases execute in numeric order: 1 → 2 → 3 → 4 → 5 → 6 → 7

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. Project Foundation & Mock Upstreams | 0/3 | Not started | - |
| 2. Core Aggregation & Search | 0/3 | Not started | - |
| 3. Persistence & Caching | 0/2 | Not started | - |
| 4. Resiliency | 0/2 | Not started | - |
| 5. Observability | 0/3 | Not started | - |
| 6. Testing & Quality | 0/3 | Not started | - |
| 7. Documentation & Delivery | 0/2 | Not started | - |
