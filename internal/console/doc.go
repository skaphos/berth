// Package console provides the web console server for Berth.
//
// [Server] wraps an [net/http.Handler] to serve the Berth console UI.
// The handler is provided at construction time via [NewServer], defaulting
// to an empty [net/http.ServeMux] when nil.
package console
