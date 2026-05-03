package upstream_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tungnguyen/unified-document-viewer/internal/upstream"
)

func TestClient_Fetch_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/vehicles/1HGCM82633A004352/documents" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"documents": []map[string]any{
				{
					"id":    "DOC-001",
					"vin":   "1HGCM82633A004352",
					"type":  "purchase_agreement",
					"title": "Purchase Agreement",
					"date":  "2024-01-15T10:30:00Z",
				},
			},
		})
	}))
	defer srv.Close()

	client := upstream.NewClient(srv.URL, "sales", &http.Client{Timeout: 5 * time.Second})
	docs, err := client.Fetch(context.Background(), "1HGCM82633A004352")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 document, got %d", len(docs))
	}
	if docs[0].ID != "DOC-001" {
		t.Errorf("expected id DOC-001, got %s", docs[0].ID)
	}
	if docs[0].Source != "sales" {
		t.Errorf("expected source 'sales', got %s", docs[0].Source)
	}
}

func TestClient_Fetch_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"internal_server_error"}`))
	}))
	defer srv.Close()

	client := upstream.NewClient(srv.URL, "sales", &http.Client{Timeout: 5 * time.Second})
	_, err := client.Fetch(context.Background(), "1HGCM82633A004352")

	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestClient_Fetch_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := upstream.NewClient(srv.URL, "service", &http.Client{Timeout: 100 * time.Millisecond})
	_, err := client.Fetch(context.Background(), "1HGCM82633A004352")

	if err == nil {
		t.Fatal("expected error for timeout")
	}
}
