package client

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewAppliesOptionsAndTrimsBaseURL(t *testing.T) {
	t.Parallel()

	httpClient := &http.Client{}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS13}

	client := New("https://example.com/", WithAPIKey("token"), WithHTTPClient(httpClient), WithTLSConfig(tlsConfig))

	if client.baseURL != "https://example.com" {
		t.Fatalf("baseURL = %q, want %q", client.baseURL, "https://example.com")
	}
	if client.apiKeyFunc == nil {
		t.Fatal("apiKey getter was not configured")
	}
	if got := client.apiKeyFunc(); got != "token" {
		t.Fatalf("apiKey getter returned %q, want %q", got, "token")
	}
	if client.httpClient != httpClient {
		t.Fatal("http client was not preserved")
	}

	transport, ok := client.httpClient.Transport.(*http.Transport)
	if !ok || transport == nil {
		t.Fatal("expected http transport to be configured")
	}
	if transport.TLSClientConfig != tlsConfig {
		t.Fatal("expected tls config to be configured on transport")
	}
}

func TestWithHTTPClientNilLeavesDefaultClient(t *testing.T) {
	t.Parallel()

	client := New("https://example.com", WithHTTPClient(nil), WithTLSConfig(nil))
	if client.httpClient == nil {
		t.Fatal("expected default http client")
	}
}

func TestNewAppliesDefaultTimeout(t *testing.T) {
	t.Parallel()

	client := New("https://example.com")
	if client.httpClient.Timeout != DefaultTimeout {
		t.Fatalf("timeout = %s, want the %s default", client.httpClient.Timeout, DefaultTimeout)
	}
}

func TestWithTimeoutIsOrderIndependentAndBeatsSuppliedClient(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts []Option
		want time.Duration
	}{
		{"before other options", []Option{WithTimeout(3 * time.Second), WithAPIKey("k")}, 3 * time.Second},
		{"after other options", []Option{WithAPIKey("k"), WithTimeout(3 * time.Second)}, 3 * time.Second},
		{
			"overrides a supplied client",
			[]Option{WithHTTPClient(&http.Client{Timeout: time.Minute}), WithTimeout(3 * time.Second)},
			3 * time.Second,
		},
		{
			"supplied client before the option still loses",
			[]Option{WithTimeout(3 * time.Second), WithHTTPClient(&http.Client{Timeout: time.Minute})},
			3 * time.Second,
		},
		{"zero disables the bound", []Option{WithTimeout(0)}, 0},
		{"negative is clamped to disabled", []Option{WithTimeout(-time.Second)}, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := New("https://example.com", tc.opts...).httpClient.Timeout; got != tc.want {
				t.Fatalf("timeout = %s, want %s", got, tc.want)
			}
		})
	}
}

// A supplied client with no WithTimeout keeps its own semantics — we do
// not silently impose a bound on a client the caller configured.
func TestWithHTTPClientKeepsItsOwnTimeout(t *testing.T) {
	t.Parallel()

	supplied := &http.Client{Timeout: 42 * time.Second}
	if got := New("https://example.com", WithHTTPClient(supplied)).httpClient.Timeout; got != 42*time.Second {
		t.Fatalf("timeout = %s, want the supplied 42s", got)
	}
}

// The bound must actually fire against a server that accepts the
// connection and then stalls — the failure mode behind issue #97.
func TestRequestAgainstStalledServerFailsFast(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		<-release
	}))
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})

	c := New(srv.URL, WithTimeout(100*time.Millisecond))

	done := make(chan error, 1)
	go func() { done <- c.Ping(context.Background()) }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected a timeout error from the stalled server")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Ping did not return: the request was not bounded by the client timeout")
	}
}

func TestPingRequiresBaseURL(t *testing.T) {
	t.Parallel()

	// Ping short-circuits before issuing any request when no base URL is
	// configured. Reachability behaviour against a live server is covered by
	// the tests in ping_test.go.
	empty := &Client{}
	if err := empty.Ping(context.Background()); err == nil {
		t.Fatal("expected error for empty baseURL")
	}
}
