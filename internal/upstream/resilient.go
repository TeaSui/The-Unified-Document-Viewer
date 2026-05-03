package upstream

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/sony/gobreaker"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"github.com/tungnguyen/unified-document-viewer/internal/domain"
)

var (
	upstreamTracer  = otel.Tracer("upstream")
	upstreamMeter   = otel.Meter("upstream")
	upstreamLatency metric.Float64Histogram
	upstreamCalls   metric.Int64Counter
)

func init() {
	var err error
	upstreamLatency, err = upstreamMeter.Float64Histogram("upstream.request.duration",
		metric.WithDescription("Upstream request duration in seconds"),
		metric.WithUnit("s"),
	)
	if err != nil {
		panic(err)
	}
	upstreamCalls, err = upstreamMeter.Int64Counter("upstream.request.total",
		metric.WithDescription("Total upstream requests"),
	)
	if err != nil {
		panic(err)
	}
}

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
	ctx, span := upstreamTracer.Start(ctx, "upstream."+rc.inner.Name(),
		trace.WithAttributes(
			attribute.String("upstream.source", rc.inner.Name()),
			attribute.String("upstream.vin_suffix", vinSuffix(vin)),
		),
	)
	defer span.End()

	start := time.Now()

	result, err := rc.breaker.Execute(func() (interface{}, error) {
		return rc.fetchWithRetry(ctx, vin)
	})

	duration := time.Since(start).Seconds()
	outcome := "ok"
	if err != nil {
		outcome = classifyError(err)
		span.SetAttributes(attribute.String("upstream.outcome", outcome))
		span.RecordError(err)
	} else {
		span.SetAttributes(attribute.String("upstream.outcome", "ok"))
	}

	attrs := metric.WithAttributes(
		attribute.String("source", rc.inner.Name()),
		attribute.String("outcome", outcome),
	)
	upstreamLatency.Record(ctx, duration, attrs)
	upstreamCalls.Add(ctx, 1, attrs)

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

func classifyError(err error) string {
	if err == gobreaker.ErrOpenState || err == gobreaker.ErrTooManyRequests {
		return "circuit_open"
	}
	if err == context.DeadlineExceeded {
		return "timeout"
	}
	return "failed"
}

func vinSuffix(vin string) string {
	if len(vin) <= 6 {
		return vin
	}
	return vin[len(vin)-6:]
}
