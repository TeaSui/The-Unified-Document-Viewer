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
