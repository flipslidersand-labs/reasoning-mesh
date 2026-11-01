// Package tasktype defines the single task-category type shared by eval,
// qdrant payloads and the knowledge pipeline (#212). Having one definition
// removes the hand-written conversion that used to silently coerce unknown
// values to "implementation".
package tasktype

// Type is the category of a task or knowledge item.
type Type string

const (
	Debugging      Type = "debugging"
	Implementation Type = "implementation"
	Architecture   Type = "architecture"
	Testing        Type = "testing"
)

// Valid reports whether t is one of the defined task types.
func Valid(t Type) bool {
	switch t {
	case Debugging, Implementation, Architecture, Testing:
		return true
	}
	return false
}
