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
