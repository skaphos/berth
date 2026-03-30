package client

import (
	"crypto/tls"
	"net/http"
)

type Option func(*Client)

func WithAPIKey(key string) Option {
	return func(c *Client) {
		c.apiKey = key
	}
}

func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) {
		if httpClient != nil {
			c.httpClient = httpClient
		}
	}
}

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
