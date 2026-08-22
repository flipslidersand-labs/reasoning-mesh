package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Ollama   OllamaConfig   `yaml:"ollama"`
	Qdrant   QdrantConfig   `yaml:"qdrant"`
	Embedder EmbedderConfig `yaml:"embedder"`
	Server   ServerConfig   `yaml:"server"`
	Scorer   ScorerConfig   `yaml:"scorer"`
}

type OllamaConfig struct {
	Endpoint       string            `yaml:"endpoint"`
	TimeoutSeconds int               `yaml:"timeout_seconds"`
	Models         map[string]string `yaml:"models"`
}

type QdrantConfig struct {
	Endpoint    string            `yaml:"endpoint"`
	Collections map[string]string `yaml:"collections"`
}

type EmbedderConfig struct {
	Endpoint   string `yaml:"endpoint"`
	APIKey     string `yaml:"api_key"`
	Collection string `yaml:"collection"`
	BatchSize  int    `yaml:"batch_size"`
	Dim        int    `yaml:"dim"`
}

type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type ScorerConfig struct {
	Alpha           float64 `yaml:"alpha"`
	Beta            float64 `yaml:"beta"`
	FreshnessLambda float64 `yaml:"freshness_lambda"`
	TaskBoost       float64 `yaml:"task_boost"`
}

// Validate returns an error listing all missing required fields.
func (c *Config) Validate() error {
	var errs []string
	if c.Ollama.Endpoint == "" {
		errs = append(errs, "ollama.endpoint required")
	}
	if c.Ollama.TimeoutSeconds <= 0 {
		errs = append(errs, "ollama.timeout_seconds must be > 0")
	}
	if c.Qdrant.Endpoint == "" {
		errs = append(errs, "qdrant.endpoint required")
	}
	if c.Embedder.Endpoint == "" {
		errs = append(errs, "embedder.endpoint required")
	}
	if c.Server.Port == 0 {
		errs = append(errs, "server.port required")
	}
	if len(errs) > 0 {
		return fmt.Errorf("invalid config: %s", strings.Join(errs, "; "))
	}
	return nil
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
