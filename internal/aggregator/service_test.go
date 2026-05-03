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
	expected := []string{"S2", "V1", "S1"}
	for i, id := range expected {
		if result.Documents[i].ID != id {
			t.Errorf("position %d: expected %s, got %s", i, id, result.Documents[i].ID)
		}
	}
}

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
	cache := newFakeCache()

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

	if _, ok := cache.data["1HGCM82633A004352:sales"]; !ok {
		t.Error("expected sales docs to be cached")
	}
	if _, ok := cache.data["1HGCM82633A004352:service"]; !ok {
		t.Error("expected service docs to be cached")
	}
}

func TestService_DeadlineExceeded(t *testing.T) {
	sales := &fakeSource{
		name: "sales",
		docs: []domain.Document{{ID: "S1", Source: "sales", Date: time.Now()}},
	}
	service := &fakeSource{
		name: "service",
		docs: []domain.Document{{ID: "V1", Source: "service", Date: time.Now()}},
	}

	svc := aggregator.NewService(
		[]aggregator.Source{sales, service},
		aggregator.WithDeadline(50*time.Millisecond),
	)

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

func TestService_EmptyDocumentsFromBothSources(t *testing.T) {
	sales := &fakeSource{name: "sales", docs: []domain.Document{}}
	service := &fakeSource{name: "service", docs: []domain.Document{}}

	svc := aggregator.NewService([]aggregator.Source{sales, service})
	result, err := svc.Aggregate(context.Background(), "1HGCM82633A004352")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Documents) != 0 {
		t.Errorf("expected 0 documents, got %d", len(result.Documents))
	}
	if result.VIN != "1HGCM82633A004352" {
		t.Errorf("expected VIN in result, got %s", result.VIN)
	}
}

func TestService_DocumentsWithSameDateStableOrder(t *testing.T) {
	sameDate := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	sales := &fakeSource{
		name: "sales",
		docs: []domain.Document{
			{ID: "S1", Source: "sales", Date: sameDate},
			{ID: "S2", Source: "sales", Date: sameDate},
		},
	}
	service := &fakeSource{
		name: "service",
		docs: []domain.Document{
			{ID: "V1", Source: "service", Date: sameDate},
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
}

func TestService_SingleSourceReturnsMany(t *testing.T) {
	sales := &fakeSource{
		name: "sales",
		docs: []domain.Document{
			{ID: "S1", Source: "sales", Date: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
			{ID: "S2", Source: "sales", Date: time.Date(2024, 12, 1, 0, 0, 0, 0, time.UTC)},
			{ID: "S3", Source: "sales", Date: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)},
		},
	}
	service := &fakeSource{name: "service", err: fmt.Errorf("down")}

	svc := aggregator.NewService([]aggregator.Source{sales, service})
	result, err := svc.Aggregate(context.Background(), "1HGCM82633A004352")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Documents) != 3 {
		t.Fatalf("expected 3 docs, got %d", len(result.Documents))
	}
	// Dec > Jun > Jan
	expected := []string{"S2", "S3", "S1"}
	for i, id := range expected {
		if result.Documents[i].ID != id {
			t.Errorf("position %d: expected %s, got %s", i, id, result.Documents[i].ID)
		}
	}
}
