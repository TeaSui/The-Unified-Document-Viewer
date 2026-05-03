package documents

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/otel/trace"
	"github.com/tungnguyen/unified-document-viewer/internal/domain"
	"github.com/tungnguyen/unified-document-viewer/internal/observability"
	"github.com/tungnguyen/unified-document-viewer/internal/vin"
)

type Aggregator interface {
	Aggregate(ctx context.Context, vinStr string) (*domain.AggregateResult, error)
}

type Audit interface {
	Insert(ctx context.Context, entry domain.AuditEntry) error
}

type HandlerOption func(*Handler)

func WithAudit(audit Audit) HandlerOption {
	return func(h *Handler) {
		h.audit = audit
	}
}

type Handler struct {
	aggregator Aggregator
	audit      Audit
}

func NewHandler(aggregator Aggregator, opts ...HandlerOption) *Handler {
	h := &Handler{aggregator: aggregator}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

func (h *Handler) GetDocuments(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	w.Header().Set("Content-Type", "application/json")

	vinParam := chi.URLParam(r, "vin")
	if err := vin.Validate(vinParam); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(domain.ErrorResponse{
			Error: domain.ErrorDetail{
				Code:    "invalid_vin",
				Message: err.Error(),
			},
		})
		return
	}

	result, err := h.aggregator.Aggregate(r.Context(), vinParam)

	requestID := middleware.GetReqID(r.Context())
	traceID := trace.SpanFromContext(r.Context()).SpanContext().TraceID().String()
	maskedVIN := observability.MaskVIN(vinParam)

	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(domain.ErrorResponse{
			Error: domain.ErrorDetail{
				Code:    "upstream_failure",
				Message: "all upstream sources failed",
			},
		})
		slog.Warn("aggregation failed",
			"request_id", requestID,
			"trace_id", traceID,
			"vin", maskedVIN,
			"duration_ms", time.Since(start).Milliseconds(),
		)
		h.writeAudit(r.Context(), requestID, vinParam, http.StatusBadGateway, start, nil)
		return
	}

	envelope := domain.DocumentsEnvelope{
		Data: domain.DocumentsData{
			VIN:       result.VIN,
			Documents: result.Documents,
			Sources:   result.Sources,
		},
		Meta: domain.ResponseMeta{
			RequestID: requestID,
			Timestamp: time.Now().UTC(),
		},
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(envelope)

	slog.Info("documents served",
		"request_id", requestID,
		"trace_id", traceID,
		"vin", maskedVIN,
		"doc_count", len(result.Documents),
		"sources", sourcesSummary(result.Sources),
		"duration_ms", time.Since(start).Milliseconds(),
	)
	h.writeAudit(r.Context(), requestID, vinParam, http.StatusOK, start, result.Sources)
}

func (h *Handler) writeAudit(ctx context.Context, requestID, vinStr string, status int, start time.Time, outcomes []domain.SourceStatus) {
	if h.audit == nil {
		return
	}

	entry := domain.AuditEntry{
		RequestID:  requestID,
		VIN:        vinStr,
		HTTPStatus: status,
		DurationMs: int(time.Since(start).Milliseconds()),
		Outcomes:   outcomes,
	}

	if err := h.audit.Insert(ctx, entry); err != nil {
		slog.Warn("audit write failed", "error", err, "request_id", requestID)
	}
}

func sourcesSummary(sources []domain.SourceStatus) string {
	result := ""
	for i, s := range sources {
		if i > 0 {
			result += ","
		}
		result += s.Name + ":" + s.Status
	}
	return result
}
