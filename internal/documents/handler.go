package documents

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/tungnguyen/unified-document-viewer/internal/domain"
	"github.com/tungnguyen/unified-document-viewer/internal/vin"
)

type Aggregator interface {
	Aggregate(ctx context.Context, vinStr string) (*domain.AggregateResult, error)
}

type Handler struct {
	aggregator Aggregator
}

func NewHandler(aggregator Aggregator) *Handler {
	return &Handler{aggregator: aggregator}
}

func (h *Handler) GetDocuments(w http.ResponseWriter, r *http.Request) {
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
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(domain.ErrorResponse{
			Error: domain.ErrorDetail{
				Code:    "upstream_failure",
				Message: "all upstream sources failed",
			},
		})
		return
	}

	requestID := middleware.GetReqID(r.Context())

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
}
