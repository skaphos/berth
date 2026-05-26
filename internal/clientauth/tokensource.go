package clientauth

import (
	"errors"
	"os"
	"strings"
	"sync"
	"time"
)

// FileTokenSource reads a bearer token from a file each time the cached
// value goes stale. It exists so the operator can present a refreshing
// JWT minted by an external sidecar (typically an OIDC token broker)
// without re-implementing the IdP integration in the operator itself.
//
// The file is expected to contain just the raw token (whitespace is
// trimmed). On read errors the previously-cached token is returned;
// callers can detect prolonged staleness via the API server returning
// 401, which is handled by the standard reconciler retry/backoff.
type FileTokenSource struct {
	path     string
	cacheTTL time.Duration

	mu       sync.Mutex
	cached   string
	cachedAt time.Time
}

// NewFileTokenSource returns a FileTokenSource that reads from path.
// cacheTTL bounds how often the file is re-read; a small value (e.g.
// 1s) is a safe default — file-system reads are cheap, and a fresh
// rotation written by a sidecar will be picked up within one TTL window.
func NewFileTokenSource(path string, cacheTTL time.Duration) (*FileTokenSource, error) {
	if path == "" {
		return nil, errors.New("token source: file path is required")
	}
	if cacheTTL <= 0 {
		cacheTTL = time.Second
	}
	s := &FileTokenSource{path: path, cacheTTL: cacheTTL}
	// Warm the cache so a missing/unreadable file fails fast at startup
	// rather than on first reconcile.
	if _, err := s.read(); err != nil {
		return nil, err
	}
	return s, nil
}

// Get returns the current token. It re-reads the file when the cached
// value is older than cacheTTL. On read error the previously-cached
// token is returned (the operator's reconcile retry will surface
// repeated failures via 401s).
func (s *FileTokenSource) Get() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Since(s.cachedAt) < s.cacheTTL {
		return s.cached
	}
	if tok, err := s.readLocked(); err == nil {
		s.cached = tok
		s.cachedAt = time.Now()
	}
	return s.cached
}

func (s *FileTokenSource) read() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tok, err := s.readLocked()
	if err != nil {
		return "", err
	}
	s.cached = tok
	s.cachedAt = time.Now()
	return tok, nil
}

func (s *FileTokenSource) readLocked() (string, error) {
	data, err := os.ReadFile(s.path) //nolint:gosec // operator-supplied path
	if err != nil {
		return "", err
	}
	tok := strings.TrimSpace(string(data))
	if tok == "" {
		return "", errors.New("token source: file is empty")
	}
	return tok, nil
}
