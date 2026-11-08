package router

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestClaudeAdapter_TruncatedFlag(t *testing.T) {
	a := newTestClaude(t, 200, `{"model":"m","stop_reason":"max_tokens","content":[{"type":"text","text":"partial"}]}`)
	r, _ := a.Generate(context.Background(), "p")
	if !r.Truncated {
		t.Fatal("want Truncated=true for max_tokens")
	}
	a = newTestClaude(t, 200, `{"model":"m","stop_reason":"end_turn","content":[{"type":"text","text":"ok"}]}`)
	if r, _ := a.Generate(context.Background(), "p"); r.Truncated {
		t.Fatal("want Truncated=false for end_turn")
	}
}

func TestClaudeAdapter_RequestHeadersAndBody(t *testing.T) {
	var gotKey, gotVer, gotCT, gotMethod, gotModel, gotPrompt string
	var gotMax int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey, gotVer, gotCT, gotMethod = r.Header.Get("x-api-key"), r.Header.Get("anthropic-version"), r.Header.Get("Content-Type"), r.Method
		var req claudeRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotModel, gotMax = req.Model, req.MaxTokens
		if len(req.Messages) == 1 {
			gotPrompt = req.Messages[0].Role + ":" + req.Messages[0].Content
		}
		_, _ = w.Write([]byte(`{"model":"m","content":[{"type":"text","text":"ok"}]}`))
	}))
	defer srv.Close()
	a := NewClaudeAdapter("dummy-key", "test-model")
	a.url = srv.URL
	if _, err := a.Generate(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	if gotKey != "dummy-key" || gotVer != claudeAPIVersion || gotCT != "application/json" || gotMethod != http.MethodPost {
		t.Fatalf("headers: key=%q ver=%q ct=%q method=%q", gotKey, gotVer, gotCT, gotMethod)
	}
	if gotModel != "test-model" || gotMax != claudeMaxTokens || gotPrompt != "user:hello" {
		t.Fatalf("body: model=%q max=%d prompt=%q", gotModel, gotMax, gotPrompt)
	}
}

func TestClaudeAdapter_MultiBlockSkipsNonText(t *testing.T) {
	a := newTestClaude(t, 200, `{"model":"resp-model","stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":7},"content":[`+
		`{"type":"text","text":"foo"},{"type":"tool_use","text":"IGNORED"},{"type":"thinking","text":"IGNORED"},{"type":"text","text":"bar"}]}`)
	r, err := a.Generate(context.Background(), "p")
	if err != nil {
		t.Fatal(err)
	}
	if r.Text != "foobar" || r.PromptTokens != 10 || r.TotalTokens != 17 || r.Model != "resp-model" || r.Truncated {
		t.Fatalf("got %+v", r)
	}
}

func TestClaudeAdapter_OnlyNonTextBlocksIsError(t *testing.T) {
	a := newTestClaude(t, 200, `{"model":"m","stop_reason":"tool_use","content":[{"type":"tool_use","text":"x"}]}`)
	_, err := a.Generate(context.Background(), "p")
	if err == nil || !strings.Contains(err.Error(), "tool_use") {
		t.Fatalf("got %v", err)
	}
}

func TestClaudeAdapter_DecodeError(t *testing.T) {
	a := newTestClaude(t, 200, `{not json`)
	_, err := a.Generate(context.Background(), "p")
	if err == nil || !strings.Contains(err.Error(), "claude decode") {
		t.Fatalf("got %v", err)
	}
}

func TestClaudeAdapter_TransportError(t *testing.T) {
	a := newTestClaude(t, 200, `{}`)
	a.url = "http://127.0.0.1:1" // closed port
	_, err := a.Generate(context.Background(), "p")
	if err == nil || !strings.Contains(err.Error(), "claude http") {
		t.Fatalf("got %v", err)
	}
}
