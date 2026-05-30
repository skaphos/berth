package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Client communicates with the Berth API server over HTTP. A zero-value
// Client is not usable; create one with [New].
type Client struct {
	baseURL    string
	httpClient *http.Client

	// apiKeyFunc, when non-nil, is invoked on every request to get the
	// current bearer token. A static-string [WithAPIKey] is implemented
	// as a closure over the string; [WithAPIKeyFunc] takes any getter
	// (typically backed by a file the operator periodically refreshes).
	apiKeyFunc func() string
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

// Ping checks that the Berth API server is reachable by issuing an
// unauthenticated GET against its /healthz liveness route. It returns a
// non-nil error when the base URL is unset, the request cannot be sent, or
// the server answers with a non-2xx status — so an operator readiness probe
// gated on Ping is drained when the central API is unreachable. The caller
// should pass a context with a deadline to bound a hung connection.
func (c *Client) Ping(ctx context.Context) error {
	if c.baseURL == "" {
		return errors.New("client: base URL is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/healthz", nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("berth api healthz: status %d", resp.StatusCode)
	}
	return nil
}
