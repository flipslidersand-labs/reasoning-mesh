package validate_test

import (
	"testing"

	"github.com/flipslidersand/reasoning-mesh/internal/validate"
)

func TestTaskType(t *testing.T) {
	valid := []string{"debugging", "implementation", "architecture", "testing"}
	for _, v := range valid {
		if err := validate.TaskType(v); err != nil {
			t.Errorf("TaskType(%q) unexpected error: %v", v, err)
		}
	}
	invalid := []string{"", "unknown", "DEBUGGING", "hack; DROP TABLE"}
	for _, v := range invalid {
		if err := validate.TaskType(v); err == nil {
			t.Errorf("TaskType(%q) want error, got nil", v)
		}
	}
}

func TestCommitSHA(t *testing.T) {
	valid := []string{
		"abc1234",
		"abc1234def567890abc1234def567890abc1234def567890abc1234def567890",
		"0000000",
	}
	for _, v := range valid {
		if err := validate.CommitSHA(v); err != nil {
			t.Errorf("CommitSHA(%q) unexpected error: %v", v, err)
		}
	}
	invalid := []string{
		"",
		"abc123",    // 6 chars, too short
		"ABC1234",   // uppercase
		"abc1234\n", // control char
		"../../etc", // path traversal attempt
	}
	for _, v := range invalid {
		if err := validate.CommitSHA(v); err == nil {
			t.Errorf("CommitSHA(%q) want error, got nil", v)
		}
	}
}

func TestKnowledgeID(t *testing.T) {
	valid := []string{
		"550e8400-e29b-41d4-a716-446655440000",
		"6ba7b810-9dad-41d1-80b4-00c04fd430c8",
		"550e8400-e29b-31d4-a716-446655440000", // non-v4 UUID (sha256-derived IDs carry no version bits)
		"550E8400E29B41D4A716446655440000",     // hyphen-less, upper case
		"0123456789abcdef0123456789abcdef",     // deterministicID form
	}
	for _, v := range valid {
		if err := validate.KnowledgeID(v); err != nil {
			t.Errorf("KnowledgeID(%q) unexpected error: %v", v, err)
		}
	}
	invalid := []string{
		"",
		"id-001",
		"not-a-uuid",
		"550e8400e29b-41d4-a716-446655440000", // partial hyphens
		"0123456789abcdef0123456789abcdeg",    // non-hex
		"0123456789abcdef0123456789abcde",     // 31 chars
	}
	for _, v := range invalid {
		if err := validate.KnowledgeID(v); err == nil {
			t.Errorf("KnowledgeID(%q) want error, got nil", v)
		}
	}
}

func TestKnowledgeIDs(t *testing.T) {
	// all valid
	if err := validate.KnowledgeIDs([]string{
		"550e8400-e29b-41d4-a716-446655440000",
		"6ba7b810-9dad-41d1-80b4-00c04fd430c8",
	}); err != nil {
		t.Errorf("KnowledgeIDs all valid: unexpected error: %v", err)
	}
	// one invalid
	if err := validate.KnowledgeIDs([]string{
		"550e8400-e29b-41d4-a716-446655440000",
		"bad-id",
	}); err == nil {
		t.Error("KnowledgeIDs with bad-id: want error, got nil")
	}
}

func TestCollectionName(t *testing.T) {
	valid := []string{"knowledge", "my_col", "col-1", "A", "abcABC123_-"}
	for _, v := range valid {
		if err := validate.CollectionName(v); err != nil {
			t.Errorf("CollectionName(%q) unexpected error: %v", v, err)
		}
	}
	invalid := []string{
		"",
		"../../admin",
		"col/name",
		"col name",
		"a very long collection name that exceeds sixty four characters!!!!",
	}
	for _, v := range invalid {
		if err := validate.CollectionName(v); err == nil {
			t.Errorf("CollectionName(%q) want error, got nil", v)
		}
	}
}

func TestNormalizeKnowledgeID(t *testing.T) {
	const canon = "01234567-89ab-cdef-0123-456789abcdef"
	for _, in := range []string{canon, "0123456789abcdef0123456789abcdef", "0123456789ABCDEF0123456789ABCDEF", "01234567-89AB-CDEF-0123-456789ABCDEF"} {
		if got := validate.NormalizeKnowledgeID(in); got != canon {
			t.Errorf("NormalizeKnowledgeID(%q) = %q, want %q", in, got, canon)
		}
	}
	if got := validate.NormalizeKnowledgeID("bad"); got != "bad" {
		t.Errorf("invalid input must be returned unchanged, got %q", got)
	}
}
