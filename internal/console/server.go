package console

import (
	"net/http"
)

// Server serves the Berth web console UI.
type Server struct {
	handler http.Handler
}

// NewServer creates a console Server with the given handler. If handler is
// nil, an empty [net/http.ServeMux] is used.
func NewServer(handler http.Handler) *Server {
	if handler == nil {
		handler = http.NewServeMux()
	}
	return &Server{handler: handler}
}

// Handler returns the HTTP handler serving the console UI.
func (s *Server) Handler() http.Handler {
	return s.handler
}
