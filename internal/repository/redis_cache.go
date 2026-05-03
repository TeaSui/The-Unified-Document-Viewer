package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/tungnguyen/unified-document-viewer/internal/domain"
)

type RedisCacheRepository struct {
	client *redis.Client
	ttl    time.Duration
}

func NewRedisCacheRepository(client *redis.Client, ttl time.Duration) *RedisCacheRepository {
	return &RedisCacheRepository{client: client, ttl: ttl}
}

type cachedEntry struct {
	Documents []domain.Document `json:"documents"`
	FetchedAt time.Time         `json:"fetched_at"`
}

func (r *RedisCacheRepository) Put(ctx context.Context, vin, source string, docs []domain.Document) error {
	entry := cachedEntry{
		Documents: docs,
		FetchedAt: time.Now().UTC(),
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	key := cacheKey(vin, source)
	return r.client.Set(ctx, key, data, r.ttl).Err()
}

func (r *RedisCacheRepository) Get(ctx context.Context, vin, source string) ([]domain.Document, time.Time, error) {
	key := cacheKey(vin, source)
	data, err := r.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, time.Time{}, nil
	}
	if err != nil {
		return nil, time.Time{}, err
	}

	var entry cachedEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, time.Time{}, err
	}

	return entry.Documents, entry.FetchedAt, nil
}

func cacheKey(vin, source string) string {
	return fmt.Sprintf("cache:%s:%s", vin, source)
}
