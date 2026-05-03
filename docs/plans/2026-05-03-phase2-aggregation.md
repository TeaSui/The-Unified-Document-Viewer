# Phase 2: Core Aggregation & Search — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A working `GET /vehicles/{vin}/documents` endpoint that validates the VIN, fetches documents from both mocks in parallel using non-cancelling fan-out, tags each document with its source, and returns a partial-success envelope when one upstream fails.

**Architecture:** Thin HTTP handler validates VIN and delegates to an aggregation service. The aggregation service launches two goroutines (one per upstream), collects results independently (no fail-fast), tags documents with source, sorts by date descending, and constructs the response envelope with per-source status. Upstream clients are simple typed HTTP clients that return a result or an error — no resiliency policies yet (Phase 4). The handler maps the aggregate result to the correct HTTP status (200 partial/full success, 502 total failure).

**Tech Stack:** Go 1.25, chi v5, `net/http` client, `sync.WaitGroup`, `encoding/json`

**Requirements covered:** SRCH-01, SRCH-02, AGGR-01, AGGR-02, AGGR-03, AGGR-04, AGGR-05, API-01

**Success Criteria:**
1. User can search by VIN and receive a consolidated document list from both sources
2. Invalid VIN (wrong length, invalid chars) returns 400 with structured error message
3. When one upstream is down, documents from the healthy upstream are still returned with per-source status
4. When both upstreams fail and no cache exists, system returns 502 with structured error
5. Each document in the response is tagged with its source system

---

## File Structure

```
internal/
├── domain/
│   └── models.go                  # Shared types: Document, SourceStatus, SourceResult, etc.
├── vin/
│   ├── validate.go                # VIN validation function
│   └── validate_test.go           # Table-driven tests for VIN validation
├── upstream/
│   ├── client.go                  # Upstream HTTP client (one per source)
│   └── client_test.go             # Tests using httptest.Server
├── aggregator/
│   ├── service.go                 # Fan-out orchestration, merge, sort, envelope build
│   └── service_test.go            # Tests with fake upstream clients
└── documents/
    ├── handler.go                 # HTTP handler: parse VIN, call aggregator, write response
    └── handler_test.go            # Handler tests (VIN validation, status codes)
```

New files in `cmd/server/main.go` will be modified to wire the new handler and config.

---

## Task 1: Domain Models

**Files:**
- Create: `internal/domain/models.go`

---

- [ ] **Step 1: Create domain models**

Create `internal/domain/models.go`:

```go
package domain

import "time"

type Document struct {
	ID       string         `json:"id"`
	VIN      string         `json:"vin"`
	Source   string         `json:"source"`
	Type     string         `json:"type"`
	Title    string         `json:"title"`
	Date     time.Time      `json:"date"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type SourceStatus struct {
	Name      string     `json:"name"`
	Status    string     `json:"status"`
	Error     string     `json:"error,omitempty"`
	FetchedAt *time.Time `json:"fetched_at,omitempty"`
}

type SourceResult struct {
	Source    string
	Documents []Document
	Err       error
}

type AggregateResult struct {
	VIN       string
	Documents []Document
	Sources   []SourceStatus
}

type DocumentsEnvelope struct {
	Data DocumentsData `json:"data"`
	Meta ResponseMeta  `json:"meta"`
}

type DocumentsData struct {
	VIN       string         `json:"vin"`
	Documents []Document     `json:"documents"`
	Sources   []SourceStatus `json:"sources"`
}

type ResponseMeta struct {
	RequestID string    `json:"request_id"`
	Timestamp time.Time `json:"timestamp"`
}

type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
```

- [ ] **Step 2: Verify compilation**

Run:
```bash
go build ./...
```

Expected: Compiles without errors.

- [ ] **Step 3: Commit**

```bash
git add internal/domain/models.go
git commit -m "feat(domain): shared types for documents, sources, and response envelope"
```

---

## Task 2: VIN Validation

**Files:**
- Create: `internal/vin/validate.go`
- Create: `internal/vin/validate_test.go`

---

- [ ] **Step 1: Write failing table-driven tests for VIN validation**

Create `internal/vin/validate_test.go`:

```go
package vin_test

import (
	"testing"

	"github.com/tungnguyen/unified-document-viewer/internal/vin"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "valid VIN", input: "1HGCM82633A004352", wantErr: false},
		{name: "valid VIN all digits", input: "12345678901234567", wantErr: false},
		{name: "valid VIN all letters", input: "ABCDEFGHJKLMNPRST", wantErr: false},
		{name: "too short", input: "1HGCM826", wantErr: true},
		{name: "too long", input: "1HGCM82633A0043521", wantErr: true},
		{name: "empty", input: "", wantErr: true},
		{name: "contains I", input: "1HGCM82633I004352", wantErr: true},
		{name: "contains O", input: "1HGCM82633O004352", wantErr: true},
		{name: "contains Q", input: "1HGCM82633Q004352", wantErr: true},
		{name: "contains lowercase", input: "1hgcm82633a004352", wantErr: true},
		{name: "contains space", input: "1HGCM82633 004352", wantErr: true},
		{name: "contains special char", input: "1HGCM82633A00435!", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := vin.Validate(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
go test ./internal/vin/ -v
```

Expected: FAIL — package does not exist yet.

- [ ] **Step 3: Implement VIN validation**

Create `internal/vin/validate.go`:

```go
package vin

import (
	"fmt"
	"regexp"
)

var vinRegex = regexp.MustCompile(`^[A-HJ-NPR-Z0-9]{17}$`)

func Validate(v string) error {
	if !vinRegex.MatchString(v) {
		return fmt.Errorf("invalid VIN: must be exactly 17 characters, alphanumeric excluding I, O, Q")
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:
```bash
go test ./internal/vin/ -v -race
```

Expected: All 12 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/vin/
git commit -m "feat(vin): VIN validation with ISO 3779 charset rules"
```

---

## Task 3: Upstream Client

**Files:**
- Create: `internal/upstream/client.go`
- Create: `internal/upstream/client_test.go`

---

- [ ] **Step 1: Write failing tests for upstream client**

Create `internal/upstream/client_test.go`:

```go
package upstream_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tungnguyen/unified-document-viewer/internal/upstream"
)

func TestClient_Fetch_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/vehicles/1HGCM82633A004352/documents" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"documents": []map[string]any{
				{
					"id":    "DOC-001",
					"vin":   "1HGCM82633A004352",
					"type":  "purchase_agreement",
					"title": "Purchase Agreement",
					"date":  "2024-01-15T10:30:00Z",
				},
			},
		})
	}))
	defer srv.Close()

	client := upstream.NewClient(srv.URL, "sales", &http.Client{Timeout: 5 * time.Second})
	docs, err := client.Fetch(context.Background(), "1HGCM82633A004352")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 document, got %d", len(docs))
	}
	if docs[0].ID != "DOC-001" {
		t.Errorf("expected id DOC-001, got %s", docs[0].ID)
	}
	if docs[0].Source != "sales" {
		t.Errorf("expected source 'sales', got %s", docs[0].Source)
	}
}

func TestClient_Fetch_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"internal_server_error"}`))
	}))
	defer srv.Close()

	client := upstream.NewClient(srv.URL, "sales", &http.Client{Timeout: 5 * time.Second})
	_, err := client.Fetch(context.Background(), "1HGCM82633A004352")

	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestClient_Fetch_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := upstream.NewClient(srv.URL, "service", &http.Client{Timeout: 100 * time.Millisecond})
	_, err := client.Fetch(context.Background(), "1HGCM82633A004352")

	if err == nil {
		t.Fatal("expected error for timeout")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
go test ./internal/upstream/ -v
```

Expected: FAIL — package does not exist.

- [ ] **Step 3: Implement upstream client**

Create `internal/upstream/client.go`:

```go
package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/tungnguyen/unified-document-viewer/internal/domain"
)

type Client struct {
	baseURL    string
	sourceName string
	httpClient *http.Client
}

func NewClient(baseURL, sourceName string, httpClient *http.Client) *Client {
	return &Client{
		baseURL:    baseURL,
		sourceName: sourceName,
		httpClient: httpClient,
	}
}

func (c *Client) Name() string {
	return c.sourceName
}

type upstreamResponse struct {
	Documents []upstreamDocument `json:"documents"`
}

type upstreamDocument struct {
	ID       string         `json:"id"`
	VIN      string         `json:"vin"`
	Type     string         `json:"type"`
	Title    string         `json:"title"`
	Date     string         `json:"date"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

func (c *Client) Fetch(ctx context.Context, vin string) ([]domain.Document, error) {
	url := fmt.Sprintf("%s/api/v1/vehicles/%s/documents", c.baseURL, vin)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching from %s: %w", c.sourceName, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream %s returned status %d", c.sourceName, resp.StatusCode)
	}

	var body upstreamResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decoding response from %s: %w", c.sourceName, err)
	}

	docs := make([]domain.Document, 0, len(body.Documents))
	for _, d := range body.Documents {
		parsed, err := time.Parse(time.RFC3339, d.Date)
		if err != nil {
			parsed = time.Time{}
		}
		docs = append(docs, domain.Document{
			ID:       d.ID,
			VIN:      d.VIN,
			Source:   c.sourceName,
			Type:     d.Type,
			Title:    d.Title,
			Date:     parsed,
			Metadata: d.Metadata,
		})
	}

	return docs, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:
```bash
go test ./internal/upstream/ -v -race
```

Expected: All 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/upstream/
git commit -m "feat(upstream): HTTP client for upstream document APIs with source tagging"
```

---

## Task 4: Aggregation Service

**Files:**
- Create: `internal/aggregator/service.go`
- Create: `internal/aggregator/service_test.go`

---

- [ ] **Step 1: Write failing tests for aggregation service**

Create `internal/aggregator/service_test.go`:

```go
package aggregator_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/tungnguyen/unified-document-viewer/internal/aggregator"
	"github.com/tungnguyen/unified-document-viewer/internal/domain"
)

type fakeSource struct {
	name string
	docs []domain.Document
	err  error
}

func (f *fakeSource) Name() string { return f.name }
func (f *fakeSource) Fetch(_ context.Context, _ string) ([]domain.Document, error) {
	return f.docs, f.err
}

func TestService_BothSucceed(t *testing.T) {
	sales := &fakeSource{
		name: "sales",
		docs: []domain.Document{
			{ID: "S1", Source: "sales", Date: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)},
		},
	}
	service := &fakeSource{
		name: "service",
		docs: []domain.Document{
			{ID: "V1", Source: "service", Date: time.Date(2024, 3, 20, 8, 0, 0, 0, time.UTC)},
		},
	}

	svc := aggregator.NewService([]aggregator.Source{sales, service})
	result, err := svc.Aggregate(context.Background(), "1HGCM82633A004352")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Documents) != 2 {
		t.Fatalf("expected 2 documents, got %d", len(result.Documents))
	}
	// Should be sorted by date descending — service doc (March) before sales doc (Jan)
	if result.Documents[0].ID != "V1" {
		t.Errorf("expected first doc to be V1 (newest), got %s", result.Documents[0].ID)
	}
	if len(result.Sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(result.Sources))
	}
	for _, s := range result.Sources {
		if s.Status != "ok" {
			t.Errorf("expected source %s status 'ok', got '%s'", s.Name, s.Status)
		}
	}
}

func TestService_OneSourceFails(t *testing.T) {
	sales := &fakeSource{
		name: "sales",
		docs: []domain.Document{
			{ID: "S1", Source: "sales", Date: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)},
		},
	}
	service := &fakeSource{
		name: "service",
		err:  fmt.Errorf("upstream service returned status 500"),
	}

	svc := aggregator.NewService([]aggregator.Source{sales, service})
	result, err := svc.Aggregate(context.Background(), "1HGCM82633A004352")

	if err != nil {
		t.Fatalf("should not return error for partial failure: %v", err)
	}
	if len(result.Documents) != 1 {
		t.Fatalf("expected 1 document from healthy source, got %d", len(result.Documents))
	}
	if result.Documents[0].ID != "S1" {
		t.Errorf("expected doc S1, got %s", result.Documents[0].ID)
	}

	var salesStatus, serviceStatus domain.SourceStatus
	for _, s := range result.Sources {
		if s.Name == "sales" {
			salesStatus = s
		}
		if s.Name == "service" {
			serviceStatus = s
		}
	}
	if salesStatus.Status != "ok" {
		t.Errorf("expected sales status 'ok', got '%s'", salesStatus.Status)
	}
	if serviceStatus.Status != "failed" {
		t.Errorf("expected service status 'failed', got '%s'", serviceStatus.Status)
	}
	if serviceStatus.Error == "" {
		t.Error("expected service error message to be non-empty")
	}
}

func TestService_BothFail(t *testing.T) {
	sales := &fakeSource{name: "sales", err: fmt.Errorf("connection refused")}
	service := &fakeSource{name: "service", err: fmt.Errorf("timeout")}

	svc := aggregator.NewService([]aggregator.Source{sales, service})
	_, err := svc.Aggregate(context.Background(), "1HGCM82633A004352")

	if err == nil {
		t.Fatal("expected error when all sources fail")
	}
}

func TestService_DocumentsSortedByDateDescending(t *testing.T) {
	sales := &fakeSource{
		name: "sales",
		docs: []domain.Document{
			{ID: "S1", Source: "sales", Date: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
			{ID: "S2", Source: "sales", Date: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)},
		},
	}
	service := &fakeSource{
		name: "service",
		docs: []domain.Document{
			{ID: "V1", Source: "service", Date: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)},
		},
	}

	svc := aggregator.NewService([]aggregator.Source{sales, service})
	result, err := svc.Aggregate(context.Background(), "1HGCM82633A004352")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Documents) != 3 {
		t.Fatalf("expected 3 documents, got %d", len(result.Documents))
	}
	// June > March > January
	expected := []string{"S2", "V1", "S1"}
	for i, id := range expected {
		if result.Documents[i].ID != id {
			t.Errorf("position %d: expected %s, got %s", i, id, result.Documents[i].ID)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
go test ./internal/aggregator/ -v
```

Expected: FAIL — package does not exist.

- [ ] **Step 3: Implement aggregation service**

Create `internal/aggregator/service.go`:

```go
package aggregator

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/tungnguyen/unified-document-viewer/internal/domain"
)

type Source interface {
	Name() string
	Fetch(ctx context.Context, vin string) ([]domain.Document, error)
}

type Service struct {
	sources []Source
}

func NewService(sources []Source) *Service {
	return &Service{sources: sources}
}

func (s *Service) Aggregate(ctx context.Context, vin string) (*domain.AggregateResult, error) {
	results := make([]domain.SourceResult, len(s.sources))
	var wg sync.WaitGroup

	for i, src := range s.sources {
		wg.Add(1)
		go func(idx int, source Source) {
			defer wg.Done()
			docs, err := source.Fetch(ctx, vin)
			results[idx] = domain.SourceResult{
				Source:    source.Name(),
				Documents: docs,
				Err:       err,
			}
		}(i, src)
	}
	wg.Wait()

	var allDocs []domain.Document
	sources := make([]domain.SourceStatus, 0, len(results))
	allFailed := true

	for _, r := range results {
		status := domain.SourceStatus{Name: r.Source}

		if r.Err != nil {
			status.Status = "failed"
			status.Error = r.Err.Error()
		} else {
			status.Status = "ok"
			allDocs = append(allDocs, r.Documents...)
			allFailed = false
		}

		sources = append(sources, status)
	}

	if allFailed {
		return nil, fmt.Errorf("all upstream sources failed")
	}

	sort.Slice(allDocs, func(i, j int) bool {
		return allDocs[i].Date.After(allDocs[j].Date)
	})

	return &domain.AggregateResult{
		VIN:       vin,
		Documents: allDocs,
		Sources:   sources,
	}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:
```bash
go test ./internal/aggregator/ -v -race
```

Expected: All 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/aggregator/
git commit -m "feat(aggregator): parallel fan-out with partial-success semantics and date sorting"
```

---

## Task 5: Documents HTTP Handler

**Files:**
- Create: `internal/documents/handler.go`
- Create: `internal/documents/handler_test.go`

---

- [ ] **Step 1: Write failing tests for the documents handler**

Create `internal/documents/handler_test.go`:

```go
package documents_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tungnguyen/unified-document-viewer/internal/documents"
	"github.com/tungnguyen/unified-document-viewer/internal/domain"
)

type fakeAggregator struct {
	result *domain.AggregateResult
	err    error
}

func (f *fakeAggregator) Aggregate(_ context.Context, _ string) (*domain.AggregateResult, error) {
	return f.result, f.err
}

func newTestRouter(handler *documents.Handler) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/vehicles/{vin}/documents", handler.GetDocuments)
	return r
}

func TestHandler_ValidVIN_Success(t *testing.T) {
	agg := &fakeAggregator{
		result: &domain.AggregateResult{
			VIN: "1HGCM82633A004352",
			Documents: []domain.Document{
				{ID: "D1", VIN: "1HGCM82633A004352", Source: "sales", Type: "purchase_agreement", Title: "Purchase", Date: time.Now()},
			},
			Sources: []domain.SourceStatus{
				{Name: "sales", Status: "ok"},
				{Name: "service", Status: "ok"},
			},
		},
	}

	handler := documents.NewHandler(agg)
	r := newTestRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/vehicles/1HGCM82633A004352/documents", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var envelope domain.DocumentsEnvelope
	if err := json.NewDecoder(rec.Body).Decode(&envelope); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if envelope.Data.VIN != "1HGCM82633A004352" {
		t.Errorf("expected VIN in response, got %s", envelope.Data.VIN)
	}
	if len(envelope.Data.Documents) != 1 {
		t.Errorf("expected 1 document, got %d", len(envelope.Data.Documents))
	}
	if len(envelope.Data.Sources) != 2 {
		t.Errorf("expected 2 sources, got %d", len(envelope.Data.Sources))
	}
	if envelope.Meta.RequestID == "" {
		t.Error("expected request_id in meta")
	}
	if envelope.Meta.Timestamp.IsZero() {
		t.Error("expected timestamp in meta")
	}
}

func TestHandler_InvalidVIN_Returns400(t *testing.T) {
	handler := documents.NewHandler(&fakeAggregator{})
	r := newTestRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/vehicles/INVALID/documents", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}

	var errResp domain.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.Error.Code != "invalid_vin" {
		t.Errorf("expected error code 'invalid_vin', got '%s'", errResp.Error.Code)
	}
}

func TestHandler_AllSourcesFail_Returns502(t *testing.T) {
	agg := &fakeAggregator{
		err: fmt.Errorf("all upstream sources failed"),
	}

	handler := documents.NewHandler(agg)
	r := newTestRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/vehicles/1HGCM82633A004352/documents", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", rec.Code)
	}

	var errResp domain.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.Error.Code != "upstream_failure" {
		t.Errorf("expected error code 'upstream_failure', got '%s'", errResp.Error.Code)
	}
}

func TestHandler_PartialFailure_Returns200(t *testing.T) {
	agg := &fakeAggregator{
		result: &domain.AggregateResult{
			VIN: "1HGCM82633A004352",
			Documents: []domain.Document{
				{ID: "D1", Source: "sales"},
			},
			Sources: []domain.SourceStatus{
				{Name: "sales", Status: "ok"},
				{Name: "service", Status: "failed", Error: "timeout"},
			},
		},
	}

	handler := documents.NewHandler(agg)
	r := newTestRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/vehicles/1HGCM82633A004352/documents", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for partial failure, got %d", rec.Code)
	}

	var envelope domain.DocumentsEnvelope
	json.NewDecoder(rec.Body).Decode(&envelope)
	if len(envelope.Data.Documents) != 1 {
		t.Errorf("expected 1 document, got %d", len(envelope.Data.Documents))
	}

	var serviceSource domain.SourceStatus
	for _, s := range envelope.Data.Sources {
		if s.Name == "service" {
			serviceSource = s
		}
	}
	if serviceSource.Status != "failed" {
		t.Errorf("expected service status 'failed', got '%s'", serviceSource.Status)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
go test ./internal/documents/ -v
```

Expected: FAIL — package does not exist.

- [ ] **Step 3: Implement the documents handler**

Create `internal/documents/handler.go`:

```go
package documents

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/tungnguyen/unified-document-viewer/internal/domain"
	"github.com/tungnguyen/unified-document-viewer/internal/vin"
)

type Aggregator interface {
	Aggregate(ctx context.Context, vinStr string) (*domain.AggregateResult, error)
}

type Handler struct {
	aggregator Aggregator
}

func NewHandler(aggregator Aggregator) *Handler {
	return &Handler{aggregator: aggregator}
}

func (h *Handler) GetDocuments(w http.ResponseWriter, r *http.Request) {
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
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(domain.ErrorResponse{
			Error: domain.ErrorDetail{
				Code:    "upstream_failure",
				Message: "all upstream sources failed",
			},
		})
		return
	}

	requestID := middleware.GetReqID(r.Context())

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
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:
```bash
go test ./internal/documents/ -v -race
```

Expected: All 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/documents/
git commit -m "feat(documents): HTTP handler with VIN validation, envelope response, and error mapping"
```

---

## Task 6: Wire Everything in main.go

**Files:**
- Modify: `cmd/server/main.go`
- Modify: `internal/config/config.go`

---

- [ ] **Step 1: Add upstream URLs to config**

Replace `internal/config/config.go` with:

```go
package config

import (
	"fmt"
	"os"
)

type Config struct {
	Port           string
	DatabaseURL    string
	SalesMockURL   string
	ServiceMockURL string
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

	return &Config{
		Port:           port,
		DatabaseURL:    dbURL,
		SalesMockURL:   salesURL,
		ServiceMockURL: serviceURL,
	}, nil
}
```

- [ ] **Step 2: Wire the aggregator and documents handler in main.go**

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

	httpClient := &http.Client{Timeout: 5 * time.Second}
	salesClient := upstream.NewClient(cfg.SalesMockURL, "sales", httpClient)
	serviceClient := upstream.NewClient(cfg.ServiceMockURL, "service", httpClient)

	aggService := aggregator.NewService([]aggregator.Source{salesClient, serviceClient})
	docsHandler := documents.NewHandler(aggService)

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

- [ ] **Step 3: Verify compilation and all tests pass**

Run:
```bash
go build ./... && go test ./... -short -v -race
```

Expected: Compiles. All tests pass (VIN validation, upstream client, aggregator, documents handler, health).

- [ ] **Step 4: Commit**

```bash
git add cmd/server/main.go internal/config/config.go
git commit -m "feat(wiring): connect aggregator and document handler to router"
```

---

## Task 7: Integration Smoke Test

**Files:**
- No new files — this task validates the endpoint works end-to-end against Docker Compose.

---

- [ ] **Step 1: Start Docker Compose stack**

Run:
```bash
docker compose up --build -d
```

Wait for all services to be healthy:
```bash
docker compose ps --format "table {{.Service}}\t{{.Status}}"
```

Expected: All services show `(healthy)`.

- [ ] **Step 2: Test happy path — both sources succeed**

Run:
```bash
curl -s http://localhost:8080/vehicles/1HGCM82633A004352/documents | python3 -m json.tool
```

Expected: 200 response with:
- `data.vin` = `"1HGCM82633A004352"`
- `data.documents` — 6 documents (3 from sales + 3 from service), sorted by date descending, each with `source` field
- `data.sources` — two entries, both with `status: "ok"`
- `meta.request_id` — non-empty UUID
- `meta.timestamp` — current time

- [ ] **Step 3: Test invalid VIN**

Run:
```bash
curl -s -w "\n%{http_code}" http://localhost:8080/vehicles/INVALID/documents
```

Expected: HTTP 400 with `{"error":{"code":"invalid_vin","message":"..."}}`

- [ ] **Step 4: Test partial failure — one source returns 500**

Run:
```bash
curl -s -H "X-Mock-Fault: 500" http://localhost:9001/api/v1/vehicles/1HGCM82633A004352/documents
```

This confirms the fault injection works on the mock directly. For the actual app, the app doesn't forward headers to upstreams — so to test partial failure, we'll use WireMock's admin API to temporarily add a fault:

```bash
# Reset sales mock to return 500 for all requests temporarily
curl -s -X POST http://localhost:9001/__admin/mappings -H "Content-Type: application/json" -d '{
  "priority": 0,
  "request": {"method": "GET", "urlPathPattern": "/api/v1/vehicles/.*/documents"},
  "response": {"status": 500, "body": "{\"error\":\"simulated\"}"}
}'
```

Then test:
```bash
curl -s http://localhost:8080/vehicles/1HGCM82633A004352/documents | python3 -m json.tool
```

Expected: 200 response with:
- `data.documents` — 3 documents (service only)
- `data.sources` — sales has `status: "failed"`, service has `status: "ok"`

Then clean up:
```bash
curl -s -X POST http://localhost:9001/__admin/mappings/reset
```

- [ ] **Step 5: Test total failure — both sources down**

Shut down the mock services:
```bash
docker compose stop sales-mock service-mock
```

Test:
```bash
curl -s -w "\n%{http_code}" http://localhost:8080/vehicles/1HGCM82633A004352/documents
```

Expected: HTTP 502 with `{"error":{"code":"upstream_failure","message":"all upstream sources failed"}}`

Bring mocks back:
```bash
docker compose start sales-mock service-mock
```

- [ ] **Step 6: Test determinism — same VIN always returns same data**

Run:
```bash
curl -s http://localhost:8080/vehicles/1HGCM82633A004352/documents | python3 -c "import sys,json; d=json.load(sys.stdin); print([doc['id'] for doc in d['data']['documents']])"
curl -s http://localhost:8080/vehicles/1HGCM82633A004352/documents | python3 -c "import sys,json; d=json.load(sys.stdin); print([doc['id'] for doc in d['data']['documents']])"
```

Expected: Both calls return the same list of document IDs.

- [ ] **Step 7: Tear down**

Run:
```bash
docker compose down -v
```

- [ ] **Step 8: Commit if any fixes were needed**

If changes were made during smoke testing:
```bash
git add -A
git commit -m "fix: adjustments from integration smoke test"
```

---

## Summary

| Task | Delivers | Requirements |
|------|----------|--------------|
| 1 | Domain types (Document, SourceStatus, Envelope) | Foundation for all |
| 2 | VIN validation | SRCH-01, SRCH-02 |
| 3 | Upstream HTTP client with source tagging | AGGR-02 |
| 4 | Aggregation service with parallel fan-out | AGGR-01, AGGR-03, AGGR-04, AGGR-05 |
| 5 | Documents HTTP handler with error mapping | API-01, SRCH-02 |
| 6 | Wiring in main.go | All (integration) |
| 7 | End-to-end validation | All Phase 2 success criteria |

**Total commits:** 6-7 atomic commits
**Estimated time:** 60-90 minutes for an experienced Go developer
