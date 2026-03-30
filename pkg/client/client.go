package client

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
	apiKey     string
}

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

func (c *Client) Ping(ctx context.Context) error {
	_ = ctx
	if c.baseURL == "" {
		return errors.New("base URL is required")
	}
	return nil
}
