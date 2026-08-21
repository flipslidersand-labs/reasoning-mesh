package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Embedder calls the MINIPC e5 embedding service.
type Embedder struct {
	endpoint   string
	apiKey     string
	collection string
	http       *http.Client
}

func NewEmbedder(endpoint string) *Embedder {
	return NewEmbedderWithAuth(endpoint, "", "rm_knowledge")
}

func NewEmbedderWithAuth(endpoint, apiKey, collection string) *Embedder {
	return &Embedder{
		endpoint:   endpoint,
		apiKey:     apiKey,
		collection: collection,
		http:       &http.Client{Timeout: 30 * time.Second},
	}
}

type embedRequest struct {
	Texts      []string `json:"texts"`
	Collection string   `json:"collection"`
}

type embedResponse struct {
	Vectors [][]float32 `json:"vectors"`
}

func (e *Embedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	col := e.collection
	if col == "" {
		col = "rm_knowledge"
	}
	body, _ := json.Marshal(embedRequest{Texts: texts, Collection: col})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint+"/embed/batch", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		req.Header.Set("X-API-Key", e.apiKey)
	}

	resp, err := e.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedder: status %d", resp.StatusCode)
	}

	var result embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Vectors, nil
}

func (e *Embedder) EmbedOne(ctx context.Context, text string) ([]float32, error) {
	vecs, err := e.Embed(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("embedder returned empty result")
	}
	return vecs[0], nil
}
