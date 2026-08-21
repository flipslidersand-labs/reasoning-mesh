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
	endpoint string
	http     *http.Client
}

func NewEmbedder(endpoint string) *Embedder {
	return &Embedder{
		endpoint: endpoint,
		http:     &http.Client{Timeout: 30 * time.Second},
	}
}

type embedRequest struct {
	Texts []string `json:"texts"`
}

type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

func (e *Embedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	body, _ := json.Marshal(embedRequest{Texts: texts})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint+"/embed/batch", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

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
	return result.Embeddings, nil
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
