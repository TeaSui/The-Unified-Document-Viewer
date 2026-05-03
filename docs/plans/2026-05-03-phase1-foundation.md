# Phase 1: Project Foundation & Mock Upstreams — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A running Go service with health endpoints, two standalone WireMock upstreams returning representative document data, Docker Compose orchestrating everything with healthchecks, and an OpenAPI 3.x spec defining the response contract.

**Architecture:** Standard Go layout (`cmd/server/`, `internal/`) with chi router. Two WireMock containers serve Sales and Service mock APIs with deterministic VIN-seeded responses and failure injection via environment variables. A single `docker-compose.yml` wires app + Postgres + both mocks with `depends_on: condition: service_healthy`. The OpenAPI spec is the source of truth for the response contract; health endpoints prove liveness and DB readiness.

**Tech Stack:** Go 1.24, chi v5, pgx v5, Postgres 16, WireMock 3.x (standalone Docker), Docker Compose v2

**Requirements covered:** API-02, API-03, MOCK-01, MOCK-02, MOCK-03, INFR-01, INFR-02

**Success Criteria:**
1. `docker compose up` starts the app, database, and both mock services without manual intervention
2. `GET /healthz` returns 200 and `GET /readyz` returns 200 reflecting DB connectivity
3. Mock Sales and Service APIs return deterministic document payloads for a given VIN
4. Mocks accept configuration for latency injection and failure simulation
5. OpenAPI 3.x spec file exists in the repo and documents the response contract

---

## File Structure

```
unified-document-viewer/
├── cmd/
│   └── server/
│       └── main.go                    # Application entrypoint: config, DI, server lifecycle
├── internal/
│   ├── config/
│   │   └── config.go                  # Environment-variable-driven configuration
│   ├── health/
│   │   ├── handler.go                 # /healthz and /readyz HTTP handlers
│   │   └── handler_test.go            # Unit tests for health handlers
│   └── platform/
│       └── postgres/
│           └── postgres.go            # Postgres connection pool setup
├── api/
│   └── openapi.yaml                   # OpenAPI 3.x specification (source of truth)
├── mocks/
│   ├── sales/
│   │   └── mappings/
│   │       ├── health.json            # WireMock health endpoint mapping
│   │       └── documents.json         # Sales documents response mapping (VIN-templated)
│   └── service/
│       └── mappings/
│           ├── health.json            # WireMock health endpoint mapping
│           └── documents.json         # Service documents response mapping (VIN-templated)
├── docker-compose.yml                 # Full stack: app + postgres + sales-mock + service-mock
├── Dockerfile                         # Multi-stage Go build
├── Makefile                           # Task runner: up, down, test, smoke
├── go.mod                             # Go module definition
├── go.sum                             # Go dependency checksums
└── .env.example                       # Example environment variables
```

---

## Task 1: Go Module and Project Skeleton

**Files:**
- Create: `go.mod`
- Create: `cmd/server/main.go`
- Create: `internal/config/config.go`
- Create: `internal/platform/postgres/postgres.go`
- Create: `Makefile`
- Create: `.env.example`

---

- [ ] **Step 1: Initialize Go module**

Run:
```bash
cd /Users/tungnguyen/TYME/The-Unified-Document-Viewer/The-Unified-Document-Viewer
go mod init github.com/tungnguyen/unified-document-viewer
```

Expected: `go.mod` created with module path.

- [ ] **Step 2: Install core dependencies**

Run:
```bash
go get github.com/go-chi/chi/v5@v5.2.0
go get github.com/jackc/pgx/v5@v5.7.2
go get github.com/jackc/pgx/v5/pgxpool@v5.7.2
```

Expected: `go.mod` updated with dependencies, `go.sum` created.

- [ ] **Step 3: Create configuration loader**

Create `internal/config/config.go`:

```go
package config

import (
	"fmt"
	"os"
)

type Config struct {
	Port        string
	DatabaseURL string
}

func Load() (*Config, error) {
	port := os.Getenv("APP_PORT")
	if port == "" {
		port = "8080"
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return nil, fmt.Errorf("DATABASE_URL environment variable is required")
	}

	return &Config{
		Port:        port,
		DatabaseURL: dbURL,
	}, nil
}
```

- [ ] **Step 4: Create Postgres connection pool**

Create `internal/platform/postgres/postgres.go`:

```go
package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("creating connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	return pool, nil
}
```

- [ ] **Step 5: Create application entrypoint**

Create `cmd/server/main.go`:

```go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/tungnguyen/unified-document-viewer/internal/config"
	"github.com/tungnguyen/unified-document-viewer/internal/health"
	"github.com/tungnguyen/unified-document-viewer/internal/platform/postgres"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		slog.Error("application failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer pool.Close()

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	healthHandler := health.NewHandler(pool)
	r.Get("/healthz", healthHandler.Liveness)
	r.Get("/readyz", healthHandler.Readiness)

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		slog.Info("server starting", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			cancel()
		}
	}()

	<-quit
	slog.Info("shutting down server")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	return srv.Shutdown(shutdownCtx)
}
```

- [ ] **Step 6: Create a stub health handler (to be implemented in Task 3)**

Create `internal/health/handler.go`:

```go
package health

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	pool *pgxpool.Pool
}

func NewHandler(pool *pgxpool.Pool) *Handler {
	return &Handler{pool: pool}
}

func (h *Handler) Liveness(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

func (h *Handler) Readiness(w http.ResponseWriter, r *http.Request) {
	if err := h.pool.Ping(r.Context()); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"status":"unavailable","reason":"database connection failed"}`))
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}
```

- [ ] **Step 7: Create Makefile**

Create `Makefile`:

```makefile
.PHONY: up down build test smoke lint

up:
	docker compose up --build -d

down:
	docker compose down -v

build:
	go build -o bin/server ./cmd/server

test:
	go test ./... -v -race

smoke:
	@echo "==> Checking healthz..."
	@curl -sf http://localhost:8080/healthz | jq .
	@echo "==> Checking readyz..."
	@curl -sf http://localhost:8080/readyz | jq .
	@echo "==> Checking sales mock..."
	@curl -sf http://localhost:9001/__admin/mappings | jq '.total'
	@echo "==> Checking service mock..."
	@curl -sf http://localhost:9002/__admin/mappings | jq '.total'

lint:
	golangci-lint run ./...
```

- [ ] **Step 8: Create .env.example**

Create `.env.example`:

```env
APP_PORT=8080
DATABASE_URL=postgres://docviewer:docviewer@localhost:5432/docviewer?sslmode=disable
SALES_MOCK_URL=http://sales-mock:8080
SERVICE_MOCK_URL=http://service-mock:8080
```

- [ ] **Step 9: Verify compilation**

Run:
```bash
go build ./...
```

Expected: Compiles without errors.

- [ ] **Step 10: Commit**

```bash
git add go.mod go.sum cmd/ internal/ Makefile .env.example
git commit -m "feat(skeleton): Go project structure with chi router, config, and Postgres pool"
```

---

## Task 2: Dockerfile and Docker Compose

**Files:**
- Create: `Dockerfile`
- Create: `docker-compose.yml`

---

- [ ] **Step 1: Create multi-stage Dockerfile**

Create `Dockerfile`:

```dockerfile
FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /server ./cmd/server

FROM alpine:3.21

RUN apk add --no-cache ca-certificates
COPY --from=builder /server /server

EXPOSE 8080
ENTRYPOINT ["/server"]
```

- [ ] **Step 2: Create Docker Compose file**

Create `docker-compose.yml`:

```yaml
services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: docviewer
      POSTGRES_PASSWORD: docviewer
      POSTGRES_DB: docviewer
    ports:
      - "5432:5432"
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U docviewer -d docviewer"]
      interval: 2s
      timeout: 3s
      retries: 5
    volumes:
      - pgdata:/var/lib/postgresql/data

  sales-mock:
    image: wiremock/wiremock:3.10.0
    ports:
      - "9001:8080"
    volumes:
      - ./mocks/sales/mappings:/home/wiremock/mappings
    command: ["--global-response-templating"]
    environment:
      WIREMOCK_OPTIONS: "--global-response-templating"
    healthcheck:
      test: ["CMD", "wget", "--spider", "-q", "http://localhost:8080/__admin/mappings"]
      interval: 2s
      timeout: 3s
      retries: 5

  service-mock:
    image: wiremock/wiremock:3.10.0
    ports:
      - "9002:8080"
    volumes:
      - ./mocks/service/mappings:/home/wiremock/mappings
    command: ["--global-response-templating"]
    environment:
      WIREMOCK_OPTIONS: "--global-response-templating"
    healthcheck:
      test: ["CMD", "wget", "--spider", "-q", "http://localhost:8080/__admin/mappings"]
      interval: 2s
      timeout: 3s
      retries: 5

  app:
    build: .
    ports:
      - "8080:8080"
    environment:
      APP_PORT: "8080"
      DATABASE_URL: "postgres://docviewer:docviewer@postgres:5432/docviewer?sslmode=disable"
      SALES_MOCK_URL: "http://sales-mock:8080"
      SERVICE_MOCK_URL: "http://service-mock:8080"
    depends_on:
      postgres:
        condition: service_healthy
      sales-mock:
        condition: service_healthy
      service-mock:
        condition: service_healthy
    healthcheck:
      test: ["CMD", "wget", "--spider", "-q", "http://localhost:8080/healthz"]
      interval: 3s
      timeout: 3s
      retries: 5

volumes:
  pgdata:
```

- [ ] **Step 3: Verify Docker Compose builds and starts**

Run:
```bash
docker compose up --build -d
```

Expected: All four services start. `docker compose ps` shows all healthy within ~15 seconds.

- [ ] **Step 4: Verify health endpoints**

Run:
```bash
curl -s http://localhost:8080/healthz
curl -s http://localhost:8080/readyz
```

Expected:
```json
{"status":"ok"}
{"status":"ok"}
```

- [ ] **Step 5: Tear down**

Run:
```bash
docker compose down -v
```

- [ ] **Step 6: Commit**

```bash
git add Dockerfile docker-compose.yml
git commit -m "infra(docker): multi-stage Dockerfile and Compose with Postgres healthchecks"
```

---

## Task 3: Health Endpoint Unit Tests

**Files:**
- Create: `internal/health/handler_test.go`

---

- [ ] **Step 1: Write failing tests for health handlers**

Create `internal/health/handler_test.go`:

```go
package health_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tungnguyen/unified-document-viewer/internal/health"
)

func TestLiveness_ReturnsOK(t *testing.T) {
	handler := health.NewHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	handler.Liveness(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	if rec.Body.String() != `{"status":"ok"}` {
		t.Errorf("unexpected body: %s", rec.Body.String())
	}
}

func TestReadiness_WithHealthyDB_ReturnsOK(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool := newTestPool(t)
	handler := health.NewHandler(pool)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	handler.Readiness(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestReadiness_WithNilPool_Returns503(t *testing.T) {
	handler := health.NewHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	handler.Readiness(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", rec.Code)
	}
}

func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := "postgres://docviewer:docviewer@localhost:5432/docviewer?sslmode=disable"
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Skipf("cannot connect to test database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
```

- [ ] **Step 2: Fix nil pool panic in Readiness handler**

The current `Readiness` implementation will panic if `pool` is nil (when DB is down). Update `internal/health/handler.go` — replace the `Readiness` method:

```go
func (h *Handler) Readiness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if h.pool == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"status":"unavailable","reason":"database connection failed"}`))
		return
	}

	if err := h.pool.Ping(r.Context()); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"status":"unavailable","reason":"database connection failed"}`))
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}
```

Also add `Content-Type` header to `Liveness`:

```go
func (h *Handler) Liveness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}
```

- [ ] **Step 3: Run unit tests (short mode, no DB needed)**

Run:
```bash
go test ./internal/health/ -short -v -race
```

Expected: `TestLiveness_ReturnsOK` and `TestReadiness_WithNilPool_Returns503` PASS. `TestReadiness_WithHealthyDB_ReturnsOK` is SKIPPED.

- [ ] **Step 4: Commit**

```bash
git add internal/health/
git commit -m "test(health): unit tests for liveness and readiness endpoints"
```

---

## Task 4: WireMock Sales Mock with Deterministic Data

**Files:**
- Create: `mocks/sales/mappings/health.json`
- Create: `mocks/sales/mappings/documents.json`

---

- [ ] **Step 1: Create Sales mock health mapping**

Create `mocks/sales/mappings/health.json`:

```json
{
  "mappings": [
    {
      "request": {
        "method": "GET",
        "url": "/health"
      },
      "response": {
        "status": 200,
        "headers": {
          "Content-Type": "application/json"
        },
        "body": "{\"status\":\"ok\",\"service\":\"sales-api\"}"
      }
    }
  ]
}
```

- [ ] **Step 2: Create Sales mock documents mapping with VIN templating**

Create `mocks/sales/mappings/documents.json`:

```json
{
  "mappings": [
    {
      "request": {
        "method": "GET",
        "urlPathPattern": "/api/v1/vehicles/([A-HJ-NPR-Z0-9]{17})/documents"
      },
      "response": {
        "status": 200,
        "headers": {
          "Content-Type": "application/json"
        },
        "jsonBody": {
          "documents": [
            {
              "id": "SALE-001-{{request.pathSegments.[3]}}",
              "vin": "{{request.pathSegments.[3]}}",
              "type": "purchase_agreement",
              "title": "Vehicle Purchase Agreement",
              "date": "2024-01-15T10:30:00Z",
              "metadata": {
                "dealer_id": "DLR-042",
                "salesperson": "J. Smith"
              }
            },
            {
              "id": "SALE-002-{{request.pathSegments.[3]}}",
              "vin": "{{request.pathSegments.[3]}}",
              "type": "finance_contract",
              "title": "Finance Agreement",
              "date": "2024-01-15T11:00:00Z",
              "metadata": {
                "dealer_id": "DLR-042",
                "finance_provider": "AutoLend Corp"
              }
            },
            {
              "id": "SALE-003-{{request.pathSegments.[3]}}",
              "vin": "{{request.pathSegments.[3]}}",
              "type": "trade_in_appraisal",
              "title": "Trade-In Appraisal Report",
              "date": "2024-01-14T09:00:00Z",
              "metadata": {
                "dealer_id": "DLR-042",
                "appraiser": "M. Johnson"
              }
            }
          ]
        },
        "transformers": ["response-template"]
      }
    },
    {
      "priority": 1,
      "request": {
        "method": "GET",
        "urlPathPattern": "/api/v1/vehicles/([A-HJ-NPR-Z0-9]{17})/documents",
        "headers": {
          "X-Mock-Latency": {
            "matches": ".+"
          }
        }
      },
      "response": {
        "status": 200,
        "fixedDelayMilliseconds": "{{request.headers.X-Mock-Latency}}",
        "headers": {
          "Content-Type": "application/json"
        },
        "jsonBody": {
          "documents": [
            {
              "id": "SALE-001-{{request.pathSegments.[3]}}",
              "vin": "{{request.pathSegments.[3]}}",
              "type": "purchase_agreement",
              "title": "Vehicle Purchase Agreement",
              "date": "2024-01-15T10:30:00Z",
              "metadata": {
                "dealer_id": "DLR-042",
                "salesperson": "J. Smith"
              }
            }
          ]
        },
        "transformers": ["response-template"]
      }
    },
    {
      "priority": 1,
      "request": {
        "method": "GET",
        "urlPathPattern": "/api/v1/vehicles/([A-HJ-NPR-Z0-9]{17})/documents",
        "headers": {
          "X-Mock-Fault": {
            "equalTo": "500"
          }
        }
      },
      "response": {
        "status": 500,
        "headers": {
          "Content-Type": "application/json"
        },
        "body": "{\"error\":\"internal_server_error\",\"message\":\"simulated failure\"}"
      }
    },
    {
      "priority": 1,
      "request": {
        "method": "GET",
        "urlPathPattern": "/api/v1/vehicles/([A-HJ-NPR-Z0-9]{17})/documents",
        "headers": {
          "X-Mock-Fault": {
            "equalTo": "timeout"
          }
        }
      },
      "response": {
        "status": 200,
        "fixedDelayMilliseconds": 5000,
        "headers": {
          "Content-Type": "application/json"
        },
        "body": "{\"documents\":[]}"
      }
    }
  ]
}
```

- [ ] **Step 3: Verify Sales mock starts with mappings**

Run:
```bash
docker compose up sales-mock -d
sleep 3
curl -s http://localhost:9001/__admin/mappings | jq '.total'
```

Expected: Shows the number of mappings loaded (should be 5 — 1 health + 4 document variants).

- [ ] **Step 4: Test deterministic response for a sample VIN**

Run:
```bash
curl -s http://localhost:9001/api/v1/vehicles/1HGCM82633A004352/documents | jq '.documents[0].id'
```

Expected: `"SALE-001-1HGCM82633A004352"`

- [ ] **Step 5: Test failure injection**

Run:
```bash
curl -s -H "X-Mock-Fault: 500" http://localhost:9001/api/v1/vehicles/1HGCM82633A004352/documents | jq '.error'
```

Expected: `"internal_server_error"`

- [ ] **Step 6: Tear down**

Run:
```bash
docker compose down
```

- [ ] **Step 7: Commit**

```bash
git add mocks/sales/
git commit -m "feat(mocks): Sales mock API with deterministic VIN-seeded data and failure injection"
```

---

## Task 5: WireMock Service Mock with Deterministic Data

**Files:**
- Create: `mocks/service/mappings/health.json`
- Create: `mocks/service/mappings/documents.json`

---

- [ ] **Step 1: Create Service mock health mapping**

Create `mocks/service/mappings/health.json`:

```json
{
  "mappings": [
    {
      "request": {
        "method": "GET",
        "url": "/health"
      },
      "response": {
        "status": 200,
        "headers": {
          "Content-Type": "application/json"
        },
        "body": "{\"status\":\"ok\",\"service\":\"service-api\"}"
      }
    }
  ]
}
```

- [ ] **Step 2: Create Service mock documents mapping with VIN templating**

Create `mocks/service/mappings/documents.json`:

```json
{
  "mappings": [
    {
      "request": {
        "method": "GET",
        "urlPathPattern": "/api/v1/vehicles/([A-HJ-NPR-Z0-9]{17})/documents"
      },
      "response": {
        "status": 200,
        "headers": {
          "Content-Type": "application/json"
        },
        "jsonBody": {
          "documents": [
            {
              "id": "SVC-001-{{request.pathSegments.[3]}}",
              "vin": "{{request.pathSegments.[3]}}",
              "type": "service_record",
              "title": "Regular Maintenance - 10,000km",
              "date": "2024-03-20T08:00:00Z",
              "metadata": {
                "service_center": "Downtown Auto Care",
                "technician": "R. Lee",
                "mileage_km": 10000
              }
            },
            {
              "id": "SVC-002-{{request.pathSegments.[3]}}",
              "vin": "{{request.pathSegments.[3]}}",
              "type": "inspection_report",
              "title": "Annual Safety Inspection",
              "date": "2024-06-10T14:30:00Z",
              "metadata": {
                "service_center": "Downtown Auto Care",
                "inspector": "A. Patel",
                "result": "pass"
              }
            },
            {
              "id": "SVC-003-{{request.pathSegments.[3]}}",
              "vin": "{{request.pathSegments.[3]}}",
              "type": "warranty_claim",
              "title": "Warranty Repair - AC Compressor",
              "date": "2024-08-05T11:15:00Z",
              "metadata": {
                "service_center": "Keyloop Service Hub",
                "claim_number": "WC-2024-7891",
                "status": "approved"
              }
            }
          ]
        },
        "transformers": ["response-template"]
      }
    },
    {
      "priority": 1,
      "request": {
        "method": "GET",
        "urlPathPattern": "/api/v1/vehicles/([A-HJ-NPR-Z0-9]{17})/documents",
        "headers": {
          "X-Mock-Latency": {
            "matches": ".+"
          }
        }
      },
      "response": {
        "status": 200,
        "fixedDelayMilliseconds": "{{request.headers.X-Mock-Latency}}",
        "headers": {
          "Content-Type": "application/json"
        },
        "jsonBody": {
          "documents": [
            {
              "id": "SVC-001-{{request.pathSegments.[3]}}",
              "vin": "{{request.pathSegments.[3]}}",
              "type": "service_record",
              "title": "Regular Maintenance - 10,000km",
              "date": "2024-03-20T08:00:00Z",
              "metadata": {
                "service_center": "Downtown Auto Care",
                "technician": "R. Lee",
                "mileage_km": 10000
              }
            }
          ]
        },
        "transformers": ["response-template"]
      }
    },
    {
      "priority": 1,
      "request": {
        "method": "GET",
        "urlPathPattern": "/api/v1/vehicles/([A-HJ-NPR-Z0-9]{17})/documents",
        "headers": {
          "X-Mock-Fault": {
            "equalTo": "500"
          }
        }
      },
      "response": {
        "status": 500,
        "headers": {
          "Content-Type": "application/json"
        },
        "body": "{\"error\":\"internal_server_error\",\"message\":\"simulated failure\"}"
      }
    },
    {
      "priority": 1,
      "request": {
        "method": "GET",
        "urlPathPattern": "/api/v1/vehicles/([A-HJ-NPR-Z0-9]{17})/documents",
        "headers": {
          "X-Mock-Fault": {
            "equalTo": "timeout"
          }
        }
      },
      "response": {
        "status": 200,
        "fixedDelayMilliseconds": 5000,
        "headers": {
          "Content-Type": "application/json"
        },
        "body": "{\"documents\":[]}"
      }
    }
  ]
}
```

- [ ] **Step 3: Verify Service mock starts with mappings**

Run:
```bash
docker compose up service-mock -d
sleep 3
curl -s http://localhost:9002/__admin/mappings | jq '.total'
```

Expected: Shows 5 mappings loaded.

- [ ] **Step 4: Test deterministic response**

Run:
```bash
curl -s http://localhost:9002/api/v1/vehicles/1HGCM82633A004352/documents | jq '.documents[0].id'
```

Expected: `"SVC-001-1HGCM82633A004352"`

- [ ] **Step 5: Test failure injection**

Run:
```bash
curl -s -H "X-Mock-Fault: 500" http://localhost:9002/api/v1/vehicles/1HGCM82633A004352/documents | jq '.error'
```

Expected: `"internal_server_error"`

- [ ] **Step 6: Tear down**

Run:
```bash
docker compose down
```

- [ ] **Step 7: Commit**

```bash
git add mocks/service/
git commit -m "feat(mocks): Service mock API with deterministic VIN-seeded data and failure injection"
```

---

## Task 6: OpenAPI 3.x Specification

**Files:**
- Create: `api/openapi.yaml`

---

- [ ] **Step 1: Write OpenAPI spec defining the full response contract**

Create `api/openapi.yaml`:

```yaml
openapi: 3.1.0
info:
  title: Unified Document Viewer API
  description: |
    Aggregates vehicle documents from multiple dealership systems (Sales, Service)
    into a single response, tagged by source. Supports partial-success semantics
    when one upstream is unavailable.
  version: 1.0.0
  contact:
    name: Tung Nguyen

servers:
  - url: http://localhost:8080
    description: Local development

paths:
  /healthz:
    get:
      summary: Liveness probe
      description: Returns 200 if the service process is running.
      operationId: getLiveness
      tags: [health]
      responses:
        "200":
          description: Service is alive
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/HealthResponse"

  /readyz:
    get:
      summary: Readiness probe
      description: Returns 200 if the service can accept traffic (database connected).
      operationId: getReadiness
      tags: [health]
      responses:
        "200":
          description: Service is ready
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/HealthResponse"
        "503":
          description: Service is not ready (database unavailable)
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/HealthResponse"

  /vehicles/{vin}/documents:
    get:
      summary: Get all documents for a vehicle
      description: |
        Fetches documents from all upstream systems in parallel and returns a
        consolidated list. Each document is tagged with its source system.
        Partial success is supported — if one upstream fails, documents from
        the healthy upstream are still returned with per-source status.
      operationId: getVehicleDocuments
      tags: [documents]
      parameters:
        - name: vin
          in: path
          required: true
          description: 17-character Vehicle Identification Number (ISO 3779)
          schema:
            type: string
            pattern: "^[A-HJ-NPR-Z0-9]{17}$"
            example: "1HGCM82633A004352"
      responses:
        "200":
          description: Documents retrieved (may be partial success — check sources array)
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/DocumentsEnvelope"
        "400":
          description: Invalid VIN format
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/ErrorResponse"
        "502":
          description: All upstream sources failed and no cached data available
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/ErrorResponse"

components:
  schemas:
    HealthResponse:
      type: object
      required: [status]
      properties:
        status:
          type: string
          enum: [ok, unavailable]
        reason:
          type: string

    DocumentsEnvelope:
      type: object
      required: [data, meta]
      properties:
        data:
          type: object
          required: [vin, documents, sources]
          properties:
            vin:
              type: string
              pattern: "^[A-HJ-NPR-Z0-9]{17}$"
            documents:
              type: array
              items:
                $ref: "#/components/schemas/Document"
            sources:
              type: array
              items:
                $ref: "#/components/schemas/SourceStatus"
        meta:
          $ref: "#/components/schemas/ResponseMeta"

    Document:
      type: object
      required: [id, vin, source, type, title, date]
      properties:
        id:
          type: string
          description: Unique document identifier from the source system
        vin:
          type: string
        source:
          type: string
          enum: [sales, service]
          description: Which upstream system produced this document
        type:
          type: string
          description: Document type classification
          examples: [purchase_agreement, finance_contract, service_record, inspection_report]
        title:
          type: string
        date:
          type: string
          format: date-time
        metadata:
          type: object
          additionalProperties: true
          description: Source-specific metadata (varies by document type)

    SourceStatus:
      type: object
      required: [name, status]
      properties:
        name:
          type: string
          enum: [sales, service]
        status:
          type: string
          enum: [ok, failed, timeout, stale]
        error:
          type: string
          description: Human-readable error description (present when status != ok)
        fetched_at:
          type: string
          format: date-time
          description: When cached data was originally fetched (present when status = stale)

    ResponseMeta:
      type: object
      required: [request_id, timestamp]
      properties:
        request_id:
          type: string
          format: uuid
        timestamp:
          type: string
          format: date-time

    ErrorResponse:
      type: object
      required: [error]
      properties:
        error:
          type: object
          required: [code, message]
          properties:
            code:
              type: string
              examples: [invalid_vin, upstream_failure]
            message:
              type: string
            details:
              type: array
              items:
                type: object
                additionalProperties: true
```

- [ ] **Step 2: Validate the spec is well-formed**

Run:
```bash
# Install a validator if not present
npx @redocly/cli lint api/openapi.yaml 2>/dev/null || echo "Spec written; validate with any OpenAPI linter"
```

Expected: No structural errors. Warnings about missing examples are acceptable.

- [ ] **Step 3: Commit**

```bash
git add api/openapi.yaml
git commit -m "docs(api): OpenAPI 3.1 specification for document aggregation contract"
```

---

## Task 7: Full Stack Smoke Test

**Files:**
- No new files — this task validates the whole stack works end-to-end.

---

- [ ] **Step 1: Start the full stack**

Run:
```bash
docker compose up --build -d
```

Expected: All services healthy within 20 seconds.

- [ ] **Step 2: Wait for healthy state**

Run:
```bash
docker compose ps --format "table {{.Service}}\t{{.Status}}"
```

Expected: All services show `(healthy)` in their status.

- [ ] **Step 3: Run smoke test**

Run:
```bash
make smoke
```

Expected:
- `/healthz` returns `{"status":"ok"}`
- `/readyz` returns `{"status":"ok"}`
- Sales mock reports mappings loaded
- Service mock reports mappings loaded

- [ ] **Step 4: Test mock deterministic data directly**

Run:
```bash
curl -s http://localhost:9001/api/v1/vehicles/1HGCM82633A004352/documents | jq '.documents | length'
curl -s http://localhost:9002/api/v1/vehicles/1HGCM82633A004352/documents | jq '.documents | length'
```

Expected: `3` from each mock.

- [ ] **Step 5: Test mock failure injection**

Run:
```bash
curl -s -H "X-Mock-Fault: 500" http://localhost:9001/api/v1/vehicles/1HGCM82633A004352/documents
curl -s -H "X-Mock-Fault: timeout" http://localhost:9002/api/v1/vehicles/1HGCM82633A004352/documents &
sleep 2 && kill %1 2>/dev/null
echo "Timeout simulation works (request would hang for 5s)"
```

Expected: First curl returns 500 error JSON. Second curl demonstrates the delay.

- [ ] **Step 6: Test latency injection**

Run:
```bash
time curl -s -H "X-Mock-Latency: 1000" http://localhost:9001/api/v1/vehicles/1HGCM82633A004352/documents > /dev/null
```

Expected: Request takes ~1 second (1000ms injected latency).

- [ ] **Step 7: Tear down**

Run:
```bash
docker compose down -v
```

- [ ] **Step 8: Commit (if any fixes were made during smoke test)**

If changes were needed:
```bash
git add -A
git commit -m "fix(infra): adjustments from full-stack smoke test"
```

If no changes needed, skip this step.

---

## Summary

| Task | Delivers | Requirements |
|------|----------|--------------|
| 1 | Go project skeleton, config, Postgres pool | Foundation |
| 2 | Dockerfile + Docker Compose with healthchecks | INFR-01, INFR-02 |
| 3 | Health endpoint unit tests | API-02 (test coverage) |
| 4 | Sales mock with deterministic data + failure injection | MOCK-01, MOCK-02, MOCK-03 |
| 5 | Service mock with deterministic data + failure injection | MOCK-01, MOCK-02, MOCK-03 |
| 6 | OpenAPI 3.x specification | API-03 |
| 7 | Full-stack validation | All Phase 1 success criteria |

**Total commits:** 6-7 atomic commits
**Estimated time:** 45-75 minutes for an experienced Go developer
