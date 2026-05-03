# The Unified Document Viewer

## What This Is

A unified document viewer for dealership operations that lets a user enter a Vehicle Identification Number (VIN) and see a single, consolidated list of every document related to that vehicle — aggregated in parallel from two separate (mocked) dealership systems (a Sales System API and a Service System API). Built as a take-home challenge in the Operate domain, targeted at dealership staff who today need to jump between siloed systems to piece together a vehicle's history.

## Core Value

One search by VIN returns every document for that vehicle across all source systems, with the source of each document clearly identified — even when one upstream system is slow or failing.

## Requirements

### Validated

(None yet — ship to validate)

### Active

- [ ] Single search interface accepting a VIN as input
- [ ] Backend aggregates documents by issuing parallel requests to the mocked Sales System API and Service System API
- [ ] UI (or stubbed client) displays a consolidated list of documents from both sources, each row clearly showing its source system
- [ ] Graceful partial-success behavior when one upstream system fails or times out (return what succeeded + surface which source failed)
- [ ] RESTful API exposed by the backend with a persistent database (per challenge's Backend track)
- [ ] Mocked/stubbed Sales and Service system APIs (representative payloads, configurable latency/failure for testing)
- [ ] Observability: structured logging, metrics (latency, error rate, upstream success rate), distributed tracing across the aggregation fan-out
- [ ] Test suite validating core business logic (aggregation, partial failure, VIN validation, source tagging)
- [ ] System Design Document with architecture diagram, component roles, data flow, tech choices + justifications, observability strategy, and a GenAI-in-design section
- [ ] README with build/run/test instructions and an AI Collaboration Narrative section

### Out of Scope

- Real integrations with actual dealership systems — the two upstream APIs are mocked per the challenge brief
- Authentication / authorization / multi-tenancy — not in the challenge acceptance criteria; noted as a "future" consideration in the design doc only
- Full frontend implementation — the Backend track is chosen; client is stubbed via OpenAPI + cURL/test harness
- Document content rendering (PDF/image viewing) — "viewer" here means listing/metadata, not in-browser rendering of document bodies
- Write operations on upstream systems — read-only aggregation only
- Production deployment pipelines — local-runnable via Docker Compose is sufficient for the submission

## Context

- This is a take-home evaluation submission. Reviewers will judge across four dimensions: Problem Solving & System Design, Technical Execution, AI Engineering & Verification, and Communication & Presentation.
- Keyloop's Operate domain context: dealership staff workflow; VIN is the canonical vehicle key.
- "Build for the future" is explicitly called out — scalability, performance, reliability, maintainability, and observability must be visible in the design even though the submission itself is a single service.
- A dedicated AI Collaboration Narrative is required in the README describing how AI was used, verified, and how quality was owned by the author.

## Constraints

- **Scope**: Backend track only — full backend + RESTful API + persistent DB; client-side stubbed with OpenAPI / cURL / test harness. — Chosen because the aggregation logic, upstream fan-out, and partial-failure handling are where the interesting engineering lives.
- **Deliverables**: System Design Document + Git repo with working code + README with AI Collaboration Narrative + tests. — Fixed by the challenge brief.
- **Timeline**: Take-home, single-author. — Favours a small, tight stack over enterprise sprawl.
- **Reliability**: Must handle upstream failure/slowness gracefully (partial response, not total failure). — Core Value depends on it.
- **Persistence**: Must use a persistent database. — Explicit in the brief; will be used for request/response audit and cache.

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Backend track (not frontend) | Aggregation + fan-out + partial-failure is the engineering core of this problem | — Pending |
| Mocks as standalone HTTP services (not in-process stubs) | More realistic fan-out, exercises real network timeouts, easier to inject failure scenarios | — Pending |
| Persistent DB used for request audit + short-lived cache | Satisfies "persistent database" requirement while adding genuine reliability value (stale-on-failure reads) | — Pending |

---
*Last updated: 2026-05-03 after initialization*
