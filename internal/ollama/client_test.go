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
