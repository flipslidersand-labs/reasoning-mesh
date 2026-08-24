package qdrant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Client struct {
	endpoint string
	http     *http.Client
}

func New(endpoint string) *Client {
	return &Client{
		endpoint: endpoint,
		http:     &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("qdrant ping: status %d", resp.StatusCode)
	}
	return nil
}

type upsertRequest struct {
	Points []point `json:"points"`
}

type point struct {
	ID      string         `json:"id"`
	Vector  []float32      `json:"vector"`
	Payload map[string]any `json:"payload"`
}

// UpsertPoint holds the data for a single point in a BulkUpsert call.
type UpsertPoint struct {
	ID      string
	Vectors []float32
	Payload map[string]any
}

// BulkUpsert sends all points in a single PUT /collections/{name}/points request,
// eliminating the N+1 HTTP overhead of calling Upsert in a loop.
// If points is empty, it returns nil immediately.
func (c *Client) BulkUpsert(ctx context.Context, collection string, points []UpsertPoint) error {
	if len(points) == 0 {
		return nil
	}
	pts := make([]point, len(points))
	for i, p := range points {
		pts[i] = point{ID: p.ID, Vector: p.Vectors, Payload: p.Payload}
	}
	body, err := json.Marshal(upsertRequest{Points: pts})
	if err != nil {
		return fmt.Errorf("marshal %T: %w", upsertRequest{}, err)
	}
	url := fmt.Sprintf("%s/collections/%s/points", c.endpoint, collection)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("qdrant bulk upsert: status %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) Upsert(ctx context.Context, collection, id string, vector []float32, payload map[string]any) error {
	body, err := json.Marshal(upsertRequest{
		Points: []point{{ID: id, Vector: vector, Payload: payload}},
	})
	if err != nil {
		return fmt.Errorf("marshal %T: %w", upsertRequest{}, err)
	}
	url := fmt.Sprintf("%s/collections/%s/points", c.endpoint, collection)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("qdrant upsert: status %d", resp.StatusCode)
	}
	return nil
}

type searchRequest struct {
	Vector      []float32      `json:"vector"`
	Limit       int            `json:"limit"`
	WithPayload bool           `json:"with_payload"`
	Filter      map[string]any `json:"filter,omitempty"`
}

type SearchResult struct {
	ID      string         `json:"id"`
	Score   float64        `json:"score"`
	Payload map[string]any `json:"payload"`
}

func (c *Client) Search(ctx context.Context, collection string, vector []float32, topK int, filter map[string]any) ([]SearchResult, error) {
	body, err := json.Marshal(searchRequest{
		Vector:      vector,
		Limit:       topK,
		WithPayload: true,
		Filter:      filter,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal %T: %w", searchRequest{}, err)
	}
	url := fmt.Sprintf("%s/collections/%s/points/search", c.endpoint, collection)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("qdrant search: status %d", resp.StatusCode)
	}

	var result struct {
		Result []SearchResult `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Result, nil
}

// GetByID fetches a single point by its ID.
func (c *Client) GetByID(ctx context.Context, collection, id string) ([]SearchResult, error) {
	body, err := json.Marshal(map[string]any{
		"ids":          []string{id},
		"with_payload": true,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal GetByID payload: %w", err)
	}
	url := fmt.Sprintf("%s/collections/%s/points", c.endpoint, collection)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("qdrant get: status %d", resp.StatusCode)
	}

	var result struct {
		Result []SearchResult `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Result, nil
}

// EnsureCollection checks whether the named collection exists and creates it
// if not (dim=vectorDim, distance=Cosine). Safe to call on every startup.
func (c *Client) EnsureCollection(ctx context.Context, collection string, vectorDim int) error {
	url := fmt.Sprintf("%s/collections/%s", c.endpoint, collection)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil // already exists
	}
	if resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("qdrant collection check: status %d", resp.StatusCode)
	}

	// Create the collection
	body, err := json.Marshal(map[string]any{
		"vectors": map[string]any{
			"size":     vectorDim,
			"distance": "Cosine",
		},
	})
	if err != nil {
		return fmt.Errorf("marshal create collection: %w", err)
	}
	req, err = http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err = c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("qdrant create collection: status %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) UpdatePayload(ctx context.Context, collection, id string, payload map[string]any) error {
	body, err := json.Marshal(map[string]any{
		"payload": payload,
		"points":  []string{id},
	})
	if err != nil {
		return fmt.Errorf("marshal UpdatePayload payload: %w", err)
	}
	url := fmt.Sprintf("%s/collections/%s/points/payload", c.endpoint, collection)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("qdrant update payload: status %d", resp.StatusCode)
	}
	return nil
}
