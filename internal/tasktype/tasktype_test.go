package tasktype

import "testing"

func TestValid(t *testing.T) {
	for _, ok := range []Type{Debugging, Implementation, Architecture, Testing} {
		if !Valid(ok) {
			t.Errorf("Valid(%q) = false, want true", ok)
		}
	}
	for _, bad := range []Type{"", "errors", "decisions", "Debugging", "foo"} {
		if Valid(bad) {
			t.Errorf("Valid(%q) = true, want false", bad)
		}
	}
}
