package knowledge

import "testing"

// The HTTP client timeout must be EmbedTimeout: cmd/orch derives the server
// WriteTimeout from it.
func TestNewEmbedderWithAuth_UsesEmbedTimeout(t *testing.T) {
	e := NewEmbedderWithAuth("http://example.invalid", "", "c")
	if e.http.Timeout != EmbedTimeout {
		t.Errorf("http.Timeout = %v, want EmbedTimeout %v", e.http.Timeout, EmbedTimeout)
	}
}
