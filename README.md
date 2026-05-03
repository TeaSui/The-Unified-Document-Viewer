# Unified Document Viewer

A backend aggregation service that consolidates vehicle documents from multiple dealership systems into a single API response. Search by VIN, get every document from Sales and Service — tagged by source, resilient to failures, and observable end-to-end.

## Quick Start

```bash
# Start everything (app, database, mocks, Jaeger, Prometheus)
docker compose up --build -d

# Wait for healthy (~15 seconds)
docker compose ps

# Search for vehicle documents
curl -s http://localhost:8080/vehicles/1HGCM82633A004352/documents | jq .
```

**Demo runs in under 2 minutes** from clone to first request.

## What It Does

```
GET /vehicles/1HGCM82633A004352/documents
```

Returns a consolidated list of documents from both Sales and Service systems:

```json
{
  "data": {
    "vin": "1HGCM82633A004352",
    "documents": [
      {"id": "SVC-003-...", "source": "service", "type": "warranty_claim", "title": "Warranty Repair", "date": "2024-08-05T11:15:00Z"},
      {"id": "SALE-001-...", "source": "sales", "type": "purchase_agreement", "title": "Purchase Agreement", "date": "2024-01-15T10:30:00Z"}
    ],
    "sources": [
      {"name": "sales", "status": "ok"},
      {"name": "service", "status": "ok"}
    ]
  },
  "meta": {"request_id": "...", "timestamp": "..."}
}
```

When one upstream fails, you still get documents from the healthy source (or cached stale data):
```json
"sources": [
  {"name": "sales", "status": "stale", "fetched_at": "2024-01-15T12:00:00Z"},
  {"name": "service", "status": "ok"}
]
```

## Build & Run

### Prerequisites
- Docker and Docker Compose v2
- Go 1.25+ (for running tests locally)

### Commands

| Command | Description |
|---------|-------------|
| `docker compose up --build -d` | Start entire stack |
| `docker compose down -v` | Stop and clean up |
| `make smoke` | Quick health check of all services |
| `go test ./internal/... -short -race` | Run unit tests (no Docker needed) |
| `go test ./tests/integration/ -v -timeout 120s` | Run integration tests (Docker must be up) |

### Endpoints

| Endpoint | Description |
|----------|-------------|
| `GET /vehicles/{vin}/documents` | Aggregated document search |
| `GET /healthz` | Liveness probe |
| `GET /readyz` | Readiness probe (DB connectivity) |
| `GET /metrics` | Prometheus metrics |

### Observability UIs

| Service | URL | Description |
|---------|-----|-------------|
| Jaeger | http://localhost:16686 | Distributed traces |
| Prometheus | http://localhost:9090 | Metrics queries |

## Testing

### Unit Tests (42 tests, no external deps)
```bash
go test ./internal/... -short -v -race
```

Covers: VIN validation (12 cases), upstream client (8 tests), aggregator fan-out (13 tests), HTTP handler (6 tests), health endpoints (3 tests).

### Integration Tests (7 tests, requires Docker Compose)
```bash
docker compose up --build -d
go test ./tests/integration/ -v -timeout 120s
```

Scenarios tested:
- Both upstreams succeed → 6 documents, both sources "ok"
- One upstream 5xx → partial success with stale/failed source
- One upstream timeout (>800ms) → deadline enforcement
- Both fail, no cache → 502 upstream_failure
- Both fail, cache exists → 200 with stale data
- Graceful shutdown → in-flight requests drain on SIGTERM

### Fault Injection

The WireMock mocks support manual fault injection via their admin API:

```bash
# Make sales return 500
curl -X POST http://localhost:9001/__admin/mappings -H "Content-Type: application/json" \
  -d '{"priority":0,"request":{"method":"GET","urlPathPattern":"/api/v1/vehicles/.*/documents"},"response":{"status":500}}'

# Test partial failure
curl http://localhost:8080/vehicles/1HGCM82633A004352/documents | jq '.data.sources'

# Reset
curl -X POST http://localhost:9001/__admin/mappings/reset
```

## Architecture

See [docs/SYSTEM_DESIGN.md](docs/SYSTEM_DESIGN.md) for the full System Design Document including:
- Architecture diagram and component responsibilities
- Data flow for happy path, partial failure, and total failure
- Technology choices with justifications
- Resiliency design (timeout budget, circuit breaker, retry policy)
- Observability strategy
- Scaling considerations

## Project Structure

```
cmd/server/main.go           # Application entrypoint and DI wiring
internal/
  config/                    # Environment-variable-driven configuration
  domain/                    # Shared types (Document, SourceStatus, Envelope)
  vin/                       # VIN validation (ISO 3779)
  upstream/                  # HTTP clients: base Client + ResilientClient
  aggregator/                # Parallel fan-out, cache fallback, merge/sort
  documents/                 # HTTP handler (validate → aggregate → respond)
  repository/                # Postgres: audit + cache repositories
  health/                    # Liveness and readiness probes
  observability/             # OTel tracing, metrics, middleware
  platform/postgres/         # Connection pool factory
api/openapi.yaml             # OpenAPI 3.1 specification
migrations/                  # SQL schema (audit_request, cached_response)
mocks/                       # WireMock mappings (sales + service)
tests/integration/           # Full-stack integration tests
observability/               # Prometheus scrape config
docker-compose.yml           # Full stack orchestration
```

## API Contract

The OpenAPI 3.1 specification is at [`api/openapi.yaml`](api/openapi.yaml). Key response shapes:

- **200 OK**: `DocumentsEnvelope` with `data.documents[]`, `data.sources[]`, `meta`
- **400 Bad Request**: `ErrorResponse` with `error.code = "invalid_vin"`
- **502 Bad Gateway**: `ErrorResponse` with `error.code = "upstream_failure"`

## AI Collaboration Narrative

### Strategy

AI (Claude) was used as a research accelerator and implementation partner throughout this project. The collaboration followed a structured workflow:

1. **Research phase**: AI analyzed the problem domain, recommended technology stack, and identified architectural patterns (BFF aggregation, non-cancelling fan-out, stale-on-failure caching)
2. **Planning phase**: AI produced structured implementation plans decomposed into bite-sized tasks with explicit TDD steps
3. **Implementation phase**: AI implemented each task following the plan, with compilation verification and test execution at every step
4. **Review phase**: Each task was reviewed for spec compliance before proceeding

### Verification Approach

Every AI-generated output was verified before acceptance:

- **Code correctness**: `go build ./...` + `go vet ./...` after every change
- **Behavioral correctness**: Tests written before or alongside implementation (TDD), run with `-race` flag
- **Integration correctness**: Full Docker Compose smoke tests at the end of each phase
- **Resiliency verification**: Fault injection via WireMock admin API proving timeout, retry, breaker, and stale-on-failure actually work

### What AI Got Wrong (and How It Was Caught)

| Issue | How Detected | Resolution |
|-------|-------------|-----------|
| WireMock `fixedDelayMilliseconds` can't be a template string | Container failed to start; error in logs | Changed to fixed integer value |
| goose Docker image (ghcr.io/pressly/goose) access denied | `docker pull` failed | Replaced with psql-based init.sql |
| `wget --spider` fails on WireMock admin API | Healthcheck never passed | Changed to `wget -O /dev/null` |
| Test helper used port 5432 but compose maps to 5433 | Integration tests would skip (connection refused) | Fixed port to 5433 |

### Quality Ownership

The author owns all decisions. AI accelerated execution but every commit represents verified, working software:
- 42 unit tests passing with race detector
- 7 integration tests against real infrastructure
- All 35 v1 requirements traced to implementation
- No untested code paths in the critical aggregation logic
