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
