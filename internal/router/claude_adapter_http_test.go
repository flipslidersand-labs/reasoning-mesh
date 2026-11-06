package router

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestClaude(t *testing.T, status int, body string) *ClaudeAdapter {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	a := NewClaudeAdapter("secret-key", "")
	a.url = srv.URL
	return a
}

func TestClaudeAdapter_OK(t *testing.T) {
	a := newTestClaude(t, 200, `{"model":"m","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":2},"content":[{"type":"text","text":"hi"}]}`)
	r, err := a.Generate(context.Background(), "p")
	if err != nil || r.Text != "hi" || r.TotalTokens != 3 {
		t.Fatalf("got %+v, %v", r, err)
	}
}

func TestClaudeAdapter_EmptyContentIsError(t *testing.T) {
	a := newTestClaude(t, 200, `{"model":"m","stop_reason":"refusal","content":[]}`)
	if _, err := a.Generate(context.Background(), "p"); err == nil {
		t.Fatal("want error for empty text")
	}
}

func TestClaudeAdapter_NonOKIncludesBody(t *testing.T) {
	a := newTestClaude(t, 429, `{"error":{"type":"rate_limit_error","message":"slow down"}}`)
	_, err := a.Generate(context.Background(), "p")
	if err == nil || !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), "rate_limit_error") {
		t.Fatalf("got %v", err)
	}
	if strings.Contains(err.Error(), "secret-key") {
		t.Fatal("error leaks api key")
	}
}

func TestClaudeAdapter_NonOKBodyTruncated(t *testing.T) {
	a := newTestClaude(t, 529, strings.Repeat("x", 5000))
	_, err := a.Generate(context.Background(), "p")
	if err == nil || len(err.Error()) > 700 {
		t.Fatalf("want truncated error, got len %d", len(err.Error()))
	}
}

func TestClaudeAdapter_MaxTokensWarns(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(log.Writer())
	a := newTestClaude(t, 200, `{"model":"m","stop_reason":"max_tokens","content":[{"type":"text","text":"partial"}]}`)
	r, err := a.Generate(context.Background(), "p")
	if err != nil || r.Text != "partial" {
		t.Fatalf("got %+v, %v", r, err)
	}
	if !strings.Contains(buf.String(), "max_tokens") {
		t.Fatalf("no warning logged: %q", buf.String())
	}
}
