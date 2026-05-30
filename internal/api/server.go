package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

const defaultShutdownTimeout = 5 * time.Second

type serverConfig struct {
	address     string
	handler     http.Handler
	tlsCertFile string
	tlsKeyFile  string
}

// Option configures a Server.
type Option func(*serverConfig)

// Server wraps an http.Server for the Berth API server.
type Server struct {
	httpServer  *http.Server
	tlsCertFile string
	tlsKeyFile  string
}

// WithAddress sets the server listen address.
func WithAddress(address string) Option {
	return func(cfg *serverConfig) {
		cfg.address = address
	}
}

// WithHandler sets the server HTTP handler.
func WithHandler(handler http.Handler) Option {
	return func(cfg *serverConfig) {
		cfg.handler = handler
	}
}

// WithTLSFiles sets the server TLS certificate and key file paths.
func WithTLSFiles(certFile, keyFile string) Option {
	return func(cfg *serverConfig) {
		cfg.tlsCertFile = certFile
		cfg.tlsKeyFile = keyFile
	}
}

// NewServer constructs a Server with the supplied options.
func NewServer(opts ...Option) *Server {
	cfg := serverConfig{
		address: ":8443",
		handler: NewMux(nil, nil, nil),
	}

	for _, opt := range opts {
		opt(&cfg)
	}

	return &Server{
		httpServer: &http.Server{
			Addr:              cfg.address,
			Handler:           cfg.handler,
			ReadHeaderTimeout: 5 * time.Second,
		},
		tlsCertFile: cfg.tlsCertFile,
		tlsKeyFile:  cfg.tlsKeyFile,
	}
}

// Start runs the HTTP server until it exits or the context is canceled.
func (s *Server) Start(ctx context.Context) error {
	if ctx == nil {
		return errors.New("start server: nil context")
	}

	if err := s.validateTLS(); err != nil {
		return err
	}

	serveErrCh := make(chan error, 1)
	go func() {
		serveErrCh <- s.serve()
	}()

	select {
	case err := <-serveErrCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), defaultShutdownTimeout)
		defer cancel()

		if err := s.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown server: %w", err)
		}

		return <-serveErrCh
	}
}

// Shutdown gracefully stops the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) serve() error {
	return s.httpServer.ListenAndServeTLS(s.tlsCertFile, s.tlsKeyFile)
}

func (s *Server) validateTLS() error {
	if s.tlsCertFile == "" || s.tlsKeyFile == "" {
		return errors.New("tls cert and key files are required")
	}
	return nil
}
