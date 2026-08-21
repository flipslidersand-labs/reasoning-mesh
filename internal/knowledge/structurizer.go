package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/flipslidersand/reasoning-mesh/internal/eval"
	"github.com/flipslidersand/reasoning-mesh/internal/ollama"
)

// StructuredChunk is the output of field-level extraction on a raw Chunk.
type StructuredChunk struct {
	Content   string
	TaskType  eval.TaskType
	Language  string
	Framework string
	Tags      []string
}

// Structurizer uses qwen2.5:7b to extract metadata from raw chunks.
type Structurizer struct {
	client    *ollama.Client
	routerModel string
}

// NewStructurizer creates a Structurizer that uses the given Ollama client.
func NewStructurizer(client *ollama.Client, routerModel string) *Structurizer {
	return &Structurizer{client: client, routerModel: routerModel}
}

var knownTaskTypes = map[string]eval.TaskType{
	"debugging":      eval.TaskDebugging,
	"implementation": eval.TaskImplementation,
	"architecture":   eval.TaskArchitecture,
	"testing":        eval.TaskTesting,
}

// Structurize extracts task_type, language, framework, and tags from chunk content.
// If the LLM call fails, it falls back to hint values from the Chunk.
func (s *Structurizer) Structurize(ctx context.Context, chunk Chunk) (StructuredChunk, error) {
	prompt := fmt.Sprintf(`以下のコードチャンクから、技術メタデータを抽出してください。
JSONのみで返答してください（説明文なし）。

コード:
%s

出力形式:
{
  "task_type": "debugging|implementation|architecture|testing",
  "language": "go|rust|python|typescript|etc",
  "framework": "具体的なフレームワーク名、なければ空文字",
  "tags": ["キーワード1", "キーワード2"]
}`, chunk.Content)

	raw, err := s.client.Generate(ctx, s.routerModel, prompt)
	if err != nil {
		// fallback to hint
		return StructuredChunk{
			Content:  chunk.Content,
			TaskType: chunk.HintTaskType,
		}, nil
	}

	return parseStructurizerOutput(raw, chunk)
}

type structurizerJSON struct {
	TaskType  string   `json:"task_type"`
	Language  string   `json:"language"`
	Framework string   `json:"framework"`
	Tags      []string `json:"tags"`
}

func parseStructurizerOutput(raw string, chunk Chunk) (StructuredChunk, error) {
	// Extract JSON block from potentially noisy LLM output
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start == -1 || end == -1 || end <= start {
		return StructuredChunk{Content: chunk.Content, TaskType: chunk.HintTaskType}, nil
	}
	jsonStr := raw[start : end+1]

	var parsed structurizerJSON
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return StructuredChunk{Content: chunk.Content, TaskType: chunk.HintTaskType}, nil
	}

	taskType := chunk.HintTaskType
	if tt, ok := knownTaskTypes[strings.ToLower(parsed.TaskType)]; ok {
		taskType = tt
	}

	return StructuredChunk{
		Content:   chunk.Content,
		TaskType:  taskType,
		Language:  parsed.Language,
		Framework: parsed.Framework,
		Tags:      parsed.Tags,
	}, nil
}
