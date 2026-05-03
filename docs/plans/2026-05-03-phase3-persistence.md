# Phase 3: Persistence & Caching — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every request is audit-logged to Postgres, last-known-good responses are cached per (VIN, source), and when an upstream fails the system serves cached data marked as stale with a `fetched_at` timestamp.

**Architecture:** SQL migrations create two tables (`audit_request` and `cached_response`). A repository layer provides typed access to both tables using pgx directly (no ORM). The aggregator service is enhanced with a `Cache` interface — after a successful fetch, documents are cached; on failure, the cache is consulted and stale data is returned with `status: "stale"`. Audit writes are best-effort (logged on failure, never block the response). The repository is injected into the aggregator via its constructor.

**Tech Stack:** Go 1.25, pgx/v5, Postgres 16, goose (SQL migrations)

**Requirements covered:** PERS-01, PERS-02, PERS-03

**Success Criteria:**
1. After a successful request, an audit log entry exists in the database with request_id, VIN, timestamp, and per-source outcomes
2. After a successful upstream fetch, the response is cached in the database keyed by (VIN, source)
3. When an upstream fails and a cached response exists, the cached documents are returned with status "stale" and a `fetched_at` timestamp

---

## File Structure

```
internal/
├── repository/
│   ├── audit.go                   # AuditRepository: insert audit rows
│   ├── audit_test.go              # Integration tests (require Postgres)
│   ├── cache.go                   # CacheRepository: get/put cached responses
│   └── cache_test.go              # Integration tests (require Postgres)
├── aggregator/
│   ├── service.go                 # MODIFIED: add Cache interface, stale-on-failure logic
│   └── service_test.go            # MODIFIED: add tests for cache hit/miss scenarios
├── documents/
│   └── handler.go                 # MODIFIED: pass request_id to aggregator, call audit after
└── domain/
    └── models.go                  # MODIFIED: add AuditEntry type

migrations/
├── 001_create_audit_request.sql
└── 002_create_cached_response.sql

cmd/server/main.go                 # MODIFIED: run migrations, wire repository
docker-compose.yml                 # MODIFIED: add migrate init container
Makefile                           # MODIFIED: add migrate target
```

---

## Task 1: Database Migrations

**Files:**
- Create: `migrations/001_create_audit_request.sql`
- Create: `migrations/002_create_cached_response.sql`
- Modify: `docker-compose.yml`
- Modify: `Makefile`

---

- [ ] **Step 1: Install goose**

Run:
```bash
go install github.com/pressly/goose/v3/cmd/goose@latest
```

- [ ] **Step 2: Create audit_request migration**

Create `migrations/001_create_audit_request.sql`:

```sql
-- +goose Up
CREATE TABLE audit_request (
    request_id  TEXT PRIMARY KEY,
    vin         TEXT NOT NULL,
    ts          TIMESTAMPTZ NOT NULL DEFAULT now(),
    http_status INT NOT NULL,
    duration_ms INT NOT NULL,
    outcomes    JSONB NOT NULL
);

CREATE INDEX idx_audit_vin_ts ON audit_request(vin, ts DESC);

-- +goose Down
DROP TABLE IF EXISTS audit_request;
```

- [ ] **Step 3: Create cached_response migration**

Create `migrations/002_create_cached_response.sql`:

```sql
-- +goose Up
CREATE TABLE cached_response (
    vin        TEXT NOT NULL,
    source     TEXT NOT NULL,
    payload    JSONB NOT NULL,
    fetched_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (vin, source)
);

CREATE INDEX idx_cached_fetched ON cached_response(fetched_at);

-- +goose Down
DROP TABLE IF EXISTS cached_response;
```

- [ ] **Step 4: Add migrate service to docker-compose.yml**

Add a `migrate` service after `postgres` in `docker-compose.yml`. The full updated file should include this new service between `postgres` and `sales-mock`:

```yaml
  migrate:
    image: ghcr.io/pressly/goose:latest
    volumes:
      - ./migrations:/migrations
    command: ["-dir", "/migrations", "postgres", "postgres://docviewer:docviewer@postgres:5432/docviewer?sslmode=disable", "up"]
    depends_on:
      postgres:
        condition: service_healthy
```

Also update the `app` service's `depends_on` to include migrate:

```yaml
    depends_on:
      postgres:
        condition: service_healthy
      migrate:
        condition: service_completed_successfully
      sales-mock:
        condition: service_healthy
      service-mock:
        condition: service_healthy
```

- [ ] **Step 5: Add migrate target to Makefile**

Add to `Makefile`:

```makefile
migrate-up:
	goose -dir migrations postgres "postgres://docviewer:docviewer@localhost:5433/docviewer?sslmode=disable" up

migrate-down:
	goose -dir migrations postgres "postgres://docviewer:docviewer@localhost:5433/docviewer?sslmode=disable" down

migrate-status:
	goose -dir migrations postgres "postgres://docviewer:docviewer@localhost:5433/docviewer?sslmode=disable" status
```

- [ ] **Step 6: Verify migrations run**

Run:
```bash
docker compose up postgres -d
sleep 3
make migrate-up
make migrate-status
```

Expected: Both migrations applied, status shows "Applied".

- [ ] **Step 7: Tear down and commit**

```bash
docker compose down -v
git add migrations/ docker-compose.yml Makefile
git commit -m "feat(persistence): database migrations for audit_request and cached_response tables"
```

---

## Task 2: Cache Repository

**Files:**
- Create: `internal/repository/cache.go`
- Create: `internal/repository/cache_test.go`

---

- [ ] **Step 1: Write failing integration tests for cache repository**

Create `internal/repository/cache_test.go`:

```go
package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tungnguyen/unified-document-viewer/internal/domain"
	"github.com/tungnguyen/unified-document-viewer/internal/repository"
)

func setupCacheTest(t *testing.T) (*repository.CacheRepository, *pgxpool.Pool) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool := newTestPool(t)
	repo := repository.NewCacheRepository(pool)

	// Clean table before test
	_, err := pool.Exec(context.Background(), "DELETE FROM cached_response")
	if err != nil {
		t.Fatalf("failed to clean cached_response: %v", err)
	}

	return repo, pool
}

func TestCacheRepository_PutAndGet(t *testing.T) {
	repo, _ := setupCacheTest(t)
	ctx := context.Background()

	docs := []domain.Document{
		{ID: "D1", VIN: "1HGCM82633A004352", Source: "sales", Type: "purchase", Title: "Purchase", Date: time.Now()},
	}

	err := repo.Put(ctx, "1HGCM82633A004352", "sales", docs)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	cached, fetchedAt, err := repo.Get(ctx, "1HGCM82633A004352", "sales")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if len(cached) != 1 {
		t.Fatalf("expected 1 cached doc, got %d", len(cached))
	}
	if cached[0].ID != "D1" {
		t.Errorf("expected doc ID D1, got %s", cached[0].ID)
	}
	if fetchedAt.IsZero() {
		t.Error("expected non-zero fetched_at")
	}
}

func TestCacheRepository_Get_NotFound(t *testing.T) {
	repo, _ := setupCacheTest(t)
	ctx := context.Background()

	cached, _, err := repo.Get(ctx, "NONEXISTENT1234567", "sales")
	if err != nil {
		t.Fatalf("Get should not error for missing key: %v", err)
	}
	if cached != nil {
		t.Errorf("expected nil for missing cache, got %v", cached)
	}
}

func TestCacheRepository_Put_OverwritesExisting(t *testing.T) {
	repo, _ := setupCacheTest(t)
	ctx := context.Background()

	docs1 := []domain.Document{{ID: "D1", Source: "sales"}}
	docs2 := []domain.Document{{ID: "D2", Source: "sales"}, {ID: "D3", Source: "sales"}}

	repo.Put(ctx, "1HGCM82633A004352", "sales", docs1)
	repo.Put(ctx, "1HGCM82633A004352", "sales", docs2)

	cached, _, err := repo.Get(ctx, "1HGCM82633A004352", "sales")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if len(cached) != 2 {
		t.Fatalf("expected 2 docs after overwrite, got %d", len(cached))
	}
}

func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := "postgres://docviewer:docviewer@localhost:5433/docviewer?sslmode=disable"
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Skipf("cannot connect to test database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
```

- [ ] **Step 2: Implement cache repository**

Create `internal/repository/cache.go`:

```go
package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tungnguyen/unified-document-viewer/internal/domain"
)

type CacheRepository struct {
	pool *pgxpool.Pool
}

func NewCacheRepository(pool *pgxpool.Pool) *CacheRepository {
	return &CacheRepository{pool: pool}
}

func (r *CacheRepository) Put(ctx context.Context, vin, source string, docs []domain.Document) error {
	payload, err := json.Marshal(docs)
	if err != nil {
		return err
	}

	_, err = r.pool.Exec(ctx,
		`INSERT INTO cached_response (vin, source, payload, fetched_at)
		 VALUES ($1, $2, $3, now())
		 ON CONFLICT (vin, source) DO UPDATE SET payload = $3, fetched_at = now()`,
		vin, source, payload,
	)
	return err
}

func (r *CacheRepository) Get(ctx context.Context, vin, source string) ([]domain.Document, time.Time, error) {
	var payload []byte
	var fetchedAt time.Time

	err := r.pool.QueryRow(ctx,
		`SELECT payload, fetched_at FROM cached_response WHERE vin = $1 AND source = $2`,
		vin, source,
	).Scan(&payload, &fetchedAt)

	if err == pgx.ErrNoRows {
		return nil, time.Time{}, nil
	}
	if err != nil {
		return nil, time.Time{}, err
	}

	var docs []domain.Document
	if err := json.Unmarshal(payload, &docs); err != nil {
		return nil, time.Time{}, err
	}

	return docs, fetchedAt, nil
}
```

- [ ] **Step 3: Run tests (requires Postgres running)**

Run:
```bash
docker compose up postgres -d && sleep 3 && make migrate-up
go test ./internal/repository/ -v -race -run TestCache
docker compose down -v
```

Expected: All 3 cache tests PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/repository/cache.go internal/repository/cache_test.go
git commit -m "feat(repository): cache repository for stale-on-failure reads"
```

---

## Task 3: Audit Repository

**Files:**
- Create: `internal/repository/audit.go`
- Create: `internal/repository/audit_test.go`
- Modify: `internal/domain/models.go`

---

- [ ] **Step 1: Add AuditEntry type to domain models**

Add to the end of `internal/domain/models.go`:

```go
type AuditEntry struct {
	RequestID  string
	VIN        string
	HTTPStatus int
	DurationMs int
	Outcomes   []SourceStatus
}
```

- [ ] **Step 2: Write failing integration test for audit repository**

Create `internal/repository/audit_test.go`:

```go
package repository_test

import (
	"context"
	"testing"

	"github.com/tungnguyen/unified-document-viewer/internal/domain"
	"github.com/tungnguyen/unified-document-viewer/internal/repository"
)

func setupAuditTest(t *testing.T) *repository.AuditRepository {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool := newTestPool(t)
	repo := repository.NewAuditRepository(pool)

	_, err := pool.Exec(context.Background(), "DELETE FROM audit_request")
	if err != nil {
		t.Fatalf("failed to clean audit_request: %v", err)
	}

	return repo
}

func TestAuditRepository_Insert(t *testing.T) {
	repo := setupAuditTest(t)
	ctx := context.Background()

	entry := domain.AuditEntry{
		RequestID:  "req-123",
		VIN:        "1HGCM82633A004352",
		HTTPStatus: 200,
		DurationMs: 150,
		Outcomes: []domain.SourceStatus{
			{Name: "sales", Status: "ok"},
			{Name: "service", Status: "failed", Error: "timeout"},
		},
	}

	err := repo.Insert(ctx, entry)
	if err != nil {
		t.Fatalf("Insert failed: %v", err)
	}

	// Verify it was inserted
	pool := newTestPool(t)
	var count int
	err = pool.QueryRow(ctx, "SELECT count(*) FROM audit_request WHERE request_id = $1", "req-123").Scan(&count)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 audit row, got %d", count)
	}
}

func TestAuditRepository_Insert_DuplicateRequestID(t *testing.T) {
	repo := setupAuditTest(t)
	ctx := context.Background()

	entry := domain.AuditEntry{
		RequestID:  "req-dup",
		VIN:        "1HGCM82633A004352",
		HTTPStatus: 200,
		DurationMs: 100,
		Outcomes:   []domain.SourceStatus{{Name: "sales", Status: "ok"}},
	}

	err := repo.Insert(ctx, entry)
	if err != nil {
		t.Fatalf("first insert failed: %v", err)
	}

	err = repo.Insert(ctx, entry)
	if err == nil {
		t.Error("expected error on duplicate request_id")
	}
}
```

- [ ] **Step 3: Implement audit repository**

Create `internal/repository/audit.go`:

```go
package repository

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tungnguyen/unified-document-viewer/internal/domain"
)

type AuditRepository struct {
	pool *pgxpool.Pool
}

func NewAuditRepository(pool *pgxpool.Pool) *AuditRepository {
	return &AuditRepository{pool: pool}
}

func (r *AuditRepository) Insert(ctx context.Context, entry domain.AuditEntry) error {
	outcomes, err := json.Marshal(entry.Outcomes)
	if err != nil {
		return err
	}

	_, err = r.pool.Exec(ctx,
		`INSERT INTO audit_request (request_id, vin, http_status, duration_ms, outcomes)
		 VALUES ($1, $2, $3, $4, $5)`,
		entry.RequestID, entry.VIN, entry.HTTPStatus, entry.DurationMs, outcomes,
	)
	return err
}
```

- [ ] **Step 4: Run tests**

Run:
```bash
docker compose up postgres -d && sleep 3 && make migrate-up
go test ./internal/repository/ -v -race
docker compose down -v
```

Expected: All 5 repository tests PASS (3 cache + 2 audit).

- [ ] **Step 5: Commit**

```bash
git add internal/domain/models.go internal/repository/audit.go internal/repository/audit_test.go
git commit -m "feat(repository): audit repository for request logging"
```

---

## Task 4: Integrate Cache into Aggregator (Stale-on-Failure)

**Files:**
- Modify: `internal/aggregator/service.go`
- Modify: `internal/aggregator/service_test.go`

---

- [ ] **Step 1: Add new tests for cache scenarios**

Add to the end of `internal/aggregator/service_test.go`:

```go
type fakeCache struct {
	data map[string]fakeCacheEntry
}

type fakeCacheEntry struct {
	docs      []domain.Document
	fetchedAt time.Time
}

func newFakeCache() *fakeCache {
	return &fakeCache{data: make(map[string]fakeCacheEntry)}
}

func (f *fakeCache) Get(_ context.Context, vin, source string) ([]domain.Document, time.Time, error) {
	entry, ok := f.data[vin+":"+source]
	if !ok {
		return nil, time.Time{}, nil
	}
	return entry.docs, entry.fetchedAt, nil
}

func (f *fakeCache) Put(_ context.Context, vin, source string, docs []domain.Document) error {
	f.data[vin+":"+source] = fakeCacheEntry{docs: docs, fetchedAt: time.Now()}
	return nil
}

func TestService_OneFailsWithCache_ReturnsStale(t *testing.T) {
	cache := newFakeCache()
	cachedTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	cache.data["1HGCM82633A004352:service"] = fakeCacheEntry{
		docs:      []domain.Document{{ID: "CACHED-V1", Source: "service"}},
		fetchedAt: cachedTime,
	}

	sales := &fakeSource{
		name: "sales",
		docs: []domain.Document{{ID: "S1", Source: "sales", Date: time.Now()}},
	}
	service := &fakeSource{
		name: "service",
		err:  fmt.Errorf("upstream service returned status 500"),
	}

	svc := aggregator.NewService([]aggregator.Source{sales, service}, aggregator.WithCache(cache))
	result, err := svc.Aggregate(context.Background(), "1HGCM82633A004352")

	if err != nil {
		t.Fatalf("should not return error: %v", err)
	}
	if len(result.Documents) != 2 {
		t.Fatalf("expected 2 documents (1 live + 1 cached), got %d", len(result.Documents))
	}

	var serviceStatus domain.SourceStatus
	for _, s := range result.Sources {
		if s.Name == "service" {
			serviceStatus = s
		}
	}
	if serviceStatus.Status != "stale" {
		t.Errorf("expected service status 'stale', got '%s'", serviceStatus.Status)
	}
	if serviceStatus.FetchedAt == nil || !serviceStatus.FetchedAt.Equal(cachedTime) {
		t.Errorf("expected fetched_at %v, got %v", cachedTime, serviceStatus.FetchedAt)
	}
}

func TestService_BothFailWithCache_ReturnsStale(t *testing.T) {
	cache := newFakeCache()
	cachedTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	cache.data["1HGCM82633A004352:sales"] = fakeCacheEntry{
		docs:      []domain.Document{{ID: "CACHED-S1", Source: "sales"}},
		fetchedAt: cachedTime,
	}
	cache.data["1HGCM82633A004352:service"] = fakeCacheEntry{
		docs:      []domain.Document{{ID: "CACHED-V1", Source: "service"}},
		fetchedAt: cachedTime,
	}

	sales := &fakeSource{name: "sales", err: fmt.Errorf("connection refused")}
	service := &fakeSource{name: "service", err: fmt.Errorf("timeout")}

	svc := aggregator.NewService([]aggregator.Source{sales, service}, aggregator.WithCache(cache))
	result, err := svc.Aggregate(context.Background(), "1HGCM82633A004352")

	if err != nil {
		t.Fatalf("should not return error when cache exists: %v", err)
	}
	if len(result.Documents) != 2 {
		t.Fatalf("expected 2 cached documents, got %d", len(result.Documents))
	}
	for _, s := range result.Sources {
		if s.Status != "stale" {
			t.Errorf("expected source %s status 'stale', got '%s'", s.Name, s.Status)
		}
	}
}

func TestService_BothFailNoCache_ReturnsError(t *testing.T) {
	cache := newFakeCache() // empty

	sales := &fakeSource{name: "sales", err: fmt.Errorf("connection refused")}
	service := &fakeSource{name: "service", err: fmt.Errorf("timeout")}

	svc := aggregator.NewService([]aggregator.Source{sales, service}, aggregator.WithCache(cache))
	_, err := svc.Aggregate(context.Background(), "1HGCM82633A004352")

	if err == nil {
		t.Fatal("expected error when all fail and no cache")
	}
}

func TestService_SuccessCachesDocuments(t *testing.T) {
	cache := newFakeCache()

	sales := &fakeSource{
		name: "sales",
		docs: []domain.Document{{ID: "S1", Source: "sales", Date: time.Now()}},
	}
	service := &fakeSource{
		name: "service",
		docs: []domain.Document{{ID: "V1", Source: "service", Date: time.Now()}},
	}

	svc := aggregator.NewService([]aggregator.Source{sales, service}, aggregator.WithCache(cache))
	_, err := svc.Aggregate(context.Background(), "1HGCM82633A004352")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify cache was populated
	if _, ok := cache.data["1HGCM82633A004352:sales"]; !ok {
		t.Error("expected sales docs to be cached")
	}
	if _, ok := cache.data["1HGCM82633A004352:service"]; !ok {
		t.Error("expected service docs to be cached")
	}
}
```

- [ ] **Step 2: Update aggregator service with Cache interface and stale-on-failure logic**

Replace `internal/aggregator/service.go` with:

```go
package aggregator

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/tungnguyen/unified-document-viewer/internal/domain"
)

type Source interface {
	Name() string
	Fetch(ctx context.Context, vin string) ([]domain.Document, error)
}

type Cache interface {
	Get(ctx context.Context, vin, source string) ([]domain.Document, time.Time, error)
	Put(ctx context.Context, vin, source string, docs []domain.Document) error
}

type Option func(*Service)

func WithCache(cache Cache) Option {
	return func(s *Service) {
		s.cache = cache
	}
}

type Service struct {
	sources []Source
	cache   Cache
}

func NewService(sources []Source, opts ...Option) *Service {
	s := &Service{sources: sources}
	for _, opt := range opts {
		opt(s)
	}
	return s
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
			// Try cache fallback
			if s.cache != nil {
				cached, fetchedAt, cacheErr := s.cache.Get(ctx, vin, r.Source)
				if cacheErr != nil {
					slog.Warn("cache lookup failed", "source", r.Source, "error", cacheErr)
				}
				if cached != nil {
					status.Status = "stale"
					status.FetchedAt = &fetchedAt
					allDocs = append(allDocs, cached...)
					allFailed = false
					sources = append(sources, status)
					continue
				}
			}
			status.Status = "failed"
			status.Error = r.Err.Error()
		} else {
			status.Status = "ok"
			allDocs = append(allDocs, r.Documents...)
			allFailed = false

			// Cache successful response
			if s.cache != nil {
				if err := s.cache.Put(ctx, vin, r.Source, r.Documents); err != nil {
					slog.Warn("cache write failed", "source", r.Source, "error", err)
				}
			}
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

- [ ] **Step 3: Run all aggregator tests**

Run:
```bash
go test ./internal/aggregator/ -v -race
```

Expected: All 8 tests PASS (4 original + 4 new cache tests).

- [ ] **Step 4: Commit**

```bash
git add internal/aggregator/
git commit -m "feat(aggregator): stale-on-failure cache integration with optional Cache interface"
```

---

## Task 5: Audit Integration in Handler

**Files:**
- Modify: `internal/documents/handler.go`
- Modify: `internal/documents/handler_test.go`

---

- [ ] **Step 1: Add audit test**

Add to the end of `internal/documents/handler_test.go`:

```go
type fakeAudit struct {
	entries []domain.AuditEntry
}

func (f *fakeAudit) Insert(_ context.Context, entry domain.AuditEntry) error {
	f.entries = append(f.entries, entry)
	return nil
}

func TestHandler_AuditLogWritten(t *testing.T) {
	audit := &fakeAudit{}
	agg := &fakeAggregator{
		result: &domain.AggregateResult{
			VIN:       "1HGCM82633A004352",
			Documents: []domain.Document{{ID: "D1", Source: "sales"}},
			Sources:   []domain.SourceStatus{{Name: "sales", Status: "ok"}},
		},
	}

	handler := documents.NewHandler(agg, documents.WithAudit(audit))
	r := newTestRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/vehicles/1HGCM82633A004352/documents", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if len(audit.entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(audit.entries))
	}
	entry := audit.entries[0]
	if entry.VIN != "1HGCM82633A004352" {
		t.Errorf("expected VIN in audit, got %s", entry.VIN)
	}
	if entry.HTTPStatus != 200 {
		t.Errorf("expected HTTP 200 in audit, got %d", entry.HTTPStatus)
	}
	if len(entry.Outcomes) != 1 {
		t.Errorf("expected 1 outcome, got %d", len(entry.Outcomes))
	}
}
```

- [ ] **Step 2: Update the documents handler with audit support**

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
	"github.com/tungnguyen/unified-document-viewer/internal/domain"
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

	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(domain.ErrorResponse{
			Error: domain.ErrorDetail{
				Code:    "upstream_failure",
				Message: "all upstream sources failed",
			},
		})
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
```

- [ ] **Step 3: Fix existing tests — NewHandler now accepts options**

The existing tests pass `documents.NewHandler(agg)` which still works because `opts` is variadic. Verify:

Run:
```bash
go test ./internal/documents/ -v -race
```

Expected: All 5 tests PASS (4 original + 1 new audit test).

- [ ] **Step 4: Commit**

```bash
git add internal/documents/
git commit -m "feat(handler): best-effort audit logging on every request"
```

---

## Task 6: Wire Repositories in main.go

**Files:**
- Modify: `cmd/server/main.go`

---

- [ ] **Step 1: Update main.go to wire cache and audit repositories**

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

	httpClient := &http.Client{Timeout: 5 * time.Second}
	salesClient := upstream.NewClient(cfg.SalesMockURL, "sales", httpClient)
	serviceClient := upstream.NewClient(cfg.ServiceMockURL, "service", httpClient)

	aggService := aggregator.NewService(
		[]aggregator.Source{salesClient, serviceClient},
		aggregator.WithCache(cacheRepo),
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

- [ ] **Step 2: Verify compilation and unit tests**

Run:
```bash
go build ./... && go test ./... -short -v -race
```

Expected: Compiles. All unit tests pass (short mode skips integration tests).

- [ ] **Step 3: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat(wiring): connect cache and audit repositories to aggregator and handler"
```

---

## Task 7: Integration Smoke Test

**Files:**
- No new files — validates the full persistence story end-to-end.

---

- [ ] **Step 1: Start the full stack**

Run:
```bash
docker compose up --build -d
```

Wait for healthy:
```bash
docker compose ps --format "table {{.Service}}\t{{.Status}}"
```

Expected: All services (including migrate) show healthy/completed.

- [ ] **Step 2: Test happy path and verify audit log**

Run:
```bash
curl -s http://localhost:8080/vehicles/1HGCM82633A004352/documents > /dev/null
```

Then check audit table:
```bash
docker compose exec postgres psql -U docviewer -c "SELECT request_id, vin, http_status, duration_ms FROM audit_request;"
```

Expected: One row with VIN `1HGCM82633A004352`, http_status `200`.

- [ ] **Step 3: Verify cache was populated**

Run:
```bash
docker compose exec postgres psql -U docviewer -c "SELECT vin, source, fetched_at FROM cached_response;"
```

Expected: Two rows — one for `sales`, one for `service`, both with recent `fetched_at`.

- [ ] **Step 4: Test stale-on-failure**

Inject a fault in the sales mock:
```bash
curl -s -X POST http://localhost:9001/__admin/mappings -H "Content-Type: application/json" -d '{"priority":0,"request":{"method":"GET","urlPathPattern":"/api/v1/vehicles/.*/documents"},"response":{"status":500,"body":"{\"error\":\"simulated\"}"}}'
```

Now fetch again:
```bash
curl -s http://localhost:8080/vehicles/1HGCM82633A004352/documents | python3 -c "
import sys, json
d = json.load(sys.stdin)
for s in d['data']['sources']:
    print(f\"{s['name']}: {s['status']}\", end='')
    if 'fetched_at' in s and s['fetched_at']:
        print(f\" (fetched_at: {s['fetched_at']})\", end='')
    print()
print(f\"Total docs: {len(d['data']['documents'])}\")
"
```

Expected:
- sales: `stale` with a `fetched_at` timestamp
- service: `ok`
- Total docs: 6 (3 cached from sales + 3 live from service)

Clean up:
```bash
curl -s -X POST http://localhost:9001/__admin/mappings/reset > /dev/null
```

- [ ] **Step 5: Test both-fail-with-cache returns stale (not 502)**

```bash
docker compose stop sales-mock service-mock
sleep 1
curl -s http://localhost:8080/vehicles/1HGCM82633A004352/documents | python3 -c "
import sys, json
d = json.load(sys.stdin)
print(f\"HTTP status implied by presence of 'data': {'data' in d}\")
for s in d['data']['sources']:
    print(f\"{s['name']}: {s['status']}\")
print(f\"Total docs: {len(d['data']['documents'])}\")
"
```

Expected: 200 response (not 502) with both sources as `stale` and 6 cached documents.

Bring mocks back:
```bash
docker compose start sales-mock service-mock
```

- [ ] **Step 6: Test both-fail-no-cache returns 502**

Use a different VIN that has never been cached:
```bash
docker compose stop sales-mock service-mock
sleep 1
curl -s -w "\nHTTP %{http_code}" http://localhost:8080/vehicles/2HGCM82633A004352/documents
docker compose start sales-mock service-mock
```

Expected: HTTP 502 with upstream_failure error (no cache exists for this VIN).

- [ ] **Step 7: Tear down**

```bash
docker compose down -v
```

- [ ] **Step 8: Commit if fixes were needed**

```bash
# Only if changes were made:
git add -A
git commit -m "fix: adjustments from persistence integration smoke test"
```

---

## Summary

| Task | Delivers | Requirements |
|------|----------|--------------|
| 1 | Database migrations (audit_request + cached_response) | PERS-01, PERS-02 schema |
| 2 | Cache repository (Put/Get) | PERS-02 |
| 3 | Audit repository (Insert) + AuditEntry type | PERS-01 |
| 4 | Stale-on-failure logic in aggregator | PERS-03 |
| 5 | Audit logging in handler | PERS-01 |
| 6 | Wire repositories in main.go | All (integration) |
| 7 | End-to-end validation | All Phase 3 success criteria |

**Total commits:** 6-7 atomic commits
**Estimated time:** 60-90 minutes for an experienced Go developer
