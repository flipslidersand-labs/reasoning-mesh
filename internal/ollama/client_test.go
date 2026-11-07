package ollama

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
