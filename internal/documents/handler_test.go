package documents_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/tungnguyen/unified-document-viewer/internal/documents"
	"github.com/tungnguyen/unified-document-viewer/internal/domain"
)

type fakeAggregator struct {
	result *domain.AggregateResult
	err    error
}

func (f *fakeAggregator) Aggregate(_ context.Context, _ string) (*domain.AggregateResult, error) {
	return f.result, f.err
}

func newTestRouter(handler *documents.Handler) *chi.Mux {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Get("/vehicles/{vin}/documents", handler.GetDocuments)
	return r
}

func TestHandler_ValidVIN_Success(t *testing.T) {
	agg := &fakeAggregator{
		result: &domain.AggregateResult{
			VIN: "1HGCM82633A004352",
			Documents: []domain.Document{
				{ID: "D1", VIN: "1HGCM82633A004352", Source: "sales", Type: "purchase_agreement", Title: "Purchase", Date: time.Now()},
			},
			Sources: []domain.SourceStatus{
				{Name: "sales", Status: "ok"},
				{Name: "service", Status: "ok"},
			},
		},
	}

	handler := documents.NewHandler(agg)
	r := newTestRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/vehicles/1HGCM82633A004352/documents", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var envelope domain.DocumentsEnvelope
	if err := json.NewDecoder(rec.Body).Decode(&envelope); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if envelope.Data.VIN != "1HGCM82633A004352" {
		t.Errorf("expected VIN in response, got %s", envelope.Data.VIN)
	}
	if len(envelope.Data.Documents) != 1 {
		t.Errorf("expected 1 document, got %d", len(envelope.Data.Documents))
	}
	if len(envelope.Data.Sources) != 2 {
		t.Errorf("expected 2 sources, got %d", len(envelope.Data.Sources))
	}
	if envelope.Meta.RequestID == "" {
		t.Error("expected request_id in meta")
	}
	if envelope.Meta.Timestamp.IsZero() {
		t.Error("expected timestamp in meta")
	}
}

func TestHandler_InvalidVIN_Returns400(t *testing.T) {
	handler := documents.NewHandler(&fakeAggregator{})
	r := newTestRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/vehicles/INVALID/documents", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}

	var errResp domain.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.Error.Code != "invalid_vin" {
		t.Errorf("expected error code 'invalid_vin', got '%s'", errResp.Error.Code)
	}
}

func TestHandler_AllSourcesFail_Returns502(t *testing.T) {
	agg := &fakeAggregator{
		err: fmt.Errorf("all upstream sources failed"),
	}

	handler := documents.NewHandler(agg)
	r := newTestRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/vehicles/1HGCM82633A004352/documents", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", rec.Code)
	}

	var errResp domain.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.Error.Code != "upstream_failure" {
		t.Errorf("expected error code 'upstream_failure', got '%s'", errResp.Error.Code)
	}
}

func TestHandler_PartialFailure_Returns200(t *testing.T) {
	agg := &fakeAggregator{
		result: &domain.AggregateResult{
			VIN: "1HGCM82633A004352",
			Documents: []domain.Document{
				{ID: "D1", Source: "sales"},
			},
			Sources: []domain.SourceStatus{
				{Name: "sales", Status: "ok"},
				{Name: "service", Status: "failed", Error: "timeout"},
			},
		},
	}

	handler := documents.NewHandler(agg)
	r := newTestRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/vehicles/1HGCM82633A004352/documents", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for partial failure, got %d", rec.Code)
	}

	var envelope domain.DocumentsEnvelope
	json.NewDecoder(rec.Body).Decode(&envelope)
	if len(envelope.Data.Documents) != 1 {
		t.Errorf("expected 1 document, got %d", len(envelope.Data.Documents))
	}

	var serviceSource domain.SourceStatus
	for _, s := range envelope.Data.Sources {
		if s.Name == "service" {
			serviceSource = s
		}
	}
	if serviceSource.Status != "failed" {
		t.Errorf("expected service status 'failed', got '%s'", serviceSource.Status)
	}
}

type fakeAudit struct {
	entries []domain.AuditEntry
}

func (f *fakeAudit) Insert(_ context.Context, entry domain.AuditEntry) error {
	f.entries = append(f.entries, entry)
	return nil
}

func TestHandler_AuditLogWritten(t *testing.T) {
	audit := &fakeAudit{}
	agg := &fakeAggregator{
		result: &domain.AggregateResult{
			VIN:       "1HGCM82633A004352",
			Documents: []domain.Document{{ID: "D1", Source: "sales"}},
			Sources:   []domain.SourceStatus{{Name: "sales", Status: "ok"}},
		},
	}

	handler := documents.NewHandler(agg, documents.WithAudit(audit))
	r := newTestRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/vehicles/1HGCM82633A004352/documents", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if len(audit.entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(audit.entries))
	}
	entry := audit.entries[0]
	if entry.VIN != "1HGCM82633A004352" {
		t.Errorf("expected VIN in audit, got %s", entry.VIN)
	}
	if entry.HTTPStatus != 200 {
		t.Errorf("expected HTTP 200 in audit, got %d", entry.HTTPStatus)
	}
	if len(entry.Outcomes) != 1 {
		t.Errorf("expected 1 outcome, got %d", len(entry.Outcomes))
	}
}
