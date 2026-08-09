package client

import (
	"crypto/tls"
	"net/http"
	"time"
)

// Option configures a [Client].
type Option func(*Client)

// WithAPIKey sets a static API key used for bearer token authentication.
// To source the key from a refreshing token file (e.g., an OIDC sidecar
// broker), use [WithAPIKeyFunc] instead.
func WithAPIKey(key string) Option {
	return func(c *Client) {
		c.apiKeyFunc = func() string { return key }
	}
}

// WithAPIKeyFunc sets a getter the client invokes on every request to
// retrieve the current bearer token. Use this when the token is
// short-lived and refreshed externally (e.g., an OIDC token broker
// sidecar that writes a JWT to a shared file). A nil getter, or a
// getter that returns an empty string, results in no Authorization
// header being sent.
func WithAPIKeyFunc(getter func() string) Option {
	return func(c *Client) {
		c.apiKeyFunc = getter
	}
}

// WithHTTPClient replaces the default HTTP client. If httpClient is nil,
// the default is retained.
//
// A supplied client's own Timeout is left alone unless [WithTimeout] is
// also given, in which case the explicit value wins.
func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) {
		if httpClient != nil {
			c.httpClient = httpClient
		}
	}
}

// WithTimeout bounds every request, overriding [DefaultTimeout]. It is
// applied after all other options, so it takes effect regardless of
// option order and also applies to a [WithHTTPClient] client.
//
// A zero or negative timeout disables the bound (net/http semantics),
// leaving requests limited only by the caller's context. Callers that do
// this are responsible for passing a context with a deadline; an
// unbounded lease call can stall a renew loop indefinitely.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		if d < 0 {
			d = 0
		}
		c.timeout = &d
	}
}

// WithTLSConfig sets a custom TLS configuration on the client's HTTP
// transport. If cfg is nil, this option is a no-op.
func WithTLSConfig(cfg *tls.Config) Option {
	return func(c *Client) {
		if cfg == nil {
			return
		}

		transport, ok := c.httpClient.Transport.(*http.Transport)
		if !ok || transport == nil {
			transport = http.DefaultTransport.(*http.Transport).Clone()
		}
		transport.TLSClientConfig = cfg
		c.httpClient.Transport = transport
	}
}
