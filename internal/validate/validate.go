// Package validate provides input-validation helpers shared across HTTP handlers.
package validate

import (
	"fmt"
	"regexp"
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

// uuidPattern matches UUID v4 (case-insensitive).
var uuidPattern = regexp.MustCompile(
	`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`,
)

// KnowledgeID validates that id is a UUID v4.
func KnowledgeID(id string) error {
	if !uuidPattern.MatchString(id) {
		return fmt.Errorf("invalid knowledge_id %q: must be a UUID v4", id)
	}
	return nil
}

// KnowledgeIDs validates every ID in the slice.
func KnowledgeIDs(ids []string) error {
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
