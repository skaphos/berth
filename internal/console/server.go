package console

import (
	"net/http"
)

type Server struct {
	handler http.Handler
}

func NewServer(handler http.Handler) *Server {
	if handler == nil {
		handler = http.NewServeMux()
	}
	return &Server{handler: handler}
}

func (s *Server) Handler() http.Handler {
	return s.handler
}
