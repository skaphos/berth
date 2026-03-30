package client

import (
	"crypto/tls"
	"net/http"
)

// Option configures a [Client].
type Option func(*Client)

// WithAPIKey sets the API key used for bearer token authentication.
func WithAPIKey(key string) Option {
	return func(c *Client) {
		c.apiKey = key
	}
}

// WithHTTPClient replaces the default HTTP client. If httpClient is nil,
// the default is retained.
func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) {
		if httpClient != nil {
			c.httpClient = httpClient
		}
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
