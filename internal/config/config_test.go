package config

import (
	"strings"
	"testing"
)

func validConfig() *Config {
	return &Config{
		Ollama: OllamaConfig{
			Endpoint:       "http://localhost:11434",
			TimeoutSeconds: 30,
			Models:         map[string]string{"router": "qwen2.5:7b", "knowledge": "ornith:9b"},
		},
		Qdrant:   QdrantConfig{Endpoint: "http://localhost:6333"},
		Embedder: EmbedderConfig{Endpoint: "http://localhost:9092"},
		Server:   ServerConfig{Port: 8765},
	}
}

func TestValidate_OK(t *testing.T) {
	if err := validConfig().Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_MissingOllamaEndpoint(t *testing.T) {
	cfg := validConfig()
	cfg.Ollama.Endpoint = ""
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for missing ollama.endpoint")
	}
	if !strings.Contains(err.Error(), "ollama.endpoint") {
		t.Errorf("error should mention ollama.endpoint: %v", err)
	}
}

func TestValidate_MissingQdrantEndpoint(t *testing.T) {
	cfg := validConfig()
	cfg.Qdrant.Endpoint = ""
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "qdrant.endpoint") {
		t.Errorf("expected qdrant.endpoint error, got %v", err)
	}
}

func TestValidate_MissingEmbedderEndpoint(t *testing.T) {
	cfg := validConfig()
	cfg.Embedder.Endpoint = ""
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "embedder.endpoint") {
		t.Errorf("expected embedder.endpoint error, got %v", err)
	}
}

func TestValidate_MissingServerPort(t *testing.T) {
	cfg := validConfig()
	cfg.Server.Port = 0
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "server.port") {
		t.Errorf("expected server.port error, got %v", err)
	}
}

func TestValidate_ZeroTimeout(t *testing.T) {
	cfg := validConfig()
	cfg.Ollama.TimeoutSeconds = 0
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "timeout_seconds") {
		t.Errorf("expected timeout_seconds error, got %v", err)
	}
}

func TestValidate_MissingRouterModel(t *testing.T) {
	cfg := validConfig()
	cfg.Ollama.Models["router"] = ""
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "ollama.models.router") {
		t.Errorf("expected ollama.models.router error, got %v", err)
	}
}

func TestValidate_MissingKnowledgeModel(t *testing.T) {
	cfg := validConfig()
	cfg.Ollama.Models["knowledge"] = ""
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "ollama.models.knowledge") {
		t.Errorf("expected ollama.models.knowledge error, got %v", err)
	}
}

func TestValidate_MultipleErrors(t *testing.T) {
	cfg := &Config{}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected errors for empty config")
	}
	for _, field := range []string{"ollama.endpoint", "qdrant.endpoint", "embedder.endpoint", "server.port", "ollama.models.router", "ollama.models.knowledge"} {
		if !strings.Contains(err.Error(), field) {
			t.Errorf("error should mention %s: %v", field, err)
		}
	}
}
