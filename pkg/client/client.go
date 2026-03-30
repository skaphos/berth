package client

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

// Client communicates with the Berth API server over HTTP. A zero-value
// Client is not usable; create one with [New].
type Client struct {
	baseURL    string
	httpClient *http.Client
	apiKey     string
}

// New creates a Client targeting the given base URL. Configure the client
// with [Option] functions such as [WithAPIKey] and [WithTLSConfig].
func New(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		httpClient: &http.Client{},
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Ping verifies that the client is configured with a non-empty base URL.
func (c *Client) Ping(ctx context.Context) error {
	_ = ctx
	if c.baseURL == "" {
		return errors.New("base URL is required")
	}
	return nil
}
