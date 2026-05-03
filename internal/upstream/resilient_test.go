package upstream_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tungnguyen/unified-document-viewer/internal/upstream"
)

func TestResilientClient_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"documents": []map[string]any{
				{"id": "D1", "vin": "1HGCM82633A004352", "type": "x", "title": "X", "date": "2024-01-01T00:00:00Z"},
			},
		})
	}))
	defer srv.Close()

	client := upstream.NewResilientClient(srv.URL, "sales", upstream.ResiliencyConfig{
		Timeout:          800 * time.Millisecond,
		RetryBaseDelay:   50 * time.Millisecond,
		BreakerThreshold: 5,
	})

	docs, err := client.Fetch(context.Background(), "1HGCM82633A004352")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
}

func TestResilientClient_TimeoutExceeded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"documents": []any{}})
	}))
	defer srv.Close()

	client := upstream.NewResilientClient(srv.URL, "sales", upstream.ResiliencyConfig{
		Timeout:          100 * time.Millisecond,
		RetryBaseDelay:   10 * time.Millisecond,
		BreakerThreshold: 5,
	})

	_, err := client.Fetch(context.Background(), "1HGCM82633A004352")
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestResilientClient_RetriesOnce(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"documents": []map[string]any{
				{"id": "D1", "vin": "1HGCM82633A004352", "type": "x", "title": "X", "date": "2024-01-01T00:00:00Z"},
			},
		})
	}))
	defer srv.Close()

	client := upstream.NewResilientClient(srv.URL, "sales", upstream.ResiliencyConfig{
		Timeout:          800 * time.Millisecond,
		RetryBaseDelay:   10 * time.Millisecond,
		BreakerThreshold: 5,
	})

	docs, err := client.Fetch(context.Background(), "1HGCM82633A004352")
	if err != nil {
		t.Fatalf("expected success after retry, got: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	if attempts.Load() != 2 {
		t.Errorf("expected 2 attempts (1 fail + 1 retry), got %d", attempts.Load())
	}
}

func TestResilientClient_MaxOneRetry(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := upstream.NewResilientClient(srv.URL, "sales", upstream.ResiliencyConfig{
		Timeout:          800 * time.Millisecond,
		RetryBaseDelay:   10 * time.Millisecond,
		BreakerThreshold: 50,
	})

	_, err := client.Fetch(context.Background(), "1HGCM82633A004352")
	if err == nil {
		t.Fatal("expected error after all retries exhausted")
	}
	if attempts.Load() != 2 {
		t.Errorf("expected exactly 2 attempts, got %d", attempts.Load())
	}
}

func TestResilientClient_CircuitBreakerOpens(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := upstream.NewResilientClient(srv.URL, "sales", upstream.ResiliencyConfig{
		Timeout:          800 * time.Millisecond,
		RetryBaseDelay:   1 * time.Millisecond,
		BreakerThreshold: 3,
	})

	// Trip the breaker: 3 consecutive failures (each = 2 attempts due to retry)
	for i := 0; i < 3; i++ {
		client.Fetch(context.Background(), "1HGCM82633A004352")
	}

	attemptsBeforeOpen := attempts.Load()

	// Next call should be short-circuited (no HTTP request)
	_, err := client.Fetch(context.Background(), "1HGCM82633A004352")
	if err == nil {
		t.Fatal("expected circuit breaker error")
	}

	attemptsAfterOpen := attempts.Load()
	if attemptsAfterOpen != attemptsBeforeOpen {
		t.Errorf("expected no new HTTP attempts after breaker opened, got %d more", attemptsAfterOpen-attemptsBeforeOpen)
	}
}
