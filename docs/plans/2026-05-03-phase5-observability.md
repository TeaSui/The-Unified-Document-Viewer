# Phase 5: Observability — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** All logs are structured JSON with request correlation, OpenTelemetry tracing shows parent/child spans across fan-out, Prometheus metrics expose RED method per endpoint and per upstream, and the observability stack runs in Docker Compose.

**Architecture:** A new `internal/observability/` package provides OTel tracer/meter initialization and a Prometheus HTTP handler. Tracing middleware wraps the chi router to create parent spans; the resilient client creates child spans per upstream call. Prometheus metrics use the OTel metrics SDK with a Prometheus exporter — one histogram for HTTP requests (RED) and per-upstream histograms/counters. Structured logging adds trace_id and masked VIN to the existing slog handler via a custom middleware that enriches the context. Jaeger and Prometheus run in Docker Compose.

**Tech Stack:** Go 1.25, OpenTelemetry Go SDK (traces + metrics), Prometheus exporter, OTLP exporter for traces, Jaeger (all-in-one), Prometheus

**Requirements covered:** OBSV-01, OBSV-02, OBSV-03, OBSV-04

**Success Criteria:**
1. Application logs are structured JSON containing request_id, masked VIN (last 6 chars), and trace_id
2. Jaeger displays a parent span for each request with child spans for each upstream call
3. Prometheus exposes request rate, error rate, and duration histogram per endpoint
4. Per-upstream metrics (latency histogram, success/failure/timeout counts, circuit breaker state) are queryable in Prometheus

---

## File Structure

```
internal/
├── observability/
│   ├── tracing.go                 # NEW — OTel TracerProvider init (OTLP exporter to Jaeger)
│   ├── metrics.go                 # NEW — OTel MeterProvider init (Prometheus exporter)
│   └── middleware.go              # NEW — chi middleware: tracing span + structured log context
├── upstream/
│   └── resilient.go               # MODIFIED — add child tracing span + metrics recording
├── documents/
│   └── handler.go                 # MODIFIED — structured log with masked VIN + trace_id

cmd/server/main.go                 # MODIFIED — init OTel providers, mount /metrics, shutdown
docker-compose.yml                 # MODIFIED — add jaeger + prometheus services
observability/
├── otel-collector-config.yaml     # NOT NEEDED — direct OTLP to Jaeger
└── prometheus.yml                 # NEW — Prometheus scrape config
```

---

## Task 1: Install OTel Dependencies

**Files:**
- Modify: `go.mod`

---

- [ ] **Step 1: Install OpenTelemetry packages**

Run:
```bash
go get go.opentelemetry.io/otel@v1.34.0
go get go.opentelemetry.io/otel/sdk@v1.34.0
go get go.opentelemetry.io/otel/trace@v1.34.0
go get go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc@v1.34.0
go get go.opentelemetry.io/otel/sdk/metric@v1.34.0
go get go.opentelemetry.io/otel/exporters/prometheus@v0.56.0
go get github.com/prometheus/client_golang/prometheus/promhttp@v1.20.5
```

- [ ] **Step 2: Verify compilation**

Run:
```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore(deps): add OpenTelemetry SDK, OTLP exporter, and Prometheus exporter"
```

---

## Task 2: OTel Tracing Provider

**Files:**
- Create: `internal/observability/tracing.go`

---

- [ ] **Step 1: Create tracing provider initialization**

Create `internal/observability/tracing.go`:

```go
package observability

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

func InitTracer(ctx context.Context, jaegerEndpoint string) (*sdktrace.TracerProvider, error) {
	if jaegerEndpoint == "" {
		jaegerEndpoint = "localhost:4317"
	}

	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(jaegerEndpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("creating OTLP trace exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String("unified-document-viewer"),
			semconv.ServiceVersionKey.String("1.0.0"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("creating resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	return tp, nil
}
```

- [ ] **Step 2: Verify compilation**

Run:
```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/observability/tracing.go
git commit -m "feat(observability): OTel tracing provider with OTLP gRPC exporter"
```

---

## Task 3: OTel Metrics Provider with Prometheus Exporter

**Files:**
- Create: `internal/observability/metrics.go`

---

- [ ] **Step 1: Create metrics provider**

Create `internal/observability/metrics.go`:

```go
package observability

import (
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

func InitMeter() (*sdkmetric.MeterProvider, http.Handler, error) {
	exporter, err := prometheus.New()
	if err != nil {
		return nil, nil, fmt.Errorf("creating Prometheus exporter: %w", err)
	}

	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	otel.SetMeterProvider(mp)

	handler := promhttp.Handler()
	return mp, handler, nil
}
```

- [ ] **Step 2: Verify compilation**

Run:
```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/observability/metrics.go
git commit -m "feat(observability): OTel metrics provider with Prometheus exporter"
```

---

## Task 4: Tracing and Logging Middleware

**Files:**
- Create: `internal/observability/middleware.go`

---

- [ ] **Step 1: Create chi middleware for tracing and structured logging**

Create `internal/observability/middleware.go`:

```go
package observability

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("unified-document-viewer")

func Tracer() trace.Tracer {
	return tracer
}

func TracingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), r.Method+" "+r.URL.Path)
		defer span.End()

		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r.WithContext(ctx))

		span.SetAttributes(
			attribute.Int("http.status_code", ww.Status()),
			attribute.String("http.method", r.Method),
			attribute.String("http.route", r.URL.Path),
		)
	})
}

func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		next.ServeHTTP(ww, r.WithContext(r.Context()))

		requestID := middleware.GetReqID(r.Context())
		spanCtx := trace.SpanFromContext(r.Context()).SpanContext()
		traceID := spanCtx.TraceID().String()

		slog.Info("request completed",
			"request_id", requestID,
			"trace_id", traceID,
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

func MaskVIN(vin string) string {
	if len(vin) <= 6 {
		return vin
	}
	return "***" + vin[len(vin)-6:]
}
```

- [ ] **Step 2: Verify compilation**

Run:
```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/observability/middleware.go
git commit -m "feat(observability): tracing and logging middleware with request correlation"
```

---

## Task 5: Instrument Upstream Client with Tracing and Metrics

**Files:**
- Modify: `internal/upstream/resilient.go`

---

- [ ] **Step 1: Add tracing spans and metrics to the resilient client**

Replace `internal/upstream/resilient.go` with:

```go
package upstream

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/sony/gobreaker"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"github.com/tungnguyen/unified-document-viewer/internal/domain"
)

var (
	upstreamTracer  = otel.Tracer("upstream")
	upstreamMeter   = otel.Meter("upstream")
	upstreamLatency metric.Float64Histogram
	upstreamCalls   metric.Int64Counter
)

func init() {
	var err error
	upstreamLatency, err = upstreamMeter.Float64Histogram("upstream.request.duration",
		metric.WithDescription("Upstream request duration in seconds"),
		metric.WithUnit("s"),
	)
	if err != nil {
		panic(err)
	}
	upstreamCalls, err = upstreamMeter.Int64Counter("upstream.request.total",
		metric.WithDescription("Total upstream requests"),
	)
	if err != nil {
		panic(err)
	}
}

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
	ctx, span := upstreamTracer.Start(ctx, "upstream."+rc.inner.Name(),
		trace.WithAttributes(
			attribute.String("upstream.source", rc.inner.Name()),
			attribute.String("upstream.vin_suffix", vinSuffix(vin)),
		),
	)
	defer span.End()

	start := time.Now()

	result, err := rc.breaker.Execute(func() (interface{}, error) {
		return rc.fetchWithRetry(ctx, vin)
	})

	duration := time.Since(start).Seconds()
	outcome := "ok"
	if err != nil {
		outcome = classifyError(err)
		span.SetAttributes(attribute.String("upstream.outcome", outcome))
		span.RecordError(err)
	} else {
		span.SetAttributes(attribute.String("upstream.outcome", "ok"))
	}

	attrs := metric.WithAttributes(
		attribute.String("source", rc.inner.Name()),
		attribute.String("outcome", outcome),
	)
	upstreamLatency.Record(ctx, duration, attrs)
	upstreamCalls.Add(ctx, 1, attrs)

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

func classifyError(err error) string {
	if err == gobreaker.ErrOpenState || err == gobreaker.ErrTooManyRequests {
		return "circuit_open"
	}
	if err == context.DeadlineExceeded {
		return "timeout"
	}
	return "failed"
}

func vinSuffix(vin string) string {
	if len(vin) <= 6 {
		return vin
	}
	return vin[len(vin)-6:]
}
```

- [ ] **Step 2: Run existing tests to verify nothing broke**

Run:
```bash
go test ./internal/upstream/ -v -race -timeout 30s
```

Expected: All 8 tests PASS (unchanged behavior, just added instrumentation).

- [ ] **Step 3: Commit**

```bash
git add internal/upstream/resilient.go
git commit -m "feat(observability): instrument upstream client with tracing spans and Prometheus metrics"
```

---

## Task 6: Enhance Handler with Structured Logging

**Files:**
- Modify: `internal/documents/handler.go`

---

- [ ] **Step 1: Add masked VIN and trace_id to handler logs**

Replace `internal/documents/handler.go` with:

```go
package documents

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/otel/trace"
	"github.com/tungnguyen/unified-document-viewer/internal/domain"
	"github.com/tungnguyen/unified-document-viewer/internal/observability"
	"github.com/tungnguyen/unified-document-viewer/internal/vin"
)

type Aggregator interface {
	Aggregate(ctx context.Context, vinStr string) (*domain.AggregateResult, error)
}

type Audit interface {
	Insert(ctx context.Context, entry domain.AuditEntry) error
}

type HandlerOption func(*Handler)

func WithAudit(audit Audit) HandlerOption {
	return func(h *Handler) {
		h.audit = audit
	}
}

type Handler struct {
	aggregator Aggregator
	audit      Audit
}

func NewHandler(aggregator Aggregator, opts ...HandlerOption) *Handler {
	h := &Handler{aggregator: aggregator}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

func (h *Handler) GetDocuments(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	w.Header().Set("Content-Type", "application/json")

	vinParam := chi.URLParam(r, "vin")
	if err := vin.Validate(vinParam); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(domain.ErrorResponse{
			Error: domain.ErrorDetail{
				Code:    "invalid_vin",
				Message: err.Error(),
			},
		})
		return
	}

	result, err := h.aggregator.Aggregate(r.Context(), vinParam)

	requestID := middleware.GetReqID(r.Context())
	traceID := trace.SpanFromContext(r.Context()).SpanContext().TraceID().String()
	maskedVIN := observability.MaskVIN(vinParam)

	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(domain.ErrorResponse{
			Error: domain.ErrorDetail{
				Code:    "upstream_failure",
				Message: "all upstream sources failed",
			},
		})
		slog.Warn("aggregation failed",
			"request_id", requestID,
			"trace_id", traceID,
			"vin", maskedVIN,
			"duration_ms", time.Since(start).Milliseconds(),
		)
		h.writeAudit(r.Context(), requestID, vinParam, http.StatusBadGateway, start, nil)
		return
	}

	envelope := domain.DocumentsEnvelope{
		Data: domain.DocumentsData{
			VIN:       result.VIN,
			Documents: result.Documents,
			Sources:   result.Sources,
		},
		Meta: domain.ResponseMeta{
			RequestID: requestID,
			Timestamp: time.Now().UTC(),
		},
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(envelope)

	slog.Info("documents served",
		"request_id", requestID,
		"trace_id", traceID,
		"vin", maskedVIN,
		"doc_count", len(result.Documents),
		"sources", sourcesSummary(result.Sources),
		"duration_ms", time.Since(start).Milliseconds(),
	)
	h.writeAudit(r.Context(), requestID, vinParam, http.StatusOK, start, result.Sources)
}

func (h *Handler) writeAudit(ctx context.Context, requestID, vinStr string, status int, start time.Time, outcomes []domain.SourceStatus) {
	if h.audit == nil {
		return
	}

	entry := domain.AuditEntry{
		RequestID:  requestID,
		VIN:        vinStr,
		HTTPStatus: status,
		DurationMs: int(time.Since(start).Milliseconds()),
		Outcomes:   outcomes,
	}

	if err := h.audit.Insert(ctx, entry); err != nil {
		slog.Warn("audit write failed", "error", err, "request_id", requestID)
	}
}

func sourcesSummary(sources []domain.SourceStatus) string {
	result := ""
	for i, s := range sources {
		if i > 0 {
			result += ","
		}
		result += s.Name + ":" + s.Status
	}
	return result
}
```

- [ ] **Step 2: Run handler tests**

Run:
```bash
go test ./internal/documents/ -v -race
```

Expected: All 5 tests PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/documents/handler.go
git commit -m "feat(observability): structured logging with request_id, trace_id, and masked VIN"
```

---

## Task 7: Wire Observability in main.go and Add HTTP Metrics Middleware

**Files:**
- Modify: `cmd/server/main.go`
- Modify: `internal/config/config.go`

---

- [ ] **Step 1: Add OTEL_EXPORTER_OTLP_ENDPOINT to config**

Add to `internal/config/config.go` struct and Load():

Add field after BreakerThreshold:
```go
	OTLPEndpoint string
```

Add parsing in Load() before the return:
```go
	otlpEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if otlpEndpoint == "" {
		otlpEndpoint = "localhost:4317"
	}
```

And include in the return:
```go
	OTLPEndpoint: otlpEndpoint,
```

- [ ] **Step 2: Update main.go with observability wiring**

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
	"github.com/tungnguyen/unified-document-viewer/internal/observability"
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

	tp, err := observability.InitTracer(ctx, cfg.OTLPEndpoint)
	if err != nil {
		slog.Warn("tracing initialization failed, continuing without tracing", "error", err)
	} else {
		defer func() { _ = tp.Shutdown(ctx) }()
	}

	mp, metricsHandler, err := observability.InitMeter()
	if err != nil {
		return fmt.Errorf("initializing metrics: %w", err)
	}
	defer func() { _ = mp.Shutdown(ctx) }()

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
	r.Use(observability.TracingMiddleware)
	r.Use(observability.LoggingMiddleware)
	r.Use(middleware.Recoverer)

	healthHandler := health.NewHandler(pool)
	r.Get("/healthz", healthHandler.Liveness)
	r.Get("/readyz", healthHandler.Readiness)
	r.Get("/vehicles/{vin}/documents", docsHandler.GetDocuments)
	r.Handle("/metrics", metricsHandler)

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

- [ ] **Step 3: Verify compilation and tests**

Run:
```bash
go build ./... && go test ./... -short -v -race
```

Expected: Compiles. All tests pass.

- [ ] **Step 4: Commit**

```bash
git add cmd/server/main.go internal/config/config.go
git commit -m "feat(observability): wire OTel providers, metrics endpoint, and middleware in main"
```

---

## Task 8: Docker Compose — Jaeger and Prometheus

**Files:**
- Modify: `docker-compose.yml`
- Create: `observability/prometheus.yml`

---

- [ ] **Step 1: Create Prometheus scrape config**

Create `observability/prometheus.yml`:

```yaml
global:
  scrape_interval: 5s
  evaluation_interval: 5s

scrape_configs:
  - job_name: "unified-document-viewer"
    static_configs:
      - targets: ["app:8080"]
    metrics_path: "/metrics"
```

- [ ] **Step 2: Add Jaeger and Prometheus to docker-compose.yml**

Add these services before the `volumes:` section in `docker-compose.yml`:

```yaml
  jaeger:
    image: jaegertracing/jaeger:2.2.0
    ports:
      - "16686:16686"
      - "4317:4317"
    environment:
      COLLECTOR_OTLP_ENABLED: "true"
    healthcheck:
      test: ["CMD", "wget", "-q", "-O", "/dev/null", "http://localhost:16687"]
      interval: 3s
      timeout: 3s
      retries: 5

  prometheus:
    image: prom/prometheus:v3.1.0
    ports:
      - "9090:9090"
    volumes:
      - ./observability/prometheus.yml:/etc/prometheus/prometheus.yml
    depends_on:
      app:
        condition: service_healthy
```

Also add to the `app` service's environment:
```yaml
      OTEL_EXPORTER_OTLP_ENDPOINT: "jaeger:4317"
```

- [ ] **Step 3: Commit**

```bash
git add docker-compose.yml observability/prometheus.yml
git commit -m "infra(observability): add Jaeger and Prometheus to Docker Compose"
```

---

## Task 9: Integration Smoke Test

**Files:**
- No new files — validates the full observability stack.

---

- [ ] **Step 1: Start the full stack**

Run:
```bash
docker compose up --build -d
```

Wait for healthy:
```bash
sleep 10 && docker compose ps --format "table {{.Service}}\t{{.Status}}"
```

Expected: All services running (app, postgres, mocks healthy; jaeger, prometheus up).

- [ ] **Step 2: Generate traffic**

Run:
```bash
curl -s http://localhost:8080/vehicles/1HGCM82633A004352/documents > /dev/null
curl -s http://localhost:8080/vehicles/1HGCM82633A004352/documents > /dev/null
curl -s http://localhost:8080/vehicles/INVALID/documents > /dev/null
```

- [ ] **Step 3: Verify structured logs contain correlation fields**

Run:
```bash
docker compose logs app --tail=5 2>&1 | python3 -c "
import sys, json
for line in sys.stdin:
    try:
        d = json.loads(line.split('|')[-1].strip() if '|' in line else line)
        if 'request_id' in d:
            print(f\"request_id={d.get('request_id','')[:8]}... trace_id={d.get('trace_id','')[:8]}... vin={d.get('vin','')} msg={d.get('msg','')}\")
    except:
        pass
"
```

Expected: Log lines with `request_id`, `trace_id`, and masked VIN (e.g., `***004352`).

- [ ] **Step 4: Verify Prometheus metrics**

Run:
```bash
curl -s http://localhost:8080/metrics | grep -E "upstream_request|upstream.request" | head -10
```

Expected: Metrics lines with `upstream.request.duration` and `upstream.request.total` with source/outcome labels.

- [ ] **Step 5: Verify Jaeger traces**

Run:
```bash
curl -s "http://localhost:16686/api/traces?service=unified-document-viewer&limit=5" | python3 -c "
import sys, json
d = json.load(sys.stdin)
if 'data' in d and len(d['data']) > 0:
    trace = d['data'][0]
    print(f\"Trace ID: {trace['traceID'][:16]}...\")
    print(f\"Spans: {len(trace['spans'])}\")
    for span in trace['spans']:
        print(f\"  - {span['operationName']} ({span['duration']/1000:.1f}ms)\")
else:
    print('No traces found yet (may need a few seconds for batching)')
"
```

Expected: At least one trace with parent span + child spans for upstream.sales and upstream.service.

- [ ] **Step 6: Verify Prometheus can scrape the app**

Run:
```bash
curl -s "http://localhost:9090/api/v1/query?query=upstream_request_total" | python3 -c "
import sys, json
d = json.load(sys.stdin)
print(f\"Status: {d['status']}\")
if d['data']['result']:
    for r in d['data']['result']:
        print(f\"  {r['metric']} = {r['value'][1]}\")
else:
    print('  No data yet (metrics may use dots not underscores — check raw /metrics)')
"
```

Expected: Metric data showing upstream request counts.

- [ ] **Step 7: Tear down**

Run:
```bash
docker compose down -v
```

- [ ] **Step 8: Commit if fixes were needed**

```bash
# Only if changes were made:
git add -A
git commit -m "fix: adjustments from observability integration smoke test"
```

---

## Summary

| Task | Delivers | Requirements |
|------|----------|--------------|
| 1 | OTel dependencies | Foundation |
| 2 | Tracing provider (OTLP → Jaeger) | OBSV-02 |
| 3 | Metrics provider (Prometheus exporter) | OBSV-03, OBSV-04 |
| 4 | Tracing + logging middleware | OBSV-01, OBSV-02 |
| 5 | Upstream client instrumentation | OBSV-02, OBSV-04 |
| 6 | Handler structured logging | OBSV-01 |
| 7 | Wire in main.go + /metrics endpoint | All |
| 8 | Docker Compose: Jaeger + Prometheus | OBSV-02, OBSV-03 |
| 9 | End-to-end validation | All Phase 5 success criteria |

**Total commits:** 7-8 atomic commits
**Estimated time:** 60-90 minutes for an experienced Go developer
