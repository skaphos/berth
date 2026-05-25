package clientauth

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTokenFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	return path
}

func TestNewFileTokenSourceRequiresPath(t *testing.T) {
	t.Parallel()
	if _, err := NewFileTokenSource("", time.Second); err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestNewFileTokenSourceFailsFastOnMissingFile(t *testing.T) {
	t.Parallel()
	if _, err := NewFileTokenSource("/nonexistent/token", time.Second); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestNewFileTokenSourceFailsFastOnEmptyFile(t *testing.T) {
	t.Parallel()
	path := writeTokenFile(t, "   \n")
	if _, err := NewFileTokenSource(path, time.Second); err == nil {
		t.Fatal("expected error for empty file")
	}
}

func TestFileTokenSourceTrimsWhitespace(t *testing.T) {
	t.Parallel()
	path := writeTokenFile(t, "  jwt-payload  \n")
	s, err := NewFileTokenSource(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Get(); got != "jwt-payload" {
		t.Fatalf("Get = %q, want %q", got, "jwt-payload")
	}
}

func TestFileTokenSourcePicksUpRotationAfterTTL(t *testing.T) {
	t.Parallel()

	path := writeTokenFile(t, "v1")
	s, err := NewFileTokenSource(path, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Get(); got != "v1" {
		t.Fatalf("initial Get = %q, want v1", got)
	}

	if err := os.WriteFile(path, []byte("v2"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Within TTL window the cached value is returned.
	if got := s.Get(); got != "v1" {
		t.Fatalf("Get within TTL = %q, want v1 (cached)", got)
	}
	time.Sleep(20 * time.Millisecond)
	if got := s.Get(); got != "v2" {
		t.Fatalf("Get after TTL = %q, want v2 (re-read)", got)
	}
}

func TestFileTokenSourceReturnsCachedOnReadError(t *testing.T) {
	t.Parallel()

	path := writeTokenFile(t, "v1")
	s, err := NewFileTokenSource(path, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	// Remove the file mid-flight (sidecar broker briefly unavailable).
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if got := s.Get(); got != "v1" {
		t.Fatalf("Get after file removal = %q, want previous cached v1", got)
	}
}
