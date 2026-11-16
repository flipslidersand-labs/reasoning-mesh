package validate

import (
	"os"
	"regexp"
	"testing"
)

// TestIngestDocsScriptTaskTypes guards scripts/ingest-docs.py: every task_type
// it passes to `llmo ingest-local` must be accepted by TaskType, otherwise the
// CLI rejects the chunk and the ingest silently fails.
func TestIngestDocsScriptTaskTypes(t *testing.T) {
	data, err := os.ReadFile("../../scripts/ingest-docs.py")
	if err != nil {
		t.Fatal(err)
	}
	matches := regexp.MustCompile(`"task_type":\s*"([^"]*)"`).FindAllStringSubmatch(string(data), -1)
	if len(matches) == 0 {
		t.Fatal("no task_type entries found in scripts/ingest-docs.py")
	}
	for _, m := range matches {
		if err := TaskType(m[1]); err != nil {
			t.Errorf("scripts/ingest-docs.py: %v", err)
		}
	}
}
