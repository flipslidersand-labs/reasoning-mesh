package bearer_test

import (
	"testing"

	"github.com/flipslidersand/reasoning-mesh/internal/bearer"
)

func TestMatches(t *testing.T) {
	const want = "dummy-token-value"
	cases := []struct {
		name string
		auth string
		want bool
	}{
		{"correct", "Bearer dummy-token-value", true},
		{"wrong same length", "Bearer dummy-token-valuf", false},
		{"empty bearer", "Bearer ", false},
		{"no header", "", false},
		{"no scheme", "dummy-token-value", false},
		{"lowercase scheme", "bearer dummy-token-value", false},
		{"shorter prefix of token", "Bearer dummy-token", false},
		{"longer with token as prefix", "Bearer dummy-token-value-extra", false},
		{"trailing space", "Bearer dummy-token-value ", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := bearer.Matches(c.auth, want); got != c.want {
				t.Errorf("Matches(%q) = %v, want %v", c.auth, got, c.want)
			}
		})
	}
}
