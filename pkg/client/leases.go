package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// AcquireResult mirrors the API's lease response. When Acquired is false,
// Holder/FencingToken/ExpiresAt describe the entity currently holding the
// lease.
type AcquireResult struct {
	Acquired     bool      `json:"acquired"`
	Holder       string    `json:"holder,omitempty"`
	FencingToken int32    `json:"fencingToken,omitempty"`
	ExpiresAt    time.Time `json:"expiresAt,omitempty"`
	AcquiredAt   time.Time `json:"acquiredAt,omitempty"`
}

// ErrConflict is returned by [Client.Release] when the server reports the
// lease is held by a different identity or fencing token.
var ErrConflict = errors.New("client: lease conflict")

// Acquire requests the lease at namespace/name for holder with the given
// TTL.
func (c *Client) Acquire(ctx context.Context, namespace, name, holder string, ttl time.Duration) (AcquireResult, error) {
	body := map[string]any{
		"holder":     holder,
		"ttlSeconds": int32(ttl / time.Second),
	}
	var out AcquireResult
	if err := c.postJSON(ctx, leasePath(namespace, name, "acquire"), body, &out); err != nil {
		return AcquireResult{}, err
	}
	return out, nil
}

// Renew extends the TTL of a lease at namespace/name held by holder under
// fencingToken.
func (c *Client) Renew(ctx context.Context, namespace, name, holder string, fencingToken int32, ttl time.Duration) (AcquireResult, error) {
	body := map[string]any{
		"holder":       holder,
		"fencingToken": fencingToken,
		"ttlSeconds":   int32(ttl / time.Second),
	}
	var out AcquireResult
	if err := c.postJSON(ctx, leasePath(namespace, name, "renew"), body, &out); err != nil {
		return AcquireResult{}, err
	}
	return out, nil
}

// Release relinquishes the lease at namespace/name held by holder under
// fencingToken. Returns [ErrConflict] if the server rejects the call
// because the lease is held by a different identity or token.
func (c *Client) Release(ctx context.Context, namespace, name, holder string, fencingToken int32) error {
	body := map[string]any{
		"holder":       holder,
		"fencingToken": fencingToken,
	}
	return c.postJSON(ctx, leasePath(namespace, name, "release"), body, nil)
}

func leasePath(namespace, name, op string) string {
	return fmt.Sprintf("/v1alpha1/namespaces/%s/leases/%s/%s",
		url.PathEscape(namespace), url.PathEscape(name), op)
}

func (c *Client) postJSON(ctx context.Context, path string, body, out any) error {
	if c.baseURL == "" {
		return errors.New("client: base URL is required")
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal body: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(buf))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusConflict:
		return ErrConflict
	case resp.StatusCode == http.StatusNoContent:
		return nil
	case resp.StatusCode >= 400:
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server status %d: %s", resp.StatusCode, bytes.TrimSpace(errBody))
	}

	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
