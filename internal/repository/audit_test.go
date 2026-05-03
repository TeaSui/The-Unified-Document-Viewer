package repository_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tungnguyen/unified-document-viewer/internal/domain"
	"github.com/tungnguyen/unified-document-viewer/internal/repository"
)

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
