// Package client provides a Go client library for the Berth API server.
//
// Create a client with [New] and configure it with [Option] functions:
//
//	c := client.New("https://berth.example.com:8443",
//	    client.WithAPIKey("my-api-key"),
//	    client.WithTLSConfig(tlsCfg),
//	)
//	err := c.Ping(ctx)
//
// # Options
//
//   - [WithAPIKey] sets the bearer token for API authentication.
//   - [WithHTTPClient] replaces the default [net/http.Client].
//   - [WithTLSConfig] configures custom TLS settings on the underlying
//     transport.
package client
