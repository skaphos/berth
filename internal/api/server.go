package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

const defaultShutdownTimeout = 5 * time.Second

// Default request timeouts on the API server's TLS listener. Lease RPCs are
// small, fast JSON exchanges, so these bounds are generous enough never to
// truncate a legitimate call yet close slow-body (slowloris) and idle
// keep-alive resource leaks. Without ReadTimeout/WriteTimeout a client that
// dribbles a request body, or never reads the response, pins a connection
// indefinitely; without IdleTimeout, idle keep-alives accumulate unbounded.
const (
	defaultReadHeaderTimeout = 5 * time.Second
	defaultReadTimeout       = 15 * time.Second
	defaultWriteTimeout      = 15 * time.Second
	defaultIdleTimeout       = 120 * time.Second
)

type serverConfig struct {
	address           string
	handler           http.Handler
	tlsCertFile       string
	tlsKeyFile        string
	readHeaderTimeout time.Duration
	readTimeout       time.Duration
	writeTimeout      time.Duration
	idleTimeout       time.Duration
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

// WithTimeouts overrides the default HTTP server request timeouts. A
// non-positive value for any argument leaves the corresponding default in
// place, so callers can tune a single bound without restating the rest.
func WithTimeouts(readHeader, read, write, idle time.Duration) Option {
	return func(cfg *serverConfig) {
		if readHeader > 0 {
			cfg.readHeaderTimeout = readHeader
		}
		if read > 0 {
			cfg.readTimeout = read
		}
		if write > 0 {
			cfg.writeTimeout = write
		}
		if idle > 0 {
			cfg.idleTimeout = idle
		}
	}
}

// NewServer constructs a Server with the supplied options.
func NewServer(opts ...Option) *Server {
	cfg := serverConfig{
		address:           ":8443",
		handler:           NewMux(nil, nil, nil),
		readHeaderTimeout: defaultReadHeaderTimeout,
		readTimeout:       defaultReadTimeout,
		writeTimeout:      defaultWriteTimeout,
		idleTimeout:       defaultIdleTimeout,
	}

	for _, opt := range opts {
		opt(&cfg)
	}

	return &Server{
		httpServer: &http.Server{
			Addr:              cfg.address,
			Handler:           cfg.handler,
			ReadHeaderTimeout: cfg.readHeaderTimeout,
			ReadTimeout:       cfg.readTimeout,
			WriteTimeout:      cfg.writeTimeout,
			IdleTimeout:       cfg.idleTimeout,
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
