package client

import (
	"context"
	"crypto/tls"
	"net/http"
	"testing"
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

func TestPingRequiresBaseURL(t *testing.T) {
	t.Parallel()

	empty := &Client{}
	if err := empty.Ping(context.Background()); err == nil {
		t.Fatal("expected error for empty baseURL")
	}

	client := New("https://example.com")
	if err := client.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
}
