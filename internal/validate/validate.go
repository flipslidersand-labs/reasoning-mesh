// Package validate provides input-validation helpers shared across HTTP handlers.
package validate

import (
	"fmt"
	"regexp"
	"strings"
)

// validTaskTypes is the allowlist for task_type values.
var validTaskTypes = map[string]struct{}{
	"debugging":      {},
	"implementation": {},
	"architecture":   {},
	"testing":        {},
}

// TaskType checks that t is one of the recognised task types.
// Returns an error suitable for returning as an HTTP 400 body.
func TaskType(t string) error {
	if _, ok := validTaskTypes[t]; !ok {
		return fmt.Errorf("invalid task_type %q: must be one of debugging|implementation|architecture|testing", t)
	}
	return nil
}

// commitSHAPattern accepts 7-64 lowercase hex characters.
var commitSHAPattern = regexp.MustCompile(`^[0-9a-f]{7,64}$`)

// CommitSHA validates that sha is a plausible git commit SHA (hex, 7-64 chars).
func CommitSHA(sha string) error {
	if !commitSHAPattern.MatchString(sha) {
		return fmt.Errorf("invalid commit_sha: must match ^[0-9a-f]{7,64}$")
	}
	return nil
}

// uuidPattern matches any 128-bit ID as a hyphenated UUID or 32 hex digits
// (case-insensitive). Version/variant bits are not checked: knowledge IDs are
// sha256-derived (see knowledge.deterministicID) and carry none.
var uuidPattern = regexp.MustCompile(
	`(?i)^(?:[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}|[0-9a-f]{32})$`,
)

// KnowledgeID validates that id is a UUID (hyphenated) or 32 hex digits.
func KnowledgeID(id string) error {
	if !uuidPattern.MatchString(id) {
		return fmt.Errorf("invalid knowledge_id %q: must be a UUID or 32 hex digits", id)
	}
	return nil
}

// NormalizeKnowledgeID returns id in the lowercase hyphenated form Qdrant uses
// as point ID. Input that fails KnowledgeID is returned unchanged.
func NormalizeKnowledgeID(id string) string {
	if KnowledgeID(id) != nil {
		return id
	}
	h := strings.ToLower(strings.ReplaceAll(id, "-", ""))
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

// MaxKnowledgeIDs is the maximum number of knowledge_ids accepted in a single request.
const MaxKnowledgeIDs = 128

// KnowledgeIDs validates every ID in the slice and enforces MaxKnowledgeIDs.
func KnowledgeIDs(ids []string) error {
	if len(ids) > MaxKnowledgeIDs {
		return fmt.Errorf("too many knowledge_ids: %d exceeds maximum of %d", len(ids), MaxKnowledgeIDs)
	}
	for _, id := range ids {
		if err := KnowledgeID(id); err != nil {
			return err
		}
	}
	return nil
}

// collectionNamePattern accepts alphanumerics, underscore, and hyphen (1-64 chars).
var collectionNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// CollectionName validates that name is safe to embed in a URL path segment.
func CollectionName(name string) error {
	if !collectionNamePattern.MatchString(name) {
		return fmt.Errorf("invalid collection name %q: must match ^[a-zA-Z0-9_-]{1,64}$", name)
	}
	return nil
}
