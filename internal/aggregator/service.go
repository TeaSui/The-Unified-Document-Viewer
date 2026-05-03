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
