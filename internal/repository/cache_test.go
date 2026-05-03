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

// newTestPool is defined in audit_test.go and shared across the repository_test package.
