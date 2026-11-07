package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func serve(resp GenerateResponse) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestGenerateFull_OK(t *testing.T) {
	srv := serve(GenerateResponse{Response: "hello", Done: true})
	defer srv.Close()

	c := New(srv.URL, 10)
	got, err := c.GenerateFull(context.Background(), "m", "p")
	if err != nil {
		t.Fatal(err)
	}
	if got.Response != "hello" {
		t.Errorf("want hello, got %q", got.Response)
	}
}

func TestGenerateFull_EmptyResponseWithDoneTrue(t *testing.T) {
	srv := serve(GenerateResponse{Response: "", Done: true})
	defer srv.Close()

	c := New(srv.URL, 10)
	_, err := c.GenerateFull(context.Background(), "m", "p")
	if err == nil {
		t.Fatal("want error for empty response with Done=true, got nil")
	}
}

func TestGenerateFull_EmptyResponseWithDoneFalse(t *testing.T) {
	srv := serve(GenerateResponse{Response: "", Done: false})
	defer srv.Close()

	c := New(srv.URL, 10)
	_, err := c.GenerateFull(context.Background(), "m", "p")
	if err == nil {
		t.Fatal("want error for empty response with Done=false, got nil")
	}
}

func TestIsTransient(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{"connection refused", true},
		{"connection reset by peer", true},
		{"EOF", true},
		{"read tcp ...", true},
		{"ollama generate: empty response (model may be thermal-throttling)", true},
		{"status 400", false},
		{"invalid JSON", false},
	}
	for _, tc := range cases {
		got := isTransient(fmt.Errorf("%s", tc.msg))
		if got != tc.want {
			t.Errorf("isTransient(%q) = %v, want %v", tc.msg, got, tc.want)
		}
	}
}

// captureNumPredict returns a server that records options.num_predict from
// the request and replies with the given response.
func captureNumPredict(t *testing.T, got *int, resp GenerateResponse) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req GenerateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if req.Options != nil {
			*got = req.Options.NumPredict
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestGenerateFull_LengthDoneReasonIsExposed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"m","response":"{\"task_type\": \"impl","done":true,"done_reason":"length","eval_count":600}`))
	}))
	defer srv.Close()

	got, err := New(srv.URL, 10).GenerateFull(context.Background(), "m", "p")
	if err != nil {
		t.Fatal(err)
	}
	if got.DoneReason != "length" {
		t.Errorf("DoneReason = %q, want length", got.DoneReason)
	}
	if !got.Truncated() {
		t.Error("Truncated() = false, want true for done_reason=length")
	}
}

func TestGenerateFull_StopIsNotTruncated(t *testing.T) {
	srv := serve(GenerateResponse{Response: "ok", Done: true, DoneReason: "stop"})
	defer srv.Close()

	got, err := New(srv.URL, 10).GenerateFull(context.Background(), "m", "p")
	if err != nil {
		t.Fatal(err)
	}
	if got.Truncated() {
		t.Error("Truncated() = true, want false for done_reason=stop")
	}
}

func TestGenerateFull_NumPredictDefaultAndOverride(t *testing.T) {
	tests := []struct {
		name string
		set  int
		want int
	}{
		{"default", 0, DefaultNumPredict},
		{"override", 4096, 4096},
		{"non-positive keeps default", -1, DefaultNumPredict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got int
			srv := captureNumPredict(t, &got, GenerateResponse{Response: "ok", Done: true})
			defer srv.Close()

			c := New(srv.URL, 10)
			if tt.set != 0 {
				c = c.WithNumPredict(tt.set)
			}
			if _, err := c.GenerateFull(context.Background(), "m", "p"); err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("num_predict = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestChat_OK_SendsRequestBody(t *testing.T) {
	var gotPath, gotMethod, gotCT string
	var gotReq ChatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod, gotCT = r.URL.Path, r.Method, r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotReq)
		_ = json.NewEncoder(w).Encode(ChatResponse{Message: ChatMessage{Role: "assistant", Content: "hello"}, Done: true})
	}))
	defer srv.Close()

	msgs := []ChatMessage{{Role: "system", Content: "sys"}, {Role: "user", Content: "hi"}}
	got, err := New(srv.URL, 5).Chat(context.Background(), "m1", msgs)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got != "hello" {
		t.Errorf("content = %q, want hello", got)
	}
	if gotPath != "/api/chat" || gotMethod != http.MethodPost || gotCT != "application/json" {
		t.Errorf("request = %s %s ct=%q", gotMethod, gotPath, gotCT)
	}
	if gotReq.Model != "m1" || gotReq.Stream {
		t.Errorf("model=%q stream=%v, want m1/false", gotReq.Model, gotReq.Stream)
	}
	if len(gotReq.Messages) != 2 || gotReq.Messages[0] != msgs[0] || gotReq.Messages[1] != msgs[1] {
		t.Errorf("messages = %+v, want %+v", gotReq.Messages, msgs)
	}
}

func TestChat_StreamFalseOnWire(t *testing.T) {
	var raw map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&raw)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	if _, err := New(srv.URL, 5).Chat(context.Background(), "m", nil); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	v, ok := raw["stream"]
	if !ok || v != false {
		t.Errorf("stream = %v (present=%v), want explicit false", v, ok)
	}
}

func TestChat_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	got, err := New(srv.URL, 5).Chat(context.Background(), "m", nil)
	if err == nil || err.Error() != "ollama chat: status 500" {
		t.Fatalf("err = %v, want 'ollama chat: status 500'", err)
	}
	if got != "" {
		t.Errorf("content = %q, want empty", got)
	}
}

func TestChat_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{not json`))
	}))
	defer srv.Close()
	got, err := New(srv.URL, 5).Chat(context.Background(), "m", nil)
	if err == nil {
		t.Fatal("expected decode error")
	}
	if got != "" {
		t.Errorf("content = %q, want empty", got)
	}
}

func TestChat_ServerDown(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	if _, err := New(url, 5).Chat(context.Background(), "m", nil); err == nil {
		t.Fatal("expected connection error")
	}
}

func TestChat_ContextCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := New(srv.URL, 5).Chat(ctx, "m", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestPing_OK(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		_, _ = w.Write([]byte(`{"models":[]}`))
	}))
	defer srv.Close()
	if err := New(srv.URL, 5).Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if gotPath != "/api/tags" || gotMethod != http.MethodGet {
		t.Errorf("request = %s %s, want GET /api/tags", gotMethod, gotPath)
	}
}

func TestPing_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	err := New(srv.URL, 5).Ping(context.Background())
	if err == nil || err.Error() != "ollama ping: status 503" {
		t.Fatalf("err = %v, want 'ollama ping: status 503'", err)
	}
}

func TestPing_ServerDown(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	if err := New(url, 5).Ping(context.Background()); err == nil {
		t.Fatal("expected connection error")
	}
}

func TestPing_ContextCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := New(srv.URL, 5).Ping(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestPing_InvalidEndpoint(t *testing.T) {
	// A control character makes http.NewRequestWithContext fail.
	if err := New("http://bad\x7fhost", 5).Ping(context.Background()); err == nil {
		t.Fatal("expected request construction error")
	}
}

func TestGenerate_ReturnsResponseString(t *testing.T) {
	srv := serve(GenerateResponse{Response: "answer", Done: true})
	defer srv.Close()
	got, err := New(srv.URL, 5).Generate(context.Background(), "m", "p")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if got != "answer" {
		t.Errorf("got %q, want answer", got)
	}
}

func TestGenerate_Non200PropagatesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()
	got, err := New(srv.URL, 5).Generate(context.Background(), "m", "p")
	if err == nil || err.Error() != "ollama generate: status 400" {
		t.Fatalf("err = %v, want 'ollama generate: status 400'", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// An empty response is treated as transient, so GenerateFull retries with
// 5s+10s backoff (~15s). Run in parallel to limit wall-clock impact.
func TestGenerate_EmptyResponsePropagatesError(t *testing.T) {
	t.Parallel()
	srv := serve(GenerateResponse{Response: "", Done: true})
	defer srv.Close()
	got, err := New(srv.URL, 5).Generate(context.Background(), "m", "p")
	if err == nil || !strings.Contains(err.Error(), "empty response") {
		t.Fatalf("err = %v, want empty response error", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
