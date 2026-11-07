package config

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/flipslidersand/reasoning-mesh/internal/knowledge"
	"github.com/flipslidersand/reasoning-mesh/internal/validate"
)

// maxConfigSize limits config file reads to 1 MiB to prevent OOM from oversized files.
const maxConfigSize = 1 << 20 // 1 MiB

// envVarRe matches ${VAR} and ${VAR:-default} placeholders.
var envVarRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(:-([^}]*))?\}`)

// expandEnv replaces ${VAR} occurrences in s with the corresponding OS environment
// variable. ${VAR:-default} falls back to default when VAR is unset (use
// ${VAR:-} to allow an empty value). Names of unset variables that have no
// default are returned (never their values). Full-line YAML comments are left
// untouched so documentation examples do not trigger errors.
func expandEnv(s string) (string, []string) {
	var missing []string
	seen := map[string]bool{}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		lines[i] = envVarRe.ReplaceAllStringFunc(line, func(m string) string {
			sub := envVarRe.FindStringSubmatch(m)
			if v, ok := os.LookupEnv(sub[1]); ok {
				return v
			}
			if sub[2] != "" {
				return sub[3]
			}
			if !seen[sub[1]] {
				seen[sub[1]] = true
				missing = append(missing, sub[1])
			}
			return m
		})
	}
	return strings.Join(lines, "\n"), missing
}

type Config struct {
	Ollama   OllamaConfig           `yaml:"ollama"`
	Qdrant   QdrantConfig           `yaml:"qdrant"`
	Embedder EmbedderConfig         `yaml:"embedder"`
	Server   ServerConfig           `yaml:"server"`
	Scorer   knowledge.ScorerConfig `yaml:"scorer"`
}

type OllamaConfig struct {
	Endpoint       string            `yaml:"endpoint"`
	TimeoutSeconds int               `yaml:"timeout_seconds"`
	Models         map[string]string `yaml:"models"`
	// NumPredict caps generated tokens per /api/generate call.
	// 0 = client default (ollama.DefaultNumPredict).
	NumPredict int `yaml:"num_predict"`
}

type QdrantConfig struct {
	Endpoint string `yaml:"endpoint"`
	// APIKey is sent as the api-key header when set (use ${QDRANT_API_KEY}).
	// Empty = unauthenticated Qdrant.
	APIKey      string            `yaml:"api_key"`
	Collections map[string]string `yaml:"collections"`
	// UpsertBatchSize controls how many points are sent per BulkUpsert call
	// in Extractor.Run. Default 0 means the extractor uses its built-in default (32).
	UpsertBatchSize int `yaml:"upsert_batch_size"`
	// TimeoutSeconds is the HTTP client timeout for Qdrant requests.
	// Default 0 means the client uses its built-in default (30s).
	TimeoutSeconds int `yaml:"timeout_seconds"`
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

// Validate returns an error listing all missing or invalid required fields.
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
	// Validate each collection name to prevent path traversal via config.
	for key, name := range c.Qdrant.Collections {
		if err := validate.CollectionName(name); err != nil {
			errs = append(errs, fmt.Sprintf("qdrant.collections.%s: %v", key, err))
		}
	}
	if c.Embedder.Collection != "" {
		if err := validate.CollectionName(c.Embedder.Collection); err != nil {
			errs = append(errs, fmt.Sprintf("embedder.collection: %v", err))
		}
	}
	if c.Scorer.Alpha <= 0 {
		errs = append(errs, "scorer.alpha must be > 0")
	}
	if c.Scorer.Beta <= 0 {
		errs = append(errs, "scorer.beta must be > 0")
	}
	if c.Scorer.FreshnessLambda < 0 {
		errs = append(errs, "scorer.freshness_lambda must be >= 0")
	}
	if c.Scorer.TaskBoost <= 0 {
		errs = append(errs, "scorer.task_boost must be > 0")
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
	expanded, missing := expandEnv(string(data))
	if len(missing) > 0 {
		return nil, fmt.Errorf("config %s: unset env var(s): %s", path, strings.Join(missing, ", "))
	}

	// Defaults first so an omitted or partial scorer: keeps the rest (#274).
	cfg := Config{Scorer: knowledge.DefaultScorerConfig()}
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("config %s: parse: %w", path, err)
	}
	return &cfg, nil
}
