# Stack Research

**Domain:** REST API aggregator / Backend-for-Frontend (BFF) with parallel upstream fan-out, partial-failure tolerance, persistent storage, and OTel-first observability
**Researched:** 2026-05-03
**Confidence:** HIGH (on language + persistence + OTel core); MEDIUM (on choice between Go and Java — both defensible; recommendation favours reviewer legibility)
**Project context:** Keyloop Operate — Unified Document Viewer take-home (Backend track, solo author, Docker Compose demo)

---

## TL;DR — Prescriptive Recommendation

**Primary stack: Go 1.24 + chi + stdlib `net/http` client + `sync/errgroup` + Postgres 16 + goose migrations + OpenTelemetry Go SDK + Prometheus + Jaeger, all wired together in a single `docker-compose.yml`.**

Why Go for this specific take-home:
1. Fan-out concurrency is the core of the problem. `errgroup.WithContext` + `context.WithTimeout` is 10 lines of code that *visibly* demonstrates the partial-failure pattern — reviewers can read it in one screen.
2. Single static binary → the Dockerfile is ~8 lines; the whole repo stays small and reviewable.
3. First-class OTel support (contrib instrumentation for `net/http` and `database/sql` is stable).
4. No JVM warm-up; `docker compose up` to "send a request" is under 5 seconds, which matters when a reviewer spends 20 minutes on your repo.

Alternative (equally defensible, pick if you have more Java muscle memory): **Java 21 + Spring Boot 3.5 + Resilience4j 2.2 + WebClient + Postgres + Flyway + OTel Java Agent**. More ceremony, more "enterprise" signalling, heavier to boot.

Secondary alternative (only if you are strong in it): **Kotlin 2.1 + Ktor 3 + coroutines** — elegant fan-out, but smaller ecosystem for reviewer familiarity.

---

## Recommended Stack (Primary: Go)

### Core Technologies

| Technology | Version | Purpose | Why Recommended |
|------------|---------|---------|-----------------|
| Go | 1.24.x | Language + runtime | `errgroup` + `context` make fan-out + partial-failure idiomatic and readable. Small binaries, fast boot — reviewer-friendly. |
| chi | v5.2.x | HTTP router/framework | Tiny, idiomatic, middleware-based. Far lighter than Gin/Echo for a single-endpoint BFF; plays nicely with `net/http` OTel instrumentation. |
| `net/http` (stdlib) + `sync/errgroup` | stdlib | Upstream fan-out client + concurrency | No third-party HTTP client needed. `errgroup.WithContext` cancels siblings on first error (or you use a non-cancelling variant to preserve partial success — see pattern below). |
| sony/gobreaker | v1.0.0 | Circuit breaker per upstream | Small, well-understood, zero deps. Wrap each upstream client. |
| cenkalti/backoff | v5.x | Retry with exponential backoff + jitter | Simple, context-aware. Use sparingly — only for idempotent GETs. |
| PostgreSQL | 16.x | Persistent DB (audit log + stale-read cache) | Challenge requires persistence; Postgres is the reviewer-default. Supports JSONB for upstream response blobs. |
| pgx | v5.7.x | Postgres driver | Native protocol, strongly typed, better performance than `database/sql` + lib/pq. Has OTel hook via `otelpgx`. |
| sqlc | v1.27.x | Type-safe SQL codegen | Write real SQL, get type-safe Go. Keeps the data-access layer honest and reviewable (no hidden ORM magic). |
| goose | v3.24.x | DB migrations | Simple, single-binary, SQL-first migrations. Runs in a Compose init container. |
| OpenTelemetry Go SDK | v1.34.x | Traces + metrics + logs | Industry default in 2026. One SDK emits all three signals to an OTel Collector. |
| otelhttp / otelpgx | v0.60.x / v0.3.x | Auto-instrumentation | Wraps `http.Handler`, `http.Client`, and `pgx` — fan-out spans appear automatically as children of the inbound request span. |
| OpenTelemetry Collector (contrib) | v0.118.x | Telemetry pipeline | Single container that receives OTLP and fans out to Jaeger (traces) + Prometheus (metrics) + stdout (logs). Makes the observability story visible in Compose. |
| Jaeger | 2.x (all-in-one) | Trace UI | Shows fan-out spans side-by-side — the single most visually compelling observability artefact for a reviewer. |
| Prometheus | v3.1.x | Metrics backend | Scrapes Collector; reviewers can open Prometheus UI and query `http_server_duration_bucket` live. |
| Grafana | 11.4.x | Dashboards (optional but high-ROI) | One provisioned dashboard shows upstream success rate, p95 latency, circuit-breaker state. Reviewer delight per line-of-YAML ratio is very high. |

### Supporting Libraries

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `log/slog` (stdlib) | stdlib (Go 1.24) | Structured JSON logging | Always. Replace any use of `log.Printf`. Set `trace_id` + `span_id` from context. |
| `go-playground/validator` | v10.24.x | VIN validation on input DTO | At the API boundary only (HTTP handler). Enforces 17-char ISO 3779 format. |
| oapi-codegen | v2.4.x | OpenAPI → Go server+client | Hand-write the OpenAPI spec, generate typed handlers + models. Spec-first keeps the README/demo clean. |
| testify | v1.10.x | Assertions + suites | Unit tests only. Don't over-use; table-driven tests are preferred. |
| testcontainers-go | v0.35.x | Ephemeral Postgres for integration tests | Integration tests that hit a real Postgres, not a mock. |
| httpmock OR nested httptest.Server | stdlib + `jarcoal/httpmock` v1.3.x | Upstream mocking in unit tests | Unit-level tests of the aggregator client. For full e2e, reuse the real Compose mocks. |
| k6 | v0.56.x | Load test | One script: hammer the aggregator while toggling upstream failure. Produces the "reliability" evidence for the System Design Document. |
| Mockoon OR WireMock (standalone) | Mockoon 9.x / WireMock 3.10.x | The two mocked upstreams (Sales, Service) | Run as separate Compose services. WireMock wins for configurable latency + failure injection via admin API. |

### Development Tools

| Tool | Purpose | Notes |
|------|---------|-------|
| Docker Compose v2 | Local orchestration | One `docker-compose.yml`: `app`, `postgres`, `sales-mock`, `service-mock`, `otel-collector`, `jaeger`, `prometheus`, `grafana`. Single `docker compose up` demo. |
| golangci-lint | v1.63.x | Static analysis | Runs in CI + pre-commit. Default preset is fine. |
| Air | v1.61.x | Hot reload during dev | Optional; nice to have for the live demo video if you record one. |
| Make | — | Task runner | `make up`, `make test`, `make loadtest`, `make migrate`. Reviewers love a Makefile. |
| Taskfile (Taskfile.dev) | v3.40.x | Alternative to Make | Pick one; don't ship both. |

---

## Installation

```bash
# Go module bootstrap
go mod init github.com/<you>/unified-document-viewer
go get github.com/go-chi/chi/v5@v5.2.0
go get github.com/jackc/pgx/v5@v5.7.2
go get github.com/sony/gobreaker@v1.0.0
go get github.com/cenkalti/backoff/v5@v5.0.0
go get github.com/go-playground/validator/v10@v10.24.0
go get go.opentelemetry.io/otel@v1.34.0
go get go.opentelemetry.io/otel/sdk@v1.34.0
go get go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc@v1.34.0
go get go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc@v1.34.0
go get go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp@v0.60.0
go get github.com/exaring/otelpgx@v0.3.0

# Tooling (installed as CLI, pinned in tools.go)
go install github.com/pressly/goose/v3/cmd/goose@v3.24.0
go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.27.0
go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.4.1

# Dev
go get -t github.com/stretchr/testify@v1.10.0
go get -t github.com/testcontainers/testcontainers-go@v0.35.0
go get -t github.com/jarcoal/httpmock@v1.3.1
```

---

## The Critical Pattern: Partial-Failure Fan-Out

This is the engineering core of the challenge. The go-to pattern in 2026:

```go
// Non-cancelling fan-out: we WANT partial success, so we do NOT use
// errgroup with WithContext for cancellation of siblings.
type result struct {
    source string
    docs   []Document
    err    error
}

func (a *Aggregator) Fetch(ctx context.Context, vin string) (AggregateResponse, error) {
    ctx, cancel := context.WithTimeout(ctx, a.totalBudget) // e.g. 2s
    defer cancel()

    results := make(chan result, 2)
    var wg sync.WaitGroup

    for _, src := range a.sources { // sales, service
        wg.Add(1)
        go func(s UpstreamSource) {
            defer wg.Done()
            ctx, span := a.tracer.Start(ctx, "upstream."+s.Name())
            defer span.End()
            docs, err := s.FetchWithBreaker(ctx, vin) // gobreaker + retry inside
            results <- result{source: s.Name(), docs: docs, err: err}
        }(src)
    }
    wg.Wait()
    close(results)

    return assemble(results) // tags each doc with its source; records failed sources
}
```

Why this not `errgroup.WithContext`: the default errgroup cancels all siblings when any returns an error. For a BFF that must return partial success, you want independent per-upstream contexts. Document this decision in the System Design Document — it is exactly the kind of nuance reviewers score highly.

---

## Alternatives Considered

| Recommended | Alternative | When to Use Alternative |
|-------------|-------------|-------------------------|
| Go + chi | Java 21 + Spring Boot 3.5 + WebClient + Resilience4j 2.2 | Pick if you have more Spring fluency than Go. Resilience4j gives declarative `@CircuitBreaker` + `@Retry` + `@TimeLimiter` annotations which are very legible. Cost: JVM boot time, Gradle complexity, ~3× larger image. Use `spring-boot-starter-actuator` + `micrometer-tracing-bridge-otel` for observability. |
| Go + chi | Kotlin 2.1 + Ktor 3 + coroutines + Arrow | Pick if you know Kotlin well. `async {}` + `awaitAll` is cleaner than Go's manual goroutines. Smaller reviewer pool familiar with Ktor, though. |
| Go + chi | Node 22 LTS + Fastify 5 + undici | Valid. `Promise.allSettled` gives partial-failure semantics for free. Downsides: weaker typing story than Go's compiler + sqlc, OTel Node SDK is still churn-prone in 2026, and reviewers associate Node BFFs with less backend rigour than Go/Java. |
| Go + chi | Python 3.13 + FastAPI 0.115 + httpx + asyncio.gather | Valid for small services but weaker for this specific challenge: asyncio + httpx is fine, but no compile-time guarantees, and OTel Python auto-instrumentation is less polished than Go/Java. Use only if Python is your strongest language. |
| Postgres | SQLite (WAL mode) | Truly tiny demo, no Compose service needed. Rejected: reviewers expect a "real" DB for a Backend-track submission, and Postgres + JSONB is a better match for storing heterogeneous upstream responses. |
| Postgres | DynamoDB Local | Only pick if the target team (Keyloop Operate) is known to run on DynamoDB. Adds Compose complexity and hides the ORM/migration story. Skip for take-home. |
| goose | Flyway / Liquibase | Java-ecosystem tools. Fine with Spring Boot alternative; overkill for Go. |
| Jaeger + Prometheus | Grafana Tempo + Mimir | More "modern" stack, but Jaeger all-in-one is still the lowest-friction trace UI for a reviewer. Tempo needs object storage or local FS config; not worth the bytes for a take-home. |
| Jaeger + Prometheus | Grafana LGTM stack (all-in-one image) | `grafana/otel-lgtm` is one container that does logs/metrics/traces with Grafana pre-wired. Valid shortcut; trade-off is you lose the "look, OTel Collector routes to separate backends" narrative. |
| WireMock | Mockoon | Mockoon has a GUI and fast startup. WireMock wins on dynamic fault injection and response templating, which is exactly what you want to demonstrate partial-failure handling. |

---

## What NOT to Use

| Avoid | Why | Use Instead |
|-------|-----|-------------|
| Kafka / RabbitMQ / any message broker | This is a synchronous request-response BFF. Adding a broker is pure over-engineering and screams "I don't know how to scope." | Synchronous HTTP fan-out with proper timeouts. |
| Kubernetes / Helm / Skaffold | Take-home is local-runnable. K8s adds setup friction that costs reviewer minutes. | Docker Compose. |
| Service mesh (Istio / Linkerd) | Same reason. Observability via OTel SDK is more than sufficient and more readable. | OTel SDK + Collector. |
| Hystrix | Deprecated since 2020. | Resilience4j (Java) or gobreaker (Go). |
| GORM (Go ORM) | Hides SQL, runtime-reflection-heavy, weaker typing. Reviewers will see N+1 risks. | sqlc — real SQL, generated typed Go. |
| Jaeger client libraries (legacy) | Superseded by OTel. | OpenTelemetry Go SDK with OTLP exporter. |
| Zipkin B3 propagation (unless required) | Non-default; adds config. | W3C `traceparent` (OTel default). |
| Lombok-heavy Spring Boot | If you go Java, avoid Lombok unless the reviewer likely uses it. Adds IDE-setup friction. | Java 21 records + plain constructors. |
| Custom retry loops | Easy to get wrong (thundering herd, unbounded retries). | `cenkalti/backoff` with max attempts + jitter, *only* for idempotent GETs. |
| Synchronous retries inside the request path without a budget | Amplifies upstream failure; violates partial-failure goal. | One retry max, total-deadline `context.WithTimeout` on the inbound request. |
| JWT / OAuth implementation | Explicitly out of scope per PROJECT.md. | Note it in "Future Considerations" of the design doc and move on. |
| Redis for caching | Adds a container for no additional value over a Postgres table with a TTL column. | Postgres-backed stale-on-failure cache table (demonstrates DB use + reliability in one move). |
| Full frontend framework | Backend track. | OpenAPI spec + `curl` examples in README + optional Swagger UI in Compose. |
| ELK stack | Heavy; slow to boot; visual noise for a take-home. | OTel Collector → stdout + Jaeger for traces. Structured JSON logs are enough. |

---

## Stack Patterns by Variant

**If reviewer stack is Java-heavy (Keyloop is a mixed-stack shop with strong Java presence):**
- Switch primary to **Java 21 + Spring Boot 3.5.9 + Resilience4j 2.2.0 + WebClient (reactive) OR RestClient (synchronous with virtual threads)**.
- Use Spring Boot 3.5's virtual-thread executor (`spring.threads.virtual.enabled=true`) so synchronous `RestClient` fan-out scales without reactive complexity.
- Resilience4j annotations on the upstream client: `@CircuitBreaker(name="sales")`, `@Retry`, `@TimeLimiter`. Declarative story is easy to screenshot in the design doc.
- `spring-boot-starter-actuator` exposes `/actuator/prometheus` and `/actuator/health`. `micrometer-tracing-bridge-otel` + `opentelemetry-exporter-otlp` send to Collector.
- Flyway for migrations; Spring Data JPA is acceptable *only* if you resist the urge to map upstream JSON into JPA entities (use JSONB + Jackson for that).

**If you record a demo video and want the fastest-to-impress observability:**
- Add Grafana with a single provisioned dashboard JSON. The p95-latency + upstream-success-rate panels narrate the reliability story in 30 seconds.

**If you want to demonstrate contract testing seriously:**
- Add **Pact v2** consumer tests against the two mocks, published to a local Pact Broker in Compose. High-signal for "Technical Execution" scoring but ~4h of effort. Only worth it if you're ahead of schedule.

**If submission time is tight (<2 days remaining):**
- Drop Grafana. Keep Jaeger + Prometheus. Drop k6 (do one `hey` load test manually). Drop sqlc (use pgx directly with hand-written SQL constants). Keep everything else. Observability story is non-negotiable — the brief names it explicitly.

---

## Version Compatibility

| Package A | Compatible With | Notes |
|-----------|-----------------|-------|
| Go 1.24 | otel-go v1.34 | Fine; otel-go supports Go 1.22+. |
| pgx v5.7 | otelpgx v0.3 | Confirmed compatible; `otelpgx.NewTracer()` plugs into `pgxpool.Config.ConnConfig.Tracer`. |
| otelhttp v0.60 | otel-go v1.34 | Contrib version tracks SDK minor. Use matching pair. |
| Spring Boot 3.5.9 | Java 21 | Spring Boot 3.x requires Java 17+; Java 21 gives virtual threads. Don't use Spring Boot 4.0 (milestones only as of 2026-05) unless willing to absorb churn. |
| Resilience4j 2.2.0 | Spring Boot 3.5 | Use `resilience4j-spring-boot3` starter. |
| WireMock 3.10 | Java 17+ standalone jar | Run as its own container; no JDK needed in app image. |
| Jaeger 2.x | OTLP receiver | Jaeger 2.x natively accepts OTLP — no need for the legacy jaeger-collector + agent split. |
| Prometheus 3.1 | OTel Collector prometheusexporter | Collector exposes `/metrics`; Prometheus scrapes it. Native OTLP receive in Prometheus 3.x is also available but scrape is more reviewer-familiar. |

---

## Docker Compose shape (what reviewers will see)

One file, eight services:
1. `app` — the Go service (built from `Dockerfile`)
2. `postgres:16-alpine`
3. `migrate` — one-shot `goose up` init container
4. `sales-mock` — WireMock with JSON mappings under `./mocks/sales/`
5. `service-mock` — WireMock with JSON mappings under `./mocks/service/`
6. `otel-collector` — `otel/opentelemetry-collector-contrib:0.118.0` with `otel-collector-config.yaml`
7. `jaeger` — `jaegertracing/jaeger:2.2.0` (all-in-one, OTLP enabled)
8. `prometheus` — `prom/prometheus:v3.1.0` with `prometheus.yml` scraping the collector

Optional: `grafana` with a provisioned dashboard.

`make up` does it all. `make smoke` runs a cURL against the aggregator. `make chaos` flips the sales mock to return 500s via its admin API, then re-runs smoke to show partial success.

---

## Anti-Patterns Specifically for a Take-Home Submission

1. **Hexagonal architecture with 14 folders for a 500-line service.** Use standard Go layout (`cmd/`, `internal/`, `api/openapi.yaml`). Reviewers recognise it; over-layering reads as cargo-cult.
2. **A README that opens with a 2-page table of contents.** Open with "What it does / How to run / What's interesting". Put architecture detail in the System Design Document.
3. **Shipping CI yamls you never ran.** Either run GitHub Actions green at least once, or don't include CI at all.
4. **Silent AI usage.** The brief *requires* an AI Collaboration Narrative. Be specific: what you prompted, what you verified, what you rejected.
5. **Swallowing upstream errors and returning 200 OK with empty data.** Partial success must be *visible in the response* — tag successful sources and list failed sources with a reason.
6. **Over-testing the mocks, under-testing the aggregator.** The test that matters is: "one upstream 500s, one upstream 200s → response contains the 200 docs AND surfaces the failure." This one test is the whole brief. Write it first (TDD).
7. **Leaving `docker compose up` with 20-second health-check races.** Use `depends_on: condition: service_healthy` and sensible healthchecks on Postgres + mocks, so the demo "just works" first try.

---

## Confidence Breakdown

| Decision | Confidence | Rationale |
|---|---|---|
| OTel SDK + Collector + Jaeger + Prometheus | HIGH | Default 2026 observability architecture; no credible alternative at this scale. |
| Postgres over SQLite / DynamoDB | HIGH | Brief says "persistent database"; reviewers read Postgres as the canonical choice. |
| Docker Compose (not K8s) | HIGH | Take-home convention. |
| WireMock for the two mocks | HIGH | Best fault-injection story; brief explicitly requires configurable latency/failure. |
| Go as primary | MEDIUM | Go's fan-out idioms are genuinely the cleanest demonstration of the problem. Java/Spring is equally valid if you're stronger there. Don't pick Node/Python unless they're your strongest language. |
| sqlc over GORM | HIGH | For a reviewable take-home, visible SQL beats hidden ORM. |
| chi over Gin/Echo/Fiber | MEDIUM | All are fine. chi is the smallest, most idiomatic; Gin has more ecosystem if you need JWT/etc. (you don't). |
| Jaeger 2.x over Tempo | MEDIUM | Tempo is more modern; Jaeger is faster to demo. |
| Non-cancelling fan-out pattern over `errgroup.WithContext` | HIGH | Correct semantics for partial success. Document the decision. |

---

## Sources

- `/spring-projects/spring-boot` (Context7) — Spring Boot 3.5.9 and 4.0.3 (M-series) confirmed; 3.5 is the stable GA line in 2026-05. Confidence: HIGH.
- `/resilience4j/resilience4j` (Context7) — v2.2.0 current stable for Java 17+/Spring Boot 3.x integration. Confidence: HIGH.
- `/open-telemetry/opentelemetry-java` (Context7) — v1.49.0 current for Java SDK; used for the Spring Boot alternative path.
- `/wiremock/wiremock.org` (Context7) — WireMock standalone is the canonical HTTP mock server; supports admin-API-driven failure injection.
- OpenTelemetry Go SDK changelog (go.opentelemetry.io/otel) — v1.34 line is stable in 2026-05; otelhttp contrib v0.60 matches.
- pgx GitHub — v5.7.x current 5.x line; otelpgx v0.3 supports the pgx v5 tracer interface.
- goose GitHub (pressly/goose) — v3.24.x current; supports embedded migrations and out-of-tree SQL files.
- sqlc docs — v1.27.x current; pgx v5 driver target is stable.
- Jaeger v2 release notes — native OTLP receive confirmed; eliminates legacy agent/collector split.
- Prometheus 3.x release notes — native OTLP ingest GA but scrape-of-collector remains most compatible.
- PROJECT.md (this repo) — requirements, out-of-scope, and constraints drove anti-pattern choices (no auth, no frontend, Docker Compose mandatory).

---

*Stack research for: REST aggregator / BFF with parallel fan-out, partial-failure tolerance, persistent storage, OTel observability — take-home submission*
*Researched: 2026-05-03*
