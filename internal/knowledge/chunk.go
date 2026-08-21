package knowledge

import (
	"strings"

	"github.com/flipslidersand/reasoning-mesh/internal/eval"
)

// Chunk is a decision point extracted from a git diff or CI log.
type Chunk struct {
	Content      string
	HintTaskType eval.TaskType
}

// ChunkDiff splits a git diff into decision-point chunks.
// Each hunk header (@@) starts a new chunk; small hunks are merged.
func ChunkDiff(diff string) []Chunk {
	if strings.TrimSpace(diff) == "" {
		return nil
	}

	var chunks []Chunk
	var cur strings.Builder
	hintType := eval.TaskDebugging

	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git"):
			if cur.Len() > 0 {
				chunks = append(chunks, Chunk{Content: cur.String(), HintTaskType: hintType})
				cur.Reset()
			}
			hintType = inferTaskTypeFromPath(line)
		case strings.HasPrefix(line, "@@"):
			if cur.Len() > minChunkLen {
				chunks = append(chunks, Chunk{Content: cur.String(), HintTaskType: hintType})
				cur.Reset()
			}
			cur.WriteString(line + "\n")
		default:
			cur.WriteString(line + "\n")
		}
	}
	if cur.Len() > minChunkLen {
		chunks = append(chunks, Chunk{Content: cur.String(), HintTaskType: hintType})
	}
	return chunks
}

const minChunkLen = 50

// ChunkCILog extracts error context from CI log output.
// Returns one chunk per contiguous error block.
func ChunkCILog(ciLog string) []Chunk {
	if strings.TrimSpace(ciLog) == "" {
		return nil
	}

	var chunks []Chunk
	var cur strings.Builder
	inBlock := false

	for _, line := range strings.Split(ciLog, "\n") {
		isError := isErrorLine(line)
		if isError || inBlock {
			cur.WriteString(line + "\n")
			inBlock = true
			if isBlankOrSeparator(line) && cur.Len() > minChunkLen {
				chunks = append(chunks, Chunk{Content: cur.String(), HintTaskType: eval.TaskDebugging})
				cur.Reset()
				inBlock = false
			}
		}
	}
	if cur.Len() > minChunkLen {
		chunks = append(chunks, Chunk{Content: cur.String(), HintTaskType: eval.TaskDebugging})
	}
	return chunks
}

func inferTaskTypeFromPath(diffLine string) eval.TaskType {
	lower := strings.ToLower(diffLine)
	switch {
	case strings.Contains(lower, "test"):
		return eval.TaskTesting
	case strings.Contains(lower, "arch") || strings.Contains(lower, "design"):
		return eval.TaskArchitecture
	case strings.Contains(lower, "error") || strings.Contains(lower, "fix"):
		return eval.TaskDebugging
	default:
		return eval.TaskImplementation
	}
}

func isErrorLine(line string) bool {
	lower := strings.ToLower(line)
	for _, kw := range []string{"error", "fail", "panic", "fatal", "--- fail", "=== fail"} {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func isBlankOrSeparator(line string) bool {
	trimmed := strings.TrimSpace(line)
	return trimmed == "" || trimmed == "---"
}
