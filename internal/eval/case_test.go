package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

const minimalCaseYAML = `id: %ID%
task_type: debugging
prompt: why does it fail
context:
  language: go
  framework: gin
  snippet: "x := 1"
expected:
  root_cause: nil deref
  required_keywords: [nil, pointer]
  forbidden_keywords: [segfault]
`

func caseYAML(id string) string {
	return strings.Replace(minimalCaseYAML, "%ID%", id, 1)
}

func TestLoadCases_ParsesFields(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.yaml"), caseYAML("case-a"))

	cases, err := LoadCases(dir)
	if err != nil {
		t.Fatalf("LoadCases: %v", err)
	}
	if len(cases) != 1 {
		t.Fatalf("len = %d, want 1", len(cases))
	}
	c := cases[0]
	if c.ID != "case-a" || c.TaskType != TaskDebugging || c.Prompt != "why does it fail" {
		t.Errorf("unexpected top-level fields: %+v", c)
	}
	if c.Context.Language != "go" || c.Context.Framework != "gin" || c.Context.Snippet != "x := 1" {
		t.Errorf("unexpected context: %+v", c.Context)
	}
	if c.Expected.RootCause != "nil deref" ||
		len(c.Expected.RequiredKeywords) != 2 || c.Expected.RequiredKeywords[0] != "nil" ||
		len(c.Expected.ForbiddenKeywords) != 1 || c.Expected.ForbiddenKeywords[0] != "segfault" {
		t.Errorf("unexpected expected: %+v", c.Expected)
	}
}

func TestLoadCases_SkipsSchemaAndNonYAML(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.yaml"), caseYAML("case-a"))
	// schema.yaml is excluded even though it is valid YAML with an id.
	writeFile(t, filepath.Join(dir, "schema.yaml"), caseYAML("schema"))
	writeFile(t, filepath.Join(dir, "README.txt"), "not a case")
	// A non-YAML file that would fail to parse must be ignored, not error.
	writeFile(t, filepath.Join(dir, "notes.md"), ": : : [")
	// Current behaviour: only the ".yaml" suffix is loaded; ".yml" is ignored.
	// Whether this is intentional needs confirmation (see issue #269).
	writeFile(t, filepath.Join(dir, "b.yml"), caseYAML("case-b-yml"))

	cases, err := LoadCases(dir)
	if err != nil {
		t.Fatalf("LoadCases: %v", err)
	}
	if len(cases) != 1 || cases[0].ID != "case-a" {
		t.Fatalf("cases = %+v, want only case-a", cases)
	}
}

func TestLoadCases_SchemaYAMLInSubdirIsSkipped(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "sub", "schema.yaml"), "id: [unterminated")
	writeFile(t, filepath.Join(dir, "sub", "c.yaml"), caseYAML("case-c"))

	cases, err := LoadCases(dir)
	if err != nil {
		t.Fatalf("LoadCases: %v", err)
	}
	if len(cases) != 1 || cases[0].ID != "case-c" {
		t.Fatalf("cases = %+v, want only case-c", cases)
	}
}

func TestLoadCases_RecursesIntoSubdirs(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.yaml"), caseYAML("case-a"))
	writeFile(t, filepath.Join(dir, "sub", "b.yaml"), caseYAML("case-b"))
	writeFile(t, filepath.Join(dir, "sub", "deep", "c.yaml"), caseYAML("case-c"))
	// A directory whose name ends in .yaml must not be read as a file.
	if err := os.MkdirAll(filepath.Join(dir, "dir.yaml"), 0o755); err != nil {
		t.Fatal(err)
	}

	cases, err := LoadCases(dir)
	if err != nil {
		t.Fatalf("LoadCases: %v", err)
	}
	got := map[string]bool{}
	for _, c := range cases {
		got[c.ID] = true
	}
	if len(cases) != 3 || !got["case-a"] || !got["case-b"] || !got["case-c"] {
		t.Fatalf("cases = %+v, want case-a/b/c", cases)
	}
}

func TestLoadCases_EmptyDir(t *testing.T) {
	cases, err := LoadCases(t.TempDir())
	if err != nil {
		t.Fatalf("LoadCases: %v", err)
	}
	if len(cases) != 0 {
		t.Fatalf("len = %d, want 0", len(cases))
	}
}

func TestLoadCases_ParseError(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.yaml")
	writeFile(t, filepath.Join(dir, "a.yaml"), caseYAML("case-a"))
	writeFile(t, bad, "id: [unterminated\n")

	cases, err := LoadCases(dir)
	if err == nil {
		t.Fatal("expected error for malformed YAML")
	}
	if !strings.Contains(err.Error(), "parse "+bad) {
		t.Errorf("error = %q, want it to contain %q", err, "parse "+bad)
	}
	_ = cases // partial results on error are unspecified
}

func TestLoadCases_MissingDir(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	cases, err := LoadCases(missing)
	if err == nil {
		t.Fatal("expected error for missing directory")
	}
	if !os.IsNotExist(err) {
		t.Errorf("error = %v, want a not-exist error", err)
	}
	if len(cases) != 0 {
		t.Errorf("len = %d, want 0", len(cases))
	}
}

func TestLoadCases_ReadError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read unreadable files")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "locked.yaml")
	writeFile(t, p, caseYAML("locked"))
	if err := os.Chmod(p, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(p, 0o644) })

	_, err := LoadCases(dir)
	if err == nil || !strings.Contains(err.Error(), "read "+p) {
		t.Fatalf("error = %v, want \"read %s\"", err, p)
	}
}
