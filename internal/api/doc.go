// Package api provides the HTTP server and routing for the Berth API server.
//
// The package exposes a TLS-only [Server] that wraps [net/http.Server] with
// graceful shutdown and functional options for configuration. Routes are
// registered via [NewMux], which returns an [net/http.ServeMux] with the
// current API endpoints.
//
// # Server Lifecycle
//
// Create a server with [NewServer], configure it with [Option] functions,
// and run it with [Server.Start]:
//
//	srv := api.NewServer(
//	    api.WithAddress(":8443"),
//	    api.WithTLSFiles("/path/to/cert.pem", "/path/to/key.pem"),
//	    api.WithHandler(api.NewMux()),
//	)
//	err := srv.Start(ctx)
//
// The server blocks until the context is canceled, then drains in-flight
// requests before returning.
//
// # Middleware
//
// [ChainMiddleware] composes multiple HTTP middleware functions in
// left-to-right order around a base handler.
package api
