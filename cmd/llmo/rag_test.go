package main

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/flipslidersand/reasoning-mesh/internal/config"
	"github.com/flipslidersand/reasoning-mesh/internal/eval"
)

func deadQdrantCfg() *config.Config {
	srv := httptest.NewServer(nil)
	url := srv.URL
	srv.Close() // unreachable endpoint
	cfg := &config.Config{}
	cfg.Qdrant.Endpoint = url
	return cfg
}

func TestBuildRetrieverMap_QdrantDownFailsByDefault(t *testing.T) {
	_, degraded, err := buildRetrieverMap(context.Background(), deadQdrantCfg(), false)
	if err == nil {
		t.Fatal("want error when Qdrant is unreachable and noop not allowed")
	}
	if degraded {
		t.Error("degraded must be false on error")
	}
}

func TestBuildRetrieverMap_AllowNoopMarksDegraded(t *testing.T) {
	rm, degraded, err := buildRetrieverMap(context.Background(), deadQdrantCfg(), true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !degraded {
		t.Error("want degraded=true")
	}
	if _, ok := rm[eval.CondScore].(eval.NoopRetriever); !ok {
		t.Errorf("score retriever = %T, want NoopRetriever", rm[eval.CondScore])
	}
}

func TestNeedsRAG(t *testing.T) {
	if needsRAG([]eval.Condition{eval.CondNoRAG}) {
		t.Error("no-rag only must not need RAG")
	}
	if !needsRAG([]eval.Condition{eval.CondNoRAG, eval.CondScore}) {
		t.Error("score needs RAG")
	}
}
