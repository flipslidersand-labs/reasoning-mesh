package knowledge

import (
	"strings"

	"github.com/flipslidersand-labs/reasoning-mesh/internal/eval"
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

	// flush emits the buffer as a chunk only if it exceeds minChunkLen.
	flush := func() {
		if cur.Len() > minChunkLen {
			chunks = append(chunks, Chunk{Content: cur.String(), HintTaskType: hintType})
		}
	}

	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git"):
			// Small chunks are dropped, not merged into the next file.
			flush()
			cur.Reset()
			hintType = inferTaskTypeFromPath(line)
		case strings.HasPrefix(line, "@@"):
			// Small hunks are kept in the buffer and merged with the next one.
			if cur.Len() > minChunkLen {
				flush()
				cur.Reset()
			}
			cur.WriteString(line + "\n")
		default:
			cur.WriteString(line + "\n")
		}
	}
	flush()
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
	case isTestPath(diffPath(diffLine)):
		return eval.TaskTesting
	case strings.Contains(lower, "arch") || strings.Contains(lower, "design"):
		return eval.TaskArchitecture
	case strings.Contains(lower, "error") || strings.Contains(lower, "fix"):
		return eval.TaskDebugging
	default:
		return eval.TaskImplementation
	}
}

// diffPath extracts the path from a "diff --git" line (the second/b side).
// It handles the "a/X b/Y" form and the --no-prefix form ("X Y", where both
// tokens are identical or one lacks the a/ b/ prefix). If nothing can be
// extracted the whole line is returned.
func diffPath(diffLine string) string {
	line := strings.TrimRight(diffLine, "\r")
	rest := strings.TrimPrefix(line, "diff --git ")
	// Identical halves ("X X"): handles no-prefix paths containing spaces.
	if n := len(rest); n%2 == 1 && rest[n/2] == ' ' && rest[:n/2] == rest[n/2+1:] {
		return rest[n/2+1:]
	}
	if i := strings.LastIndex(line, " b/"); i >= 0 {
		return line[i+3:]
	}
	if rest != line {
		if f := strings.Fields(rest); len(f) >= 2 {
			return strings.Trim(f[len(f)-1], "\"")
		}
	}
	return line
}

// isTestPath reports whether path looks like a test file on a word boundary:
// a "test"/"tests"/"testdata" directory, a file stem of "test"/"tests", a
// "_test" suffix, a "test_" prefix, a ".test."/".spec." token in the file name
// (foo.test.ts), or a CamelCase "...Test" stem (FooTest.java). "latest",
// "contest" and "Contest.java" do not match.
func isTestPath(path string) bool {
	parts := strings.Split(path, "/")
	for _, d := range parts[:len(parts)-1] {
		d = strings.ToLower(d)
		if d == "test" || d == "tests" || d == "testdata" {
			return true
		}
	}
	orig := parts[len(parts)-1]
	name := strings.ToLower(orig)
	if strings.Contains(name, ".test.") || strings.Contains(name, ".spec.") {
		return true
	}
	if i := strings.Index(orig, "."); i >= 0 {
		orig = orig[:i]
	}
	if len(orig) > 4 && strings.HasSuffix(orig, "Test") {
		if c := orig[len(orig)-5]; (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			return true
		}
	}
	if i := strings.Index(name, "."); i >= 0 {
		name = name[:i]
	}
	return name == "test" || name == "tests" ||
		strings.HasSuffix(name, "_test") || strings.HasPrefix(name, "test_")
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
