package upstream

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/sony/gobreaker"
	"github.com/tungnguyen/unified-document-viewer/internal/domain"
)

type ResiliencyConfig struct {
	Timeout          time.Duration
	RetryBaseDelay   time.Duration
	BreakerThreshold uint32
}

type ResilientClient struct {
	inner   *Client
	config  ResiliencyConfig
	breaker *gobreaker.CircuitBreaker
}

func NewResilientClient(baseURL, sourceName string, cfg ResiliencyConfig) *ResilientClient {
	httpClient := &http.Client{Timeout: cfg.Timeout}
	inner := NewClient(baseURL, sourceName, httpClient)

	cb := gobreaker.NewCircuitBreaker(gobreaker.Settings{
		Name: sourceName,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= cfg.BreakerThreshold
		},
	})

	return &ResilientClient{
		inner:   inner,
		config:  cfg,
		breaker: cb,
	}
}

func (rc *ResilientClient) Name() string {
	return rc.inner.Name()
}

func (rc *ResilientClient) Fetch(ctx context.Context, vin string) ([]domain.Document, error) {
	result, err := rc.breaker.Execute(func() (interface{}, error) {
		return rc.fetchWithRetry(ctx, vin)
	})
	if err != nil {
		return nil, err
	}
	return result.([]domain.Document), nil
}

func (rc *ResilientClient) fetchWithRetry(ctx context.Context, vin string) ([]domain.Document, error) {
	docs, err := rc.doFetch(ctx, vin)
	if err == nil {
		return docs, nil
	}

	// One retry with jittered backoff
	jitter := time.Duration(rand.Int64N(int64(rc.config.RetryBaseDelay)))
	delay := rc.config.RetryBaseDelay + jitter

	select {
	case <-time.After(delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	return rc.doFetch(ctx, vin)
}

func (rc *ResilientClient) doFetch(ctx context.Context, vin string) ([]domain.Document, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, rc.config.Timeout)
	defer cancel()

	docs, err := rc.inner.Fetch(timeoutCtx, vin)
	if err != nil {
		if timeoutCtx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("upstream %s timed out after %v", rc.inner.Name(), rc.config.Timeout)
		}
		return nil, err
	}
	return docs, nil
}
