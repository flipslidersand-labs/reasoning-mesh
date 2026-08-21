package qdrant

import "time"

// TaskType represents the category of a knowledge item.
type TaskType string

const (
	TaskImplementation TaskType = "implementation"
	TaskArchitecture   TaskType = "architecture"
	TaskDebugging      TaskType = "debugging"
	TaskErrors         TaskType = "errors"
	TaskTesting        TaskType = "testing"
	TaskDecisions      TaskType = "decisions"
)

// KnowledgePayload is the Qdrant payload for the knowledge collection.
type KnowledgePayload struct {
	Content      string    `json:"content"`
	TaskType     TaskType  `json:"task_type"`
	Language     string    `json:"language"`
	Framework    string    `json:"framework"`
	Source       string    `json:"source"` // claude | codex | manual
	UsageCount   int       `json:"usage_count"`
	SuccessCount int       `json:"success_count"`
	SuccessRate  float64   `json:"success_rate"` // cached, recomputed by scorer
	CreatedAt    time.Time `json:"created_at"`
	LastUsedAt   time.Time `json:"last_used_at"`
	Tags         []string  `json:"tags"`
}

// OutcomeStatus represents the result of applying a knowledge item.
type OutcomeStatus string

const (
	OutcomeSuccess OutcomeStatus = "success"
	OutcomeFailure OutcomeStatus = "failure"
	OutcomePartial OutcomeStatus = "partial"
)

// FailureType categorises why an experience failed.
type FailureType string

const (
	FailureCompile FailureType = "compile"
	FailureTest    FailureType = "test"
	FailureRuntime FailureType = "runtime"
	FailureReview  FailureType = "review"
	FailureUnknown FailureType = "unknown"
)

// ExperiencePayload is the Qdrant payload for the experience collection.
// Common fields are defined here; Failure Memory operational logic is in internal/knowledge.
type ExperiencePayload struct {
	Content      string        `json:"content"`
	TaskType     TaskType      `json:"task_type"`
	OutcomeStatus OutcomeStatus `json:"outcome_status"`
	// Failure Memory fields (populated by internal/knowledge/experience.go)
	FailureType          FailureType `json:"failure_type,omitempty"`
	Evaluator            string      `json:"evaluator,omitempty"` // ci | human | llm
	Confidence           float64     `json:"confidence,omitempty"`
	RelatedKnowledgeIDs  []string    `json:"related_knowledge_ids,omitempty"`
	CreatedAt            time.Time   `json:"created_at"`
}
