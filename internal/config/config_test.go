package config

import (
	"os"
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

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "/nonexistent/path/config.yaml") {
		t.Errorf("error should contain path: %v", err)
	}
}

func TestLoad_EnvExpansion(t *testing.T) {
	t.Setenv("TEST_API_KEY", "secret123")
	yaml := `
ollama:
  endpoint: "http://localhost:11434"
  timeout_seconds: 30
  models:
    router: "qwen2.5:7b"
    knowledge: "ornith:9b"
qdrant:
  endpoint: "http://localhost:6333"
embedder:
  endpoint: "http://localhost:9092"
  api_key: "${TEST_API_KEY}"
server:
  port: 8765
`
	f, err := os.CreateTemp(t.TempDir(), "config-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(yaml); err != nil {
		t.Fatal(err)
	}
	f.Close()

	cfg, err := Load(f.Name())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Embedder.APIKey != "secret123" {
		t.Errorf("expected api_key=secret123, got %q", cfg.Embedder.APIKey)
	}
}

func TestLoad_SizeLimit(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "big-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	// Write just over 1 MiB
	chunk := make([]byte, 1024)
	for i := range chunk {
		chunk[i] = 'x'
	}
	for i := 0; i < 1025; i++ {
		if _, err := f.Write(chunk); err != nil {
			t.Fatal(err)
		}
	}
	f.Close()

	_, err = Load(f.Name())
	if err == nil {
		t.Fatal("expected error for oversized file")
	}
	if !strings.Contains(err.Error(), "exceeds maximum") {
		t.Errorf("error should mention size limit: %v", err)
	}
}
