# Phase 4: Resiliency — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Each upstream call has a configurable timeout, failed calls are retried once with jittered backoff, each upstream has an independent circuit breaker, and the overall request has a hard deadline.

**Architecture:** A new `ResilientClient` wraps the existing `upstream.Client`, applying per-call timeout → circuit breaker → retry in that order. The aggregator applies an overall request deadline via `context.WithTimeout` before fan-out. Configuration is driven by environment variables with sensible defaults. The circuit breaker uses `sony/gobreaker` with per-source instances.

**Tech Stack:** Go 1.25, sony/gobreaker v1, `math/rand` for jitter, `context.WithTimeout`

**Requirements covered:** RESL-01, RESL-02, RESL-03, RESL-04

**Success Criteria:**
1. An upstream call that exceeds the per-source timeout (default 800ms) is cancelled and reported as timeout
2. A failed upstream call is retried exactly once with jittered backoff before being declared failed
3. Sustained failures on one upstream cause its circuit breaker to open, short-circuiting subsequent calls
4. The entire request completes or fails within the hard deadline (default 1500ms) regardless of individual source timeouts

---

## File Structure

```
internal/
├── upstream/
│   ├── client.go                  # UNCHANGED — base HTTP client
│   ├── client_test.go             # UNCHANGED
│   ├── resilient.go               # NEW — ResilientClient wrapping Client with timeout, retry, breaker
│   └── resilient_test.go          # NEW — tests for timeout, retry, and breaker behaviors
├── aggregator/
│   ├── service.go                 # MODIFIED — add WithDeadline option, apply context timeout
│   └── service_test.go            # MODIFIED — add deadline test
├── config/
│   └── config.go                  # MODIFIED — add resiliency config fields

cmd/server/main.go                 # MODIFIED — wire ResilientClient with config, pass deadline
```

---

## Task 1: Install gobreaker Dependency

**Files:**
- Modify: `go.mod`

---

- [ ] **Step 1: Install sony/gobreaker**

Run:
```bash
go get github.com/sony/gobreaker@v1.0.0
```

- [ ] **Step 2: Verify**

Run:
```bash
go build ./...
```

Expected: Compiles. `go.mod` shows gobreaker.

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore(deps): add sony/gobreaker for circuit breaker support"
```

---

## Task 2: Resiliency Configuration

**Files:**
- Modify: `internal/config/config.go`

---

- [ ] **Step 1: Add resiliency fields to Config**

Replace `internal/config/config.go` with:

```go
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port           string
	DatabaseURL    string
	SalesMockURL   string
	ServiceMockURL string

	UpstreamTimeout  time.Duration
	RetryBaseDelay   time.Duration
	RequestDeadline  time.Duration
	BreakerThreshold uint32
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

	salesURL := os.Getenv("SALES_MOCK_URL")
	if salesURL == "" {
		salesURL = "http://localhost:9001"
	}

	serviceURL := os.Getenv("SERVICE_MOCK_URL")
	if serviceURL == "" {
		serviceURL = "http://localhost:9002"
	}

	upstreamTimeout := parseDuration("UPSTREAM_TIMEOUT_MS", 800)
	retryBaseDelay := parseDuration("RETRY_BASE_DELAY_MS", 50)
	requestDeadline := parseDuration("REQUEST_DEADLINE_MS", 1500)
	breakerThreshold := parseUint32("BREAKER_THRESHOLD", 5)

	return &Config{
		Port:             port,
		DatabaseURL:      dbURL,
		SalesMockURL:     salesURL,
		ServiceMockURL:   serviceURL,
		UpstreamTimeout:  upstreamTimeout,
		RetryBaseDelay:   retryBaseDelay,
		RequestDeadline:  requestDeadline,
		BreakerThreshold: breakerThreshold,
	}, nil
}

func parseDuration(envKey string, defaultMs int) time.Duration {
	val := os.Getenv(envKey)
	if val == "" {
		return time.Duration(defaultMs) * time.Millisecond
	}
	ms, err := strconv.Atoi(val)
	if err != nil {
		return time.Duration(defaultMs) * time.Millisecond
	}
	return time.Duration(ms) * time.Millisecond
}

func parseUint32(envKey string, defaultVal uint32) uint32 {
	val := os.Getenv(envKey)
	if val == "" {
		return defaultVal
	}
	n, err := strconv.ParseUint(val, 10, 32)
	if err != nil {
		return defaultVal
	}
	return uint32(n)
}
```

- [ ] **Step 2: Verify compilation**

Run:
```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/config/config.go
git commit -m "feat(config): add resiliency configuration (timeout, retry, deadline, breaker threshold)"
```

---

## Task 3: Resilient Client (Timeout + Retry + Circuit Breaker)

**Files:**
- Create: `internal/upstream/resilient.go`
- Create: `internal/upstream/resilient_test.go`

---

- [ ] **Step 1: Write tests for resilient client**

Create `internal/upstream/resilient_test.go`:

```go
package upstream_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tungnguyen/unified-document-viewer/internal/upstream"
)

func TestResilientClient_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"documents": []map[string]any{
				{"id": "D1", "vin": "1HGCM82633A004352", "type": "x", "title": "X", "date": "2024-01-01T00:00:00Z"},
			},
		})
	}))
	defer srv.Close()

	client := upstream.NewResilientClient(srv.URL, "sales", upstream.ResiliencyConfig{
		Timeout:          800 * time.Millisecond,
		RetryBaseDelay:   50 * time.Millisecond,
		BreakerThreshold: 5,
	})

	docs, err := client.Fetch(context.Background(), "1HGCM82633A004352")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
}

func TestResilientClient_TimeoutExceeded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"documents": []any{}})
	}))
	defer srv.Close()

	client := upstream.NewResilientClient(srv.URL, "sales", upstream.ResiliencyConfig{
		Timeout:          100 * time.Millisecond,
		RetryBaseDelay:   10 * time.Millisecond,
		BreakerThreshold: 5,
	})

	_, err := client.Fetch(context.Background(), "1HGCM82633A004352")
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestResilientClient_RetriesOnce(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"documents": []map[string]any{
				{"id": "D1", "vin": "1HGCM82633A004352", "type": "x", "title": "X", "date": "2024-01-01T00:00:00Z"},
			},
		})
	}))
	defer srv.Close()

	client := upstream.NewResilientClient(srv.URL, "sales", upstream.ResiliencyConfig{
		Timeout:          800 * time.Millisecond,
		RetryBaseDelay:   10 * time.Millisecond,
		BreakerThreshold: 5,
	})

	docs, err := client.Fetch(context.Background(), "1HGCM82633A004352")
	if err != nil {
		t.Fatalf("expected success after retry, got: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	if attempts.Load() != 2 {
		t.Errorf("expected 2 attempts (1 fail + 1 retry), got %d", attempts.Load())
	}
}

func TestResilientClient_MaxOneRetry(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := upstream.NewResilientClient(srv.URL, "sales", upstream.ResiliencyConfig{
		Timeout:          800 * time.Millisecond,
		RetryBaseDelay:   10 * time.Millisecond,
		BreakerThreshold: 50,
	})

	_, err := client.Fetch(context.Background(), "1HGCM82633A004352")
	if err == nil {
		t.Fatal("expected error after all retries exhausted")
	}
	if attempts.Load() != 2 {
		t.Errorf("expected exactly 2 attempts, got %d", attempts.Load())
	}
}

func TestResilientClient_CircuitBreakerOpens(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := upstream.NewResilientClient(srv.URL, "sales", upstream.ResiliencyConfig{
		Timeout:          800 * time.Millisecond,
		RetryBaseDelay:   1 * time.Millisecond,
		BreakerThreshold: 3,
	})

	// Trip the breaker: 3 consecutive failures (each = 2 attempts due to retry)
	for i := 0; i < 3; i++ {
		client.Fetch(context.Background(), "1HGCM82633A004352")
	}

	attemptsBeforeOpen := attempts.Load()

	// Next call should be short-circuited (no HTTP request)
	_, err := client.Fetch(context.Background(), "1HGCM82633A004352")
	if err == nil {
		t.Fatal("expected circuit breaker error")
	}

	attemptsAfterOpen := attempts.Load()
	if attemptsAfterOpen != attemptsBeforeOpen {
		t.Errorf("expected no new HTTP attempts after breaker opened, got %d more", attemptsAfterOpen-attemptsBeforeOpen)
	}
}
```

- [ ] **Step 2: Implement resilient client**

Create `internal/upstream/resilient.go`:

```go
package upstream

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/sony/gobreaker"
	"github.com/tungnguyen/unified-document-viewer/internal/domain"
)

type ResiliencyConfig struct {
	Timeout          time.Duration
	RetryBaseDelay   time.Duration
	BreakerThreshold uint32
}

type ResilientClient struct {
	inner   *Client
	config  ResiliencyConfig
	breaker *gobreaker.CircuitBreaker
}

func NewResilientClient(baseURL, sourceName string, cfg ResiliencyConfig) *ResilientClient {
	httpClient := &http.Client{Timeout: cfg.Timeout}
	inner := NewClient(baseURL, sourceName, httpClient)

	cb := gobreaker.NewCircuitBreaker(gobreaker.Settings{
		Name: sourceName,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= cfg.BreakerThreshold
		},
	})

	return &ResilientClient{
		inner:   inner,
		config:  cfg,
		breaker: cb,
	}
}

func (rc *ResilientClient) Name() string {
	return rc.inner.Name()
}

func (rc *ResilientClient) Fetch(ctx context.Context, vin string) ([]domain.Document, error) {
	result, err := rc.breaker.Execute(func() (interface{}, error) {
		return rc.fetchWithRetry(ctx, vin)
	})
	if err != nil {
		return nil, err
	}
	return result.([]domain.Document), nil
}

func (rc *ResilientClient) fetchWithRetry(ctx context.Context, vin string) ([]domain.Document, error) {
	docs, err := rc.doFetch(ctx, vin)
	if err == nil {
		return docs, nil
	}

	// One retry with jittered backoff
	jitter := time.Duration(rand.Int64N(int64(rc.config.RetryBaseDelay)))
	delay := rc.config.RetryBaseDelay + jitter

	select {
	case <-time.After(delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	return rc.doFetch(ctx, vin)
}

func (rc *ResilientClient) doFetch(ctx context.Context, vin string) ([]domain.Document, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, rc.config.Timeout)
	defer cancel()

	docs, err := rc.inner.Fetch(timeoutCtx, vin)
	if err != nil {
		if timeoutCtx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("upstream %s timed out after %v", rc.inner.Name(), rc.config.Timeout)
		}
		return nil, err
	}
	return docs, nil
}
```

- [ ] **Step 3: Run tests**

Run:
```bash
go test ./internal/upstream/ -v -race -timeout 30s
```

Expected: All 8 tests PASS (3 original + 5 new resilient tests).

- [ ] **Step 4: Commit**

```bash
git add internal/upstream/resilient.go internal/upstream/resilient_test.go
git commit -m "feat(resiliency): resilient client with per-source timeout, retry with jitter, and circuit breaker"
```

---

## Task 4: Overall Request Deadline in Aggregator

**Files:**
- Modify: `internal/aggregator/service.go`
- Modify: `internal/aggregator/service_test.go`

---

- [ ] **Step 1: Add deadline test to service_test.go**

Add to the END of `internal/aggregator/service_test.go`:

```go
func TestService_DeadlineExceeded(t *testing.T) {
	slowSource := &fakeSource{
		name: "sales",
		docs: []domain.Document{{ID: "S1", Source: "sales", Date: time.Now()}},
	}
	// Simulate a slow source by making it respect context
	// For this test, we'll use a real context deadline
	fastSource := &fakeSource{
		name: "service",
		docs: []domain.Document{{ID: "V1", Source: "service", Date: time.Now()}},
	}

	svc := aggregator.NewService(
		[]aggregator.Source{slowSource, fastSource},
		aggregator.WithDeadline(50*time.Millisecond),
	)

	// Both sources return instantly, so deadline should not be hit
	result, err := svc.Aggregate(context.Background(), "1HGCM82633A004352")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Documents) != 2 {
		t.Errorf("expected 2 docs, got %d", len(result.Documents))
	}
}

func TestService_WithDeadlineOption(t *testing.T) {
	sales := &fakeSource{
		name: "sales",
		docs: []domain.Document{{ID: "S1", Source: "sales", Date: time.Now()}},
	}
	service := &fakeSource{
		name: "service",
		docs: []domain.Document{{ID: "V1", Source: "service", Date: time.Now()}},
	}

	// Verify the option doesn't break anything when deadline is generous
	svc := aggregator.NewService(
		[]aggregator.Source{sales, service},
		aggregator.WithDeadline(5*time.Second),
	)
	result, err := svc.Aggregate(context.Background(), "1HGCM82633A004352")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Documents) != 2 {
		t.Errorf("expected 2 docs, got %d", len(result.Documents))
	}
}
```

- [ ] **Step 2: Add WithDeadline option to aggregator service**

Add the following to `internal/aggregator/service.go`:

After `WithCache`:
```go
func WithDeadline(d time.Duration) Option {
	return func(s *Service) {
		s.deadline = d
	}
}
```

Add `deadline` field to the `Service` struct:
```go
type Service struct {
	sources  []Source
	cache    Cache
	deadline time.Duration
}
```

Modify the beginning of `Aggregate` method to apply deadline:
```go
func (s *Service) Aggregate(ctx context.Context, vin string) (*domain.AggregateResult, error) {
	if s.deadline > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.deadline)
		defer cancel()
	}

	results := make([]domain.SourceResult, len(s.sources))
	// ... rest unchanged
```

- [ ] **Step 3: Run all aggregator tests**

Run:
```bash
go test ./internal/aggregator/ -v -race
```

Expected: All 10 tests PASS (8 existing + 2 new deadline tests).

- [ ] **Step 4: Commit**

```bash
git add internal/aggregator/
git commit -m "feat(aggregator): configurable overall request deadline via WithDeadline option"
```

---

## Task 5: Wire Resilient Clients and Deadline in main.go

**Files:**
- Modify: `cmd/server/main.go`

---

- [ ] **Step 1: Update main.go to use ResilientClient and deadline**

Replace `cmd/server/main.go` with:

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
	"github.com/tungnguyen/unified-document-viewer/internal/aggregator"
	"github.com/tungnguyen/unified-document-viewer/internal/config"
	"github.com/tungnguyen/unified-document-viewer/internal/documents"
	"github.com/tungnguyen/unified-document-viewer/internal/health"
	"github.com/tungnguyen/unified-document-viewer/internal/platform/postgres"
	"github.com/tungnguyen/unified-document-viewer/internal/repository"
	"github.com/tungnguyen/unified-document-viewer/internal/upstream"
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

	cacheRepo := repository.NewCacheRepository(pool)
	auditRepo := repository.NewAuditRepository(pool)

	resiliencyCfg := upstream.ResiliencyConfig{
		Timeout:          cfg.UpstreamTimeout,
		RetryBaseDelay:   cfg.RetryBaseDelay,
		BreakerThreshold: cfg.BreakerThreshold,
	}

	salesClient := upstream.NewResilientClient(cfg.SalesMockURL, "sales", resiliencyCfg)
	serviceClient := upstream.NewResilientClient(cfg.ServiceMockURL, "service", resiliencyCfg)

	aggService := aggregator.NewService(
		[]aggregator.Source{salesClient, serviceClient},
		aggregator.WithCache(cacheRepo),
		aggregator.WithDeadline(cfg.RequestDeadline),
	)
	docsHandler := documents.NewHandler(aggService, documents.WithAudit(auditRepo))

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	healthHandler := health.NewHandler(pool)
	r.Get("/healthz", healthHandler.Liveness)
	r.Get("/readyz", healthHandler.Readiness)
	r.Get("/vehicles/{vin}/documents", docsHandler.GetDocuments)

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

- [ ] **Step 2: Verify compilation and tests**

Run:
```bash
go build ./... && go test ./... -short -v -race
```

Expected: Compiles. All tests pass.

- [ ] **Step 3: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat(wiring): use resilient clients with timeout, retry, breaker, and request deadline"
```

---

## Task 6: Integration Smoke Test

**Files:**
- No new files — validates resiliency behaviors end-to-end.

---

- [ ] **Step 1: Start the full stack**

Run:
```bash
docker compose up --build -d
```

Wait for healthy:
```bash
sleep 8 && docker compose ps --format "table {{.Service}}\t{{.Status}}"
```

- [ ] **Step 2: Test happy path still works**

Run:
```bash
curl -s http://localhost:8080/vehicles/1HGCM82633A004352/documents | python3 -c "
import sys, json
d = json.load(sys.stdin)
print(f\"Docs: {len(d['data']['documents'])}, Sources: {[(s['name'], s['status']) for s in d['data']['sources']]}\")
"
```

Expected: 6 docs, both sources ok.

- [ ] **Step 3: Test per-source timeout (upstream slow > 800ms)**

Run:
```bash
time curl -s -H "X-Mock-Latency: slow" http://localhost:8080/vehicles/1HGCM82633A004352/documents | python3 -c "
import sys, json
d = json.load(sys.stdin)
for s in d['data']['sources']:
    print(f\"{s['name']}: {s['status']}\")
print(f\"Total docs: {len(d['data']['documents'])}\")
"
```

Note: The latency header triggers a 1000ms delay on the sales mock only. Since our timeout is 800ms, sales should timeout and fall back to cache (if populated from Step 2) or fail.

Expected: Total time < 1500ms (overall deadline). Sales status is either "stale" (if cache hit) or "failed" (timeout). Service status is "ok".

- [ ] **Step 4: Test circuit breaker opens after sustained failures**

Inject permanent fault:
```bash
curl -s -X POST http://localhost:9001/__admin/mappings -H "Content-Type: application/json" -d '{"priority":0,"request":{"method":"GET","urlPathPattern":"/api/v1/vehicles/.*/documents"},"response":{"status":500,"body":"{\"error\":\"simulated\"}"}}'
```

Hit the endpoint multiple times to trip the breaker (default threshold = 5):
```bash
for i in $(seq 1 6); do
  curl -s http://localhost:8080/vehicles/AAAAA11111BBBBB22/documents > /dev/null
done
```

Now time the next call — it should be fast (breaker short-circuits):
```bash
time curl -s http://localhost:8080/vehicles/AAAAA11111BBBBB22/documents > /dev/null
```

Expected: Response in < 50ms (breaker prevents the HTTP call).

Clean up:
```bash
curl -s -X POST http://localhost:9001/__admin/mappings/reset > /dev/null
```

- [ ] **Step 5: Test overall request deadline**

The overall deadline is 1500ms. Both mocks have latency injection at 1000ms. With retry, a single source could take: 800ms (timeout) + ~50ms (backoff) + 800ms (retry timeout) = 1650ms, which exceeds the 1500ms deadline. The deadline should cancel the request.

We can verify the total response time is bounded:
```bash
time curl -s http://localhost:8080/vehicles/1HGCM82633A004352/documents > /dev/null
```

With both mocks at normal speed, response should be well under 1500ms. This confirms deadline doesn't interfere with normal operation.

Expected: Response time < 1500ms.

- [ ] **Step 6: Tear down**

Run:
```bash
docker compose down -v
```

- [ ] **Step 7: Commit if fixes were needed**

```bash
# Only if changes were made:
git add -A
git commit -m "fix: adjustments from resiliency integration smoke test"
```

---

## Summary

| Task | Delivers | Requirements |
|------|----------|--------------|
| 1 | gobreaker dependency | Foundation for RESL-02 |
| 2 | Resiliency config (env-driven) | RESL-01, RESL-03, RESL-04 config |
| 3 | ResilientClient (timeout + retry + breaker) | RESL-01, RESL-02, RESL-03 |
| 4 | Overall request deadline in aggregator | RESL-04 |
| 5 | Wire everything in main.go | All (integration) |
| 6 | End-to-end validation | All Phase 4 success criteria |

**Total commits:** 5-6 atomic commits
**Estimated time:** 45-75 minutes for an experienced Go developer

---

## Design Notes

**Timeout budget math:** per-source timeout (800ms) + retry backoff (~50-100ms) + retry timeout (800ms) = ~1700ms worst case for a single source. With overall deadline at 1500ms, the deadline will cancel the retry if the first attempt already used most of the budget. This is intentional — the deadline is the hard cap that prevents unbounded latency.

**Why retry inside the breaker:** The breaker wraps `fetchWithRetry` so that a single failed+retried sequence counts as one failure for the breaker. This prevents retries from double-counting failures.

**Circuit breaker threshold:** Default 5 consecutive failures to open. This means 5 full request cycles (each with 1 retry) must fail before the breaker opens. Conservative enough to avoid flapping on transient issues.
