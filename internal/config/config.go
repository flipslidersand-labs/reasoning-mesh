package config

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// maxConfigSize limits config file reads to 1 MiB to prevent OOM from oversized files.
const maxConfigSize = 1 << 20 // 1 MiB

// envVarRe matches ${VAR_NAME} placeholders.
var envVarRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// expandEnv replaces ${VAR} occurrences in s with the corresponding OS environment variable.
func expandEnv(s string) string {
	return envVarRe.ReplaceAllStringFunc(s, func(m string) string {
		name := m[2 : len(m)-1] // strip ${ and }
		if v, ok := os.LookupEnv(name); ok {
			return v
		}
		return m // leave unexpanded if not set
	})
}

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
	if c.Ollama.Models["router"] == "" {
		errs = append(errs, "ollama.models.router required")
	}
	if c.Ollama.Models["knowledge"] == "" {
		errs = append(errs, "ollama.models.knowledge required")
	}
	if len(errs) > 0 {
		return fmt.Errorf("invalid config: %s", strings.Join(errs, "; "))
	}
	return nil
}

// Load reads the YAML config at path, applies ${ENV_VAR} expansion, enforces a
// 1 MiB size limit, and returns a Config. Errors always include the path so
// callers can identify which file caused the problem.
func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, maxConfigSize+1))
	if err != nil {
		return nil, fmt.Errorf("config %s: read: %w", path, err)
	}
	if int64(len(data)) > maxConfigSize {
		return nil, fmt.Errorf("config %s: file exceeds maximum allowed size of %d bytes", path, maxConfigSize)
	}

	// Expand ${ENV_VAR} placeholders before unmarshalling.
	expanded := expandEnv(string(data))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("config %s: parse: %w", path, err)
	}
	return &cfg, nil
}
