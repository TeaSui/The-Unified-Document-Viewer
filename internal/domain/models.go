package domain

import "time"

type Document struct {
	ID       string         `json:"id"`
	VIN      string         `json:"vin"`
	Source   string         `json:"source"`
	Type     string         `json:"type"`
	Title    string         `json:"title"`
	Date     time.Time      `json:"date"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type SourceStatus struct {
	Name      string     `json:"name"`
	Status    string     `json:"status"`
	Error     string     `json:"error,omitempty"`
	FetchedAt *time.Time `json:"fetched_at,omitempty"`
}

type SourceResult struct {
	Source    string
	Documents []Document
	Err       error
}

type AggregateResult struct {
	VIN       string
	Documents []Document
	Sources   []SourceStatus
}

type DocumentsEnvelope struct {
	Data DocumentsData `json:"data"`
	Meta ResponseMeta  `json:"meta"`
}

type DocumentsData struct {
	VIN       string         `json:"vin"`
	Documents []Document     `json:"documents"`
	Sources   []SourceStatus `json:"sources"`
}

type ResponseMeta struct {
	RequestID string    `json:"request_id"`
	Timestamp time.Time `json:"timestamp"`
}

type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
