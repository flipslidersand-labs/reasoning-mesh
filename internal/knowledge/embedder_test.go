package knowledge_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/flipslidersand/reasoning-mesh/internal/knowledge"
)

type capturedEmbed struct {
	method, path, apiKey string
	hasKey               bool
	body                 struct {
		Texts      []string `json:"texts"`
		Collection string   `json:"collection"`
	}
}

func embedStub(t *testing.T, status int, resp string, got *capturedEmbed) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got != nil {
			got.method, got.path = r.Method, r.URL.Path
			_, got.hasKey = r.Header["X-Api-Key"]
			got.apiKey = r.Header.Get("X-API-Key")
			_ = json.NewDecoder(r.Body).Decode(&got.body)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(resp))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestEmbedder_Embed_RequestShape(t *testing.T) {
	tests := []struct {
		name, apiKey, collection string
		wantKey                  bool
		wantCollection           string
	}{
		{"no key, default collection", "", "rm_knowledge", false, "rm_knowledge"},
		{"key set, custom collection", "secret", "custom", true, "custom"},
		{"empty collection falls back", "", "", false, "rm_knowledge"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got capturedEmbed
			srv := embedStub(t, 200, `{"vectors":[[1,2],[3,4]]}`, &got)
			e := knowledge.NewEmbedderWithAuth(srv.URL, tt.apiKey, tt.collection)
			vecs, err := e.Embed(context.Background(), []string{"a", "b"})
			if err != nil {
				t.Fatalf("Embed: %v", err)
			}
			if len(vecs) != 2 || vecs[1][1] != 4 {
				t.Errorf("vecs = %v", vecs)
			}
			if got.method != http.MethodPost || got.path != "/embed/batch" {
				t.Errorf("request = %s %s", got.method, got.path)
			}
			if got.hasKey != tt.wantKey || got.apiKey != tt.apiKey {
				t.Errorf("X-API-Key present=%v value=%q", got.hasKey, got.apiKey)
			}
			if got.body.Collection != tt.wantCollection {
				t.Errorf("collection = %q, want %q", got.body.Collection, tt.wantCollection)
			}
			if len(got.body.Texts) != 2 || got.body.Texts[0] != "a" {
				t.Errorf("texts = %v", got.body.Texts)
			}
		})
	}
}

func TestNewEmbedder_UsesDefaultCollectionAndNoKey(t *testing.T) {
	var got capturedEmbed
	srv := embedStub(t, 200, `{"vectors":[[1]]}`, &got)
	if _, err := knowledge.NewEmbedder(srv.URL).Embed(context.Background(), []string{"x"}); err != nil {
		t.Fatal(err)
	}
	if got.hasKey || got.body.Collection != "rm_knowledge" {
		t.Errorf("hasKey=%v collection=%q", got.hasKey, got.body.Collection)
	}
}

func TestEmbedder_Embed_Errors(t *testing.T) {
	t.Run("non-200", func(t *testing.T) {
		srv := embedStub(t, 500, `boom`, nil)
		_, err := knowledge.NewEmbedder(srv.URL).Embed(context.Background(), []string{"a"})
		if err == nil || !strings.Contains(err.Error(), "status 500") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("invalid JSON", func(t *testing.T) {
		srv := embedStub(t, 200, `{not json`, nil)
		if _, err := knowledge.NewEmbedder(srv.URL).Embed(context.Background(), []string{"a"}); err == nil {
			t.Error("expected decode error")
		}
	})
	t.Run("canceled context", func(t *testing.T) {
		srv := embedStub(t, 200, `{"vectors":[[1]]}`, nil)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := knowledge.NewEmbedder(srv.URL).Embed(ctx, []string{"a"}); err == nil {
			t.Error("expected context error")
		}
	})
	t.Run("bad endpoint URL", func(t *testing.T) {
		if _, err := knowledge.NewEmbedder("http://bad\x7fhost").Embed(context.Background(), nil); err == nil {
			t.Error("expected request build error")
		}
	})
}

func TestEmbedder_EmbedOne(t *testing.T) {
	t.Run("returns first vector", func(t *testing.T) {
		srv := embedStub(t, 200, `{"vectors":[[0.5,1.5]]}`, nil)
		v, err := knowledge.NewEmbedder(srv.URL).EmbedOne(context.Background(), "a")
		if err != nil || len(v) != 2 || v[0] != 0.5 {
			t.Errorf("v=%v err=%v", v, err)
		}
	})
	t.Run("empty vectors", func(t *testing.T) {
		srv := embedStub(t, 200, `{"vectors":[]}`, nil)
		_, err := knowledge.NewEmbedder(srv.URL).EmbedOne(context.Background(), "a")
		if err == nil || !strings.Contains(err.Error(), "embedder returned empty result") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("propagates Embed error", func(t *testing.T) {
		srv := embedStub(t, 503, ``, nil)
		if _, err := knowledge.NewEmbedder(srv.URL).EmbedOne(context.Background(), "a"); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("deadline exceeded", func(t *testing.T) {
		release := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-release:
			case <-r.Context().Done():
			}
		}))
		t.Cleanup(srv.Close)
		t.Cleanup(func() { close(release) }) // runs before srv.Close (LIFO)
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		if _, err := knowledge.NewEmbedder(srv.URL).EmbedOne(ctx, "a"); err == nil {
			t.Error("expected deadline error")
		}
	})
}
