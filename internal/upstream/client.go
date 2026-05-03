package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/tungnguyen/unified-document-viewer/internal/domain"
)

type Client struct {
	baseURL    string
	sourceName string
	httpClient *http.Client
}

func NewClient(baseURL, sourceName string, httpClient *http.Client) *Client {
	return &Client{
		baseURL:    baseURL,
		sourceName: sourceName,
		httpClient: httpClient,
	}
}

func (c *Client) Name() string {
	return c.sourceName
}

type upstreamResponse struct {
	Documents []upstreamDocument `json:"documents"`
}

type upstreamDocument struct {
	ID       string         `json:"id"`
	VIN      string         `json:"vin"`
	Type     string         `json:"type"`
	Title    string         `json:"title"`
	Date     string         `json:"date"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

func (c *Client) Fetch(ctx context.Context, vin string) ([]domain.Document, error) {
	url := fmt.Sprintf("%s/api/v1/vehicles/%s/documents", c.baseURL, vin)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching from %s: %w", c.sourceName, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream %s returned status %d", c.sourceName, resp.StatusCode)
	}

	var body upstreamResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decoding response from %s: %w", c.sourceName, err)
	}

	docs := make([]domain.Document, 0, len(body.Documents))
	for _, d := range body.Documents {
		parsed, err := time.Parse(time.RFC3339, d.Date)
		if err != nil {
			parsed = time.Time{}
		}
		docs = append(docs, domain.Document{
			ID:       d.ID,
			VIN:      d.VIN,
			Source:   c.sourceName,
			Type:     d.Type,
			Title:    d.Title,
			Date:     parsed,
			Metadata: d.Metadata,
		})
	}

	return docs, nil
}
