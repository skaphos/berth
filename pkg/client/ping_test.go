package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPingSucceedsOnHealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			t.Errorf("ping path = %q, want /healthz", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := New(srv.URL).Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestPingFailsOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	if err := New(srv.URL).Ping(context.Background()); err == nil {
		t.Fatal("Ping should fail when the server reports 503")
	}
}

func TestPingFailsWhenUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing is listening now

	if err := New(url).Ping(context.Background()); err == nil {
		t.Fatal("Ping should fail when the server is unreachable")
	}
}

func TestPingFailsWithoutBaseURL(t *testing.T) {
	if err := New("").Ping(context.Background()); err == nil {
		t.Fatal("Ping should fail when no base URL is configured")
	}
}
