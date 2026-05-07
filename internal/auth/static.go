package auth

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
)

// StaticAuthenticator implements [Authenticator] using a fixed set of
// SHA-256-hashed bearer tokens.
//
// Tokens are never stored in their raw form. The on-disk file format
// expects the hash so secret material only lives wherever the operator's
// raw token is mounted (typically a Kubernetes Secret on the operator
// side); the API server only ever holds hashes.
//
// Use [NewStaticAuthenticator] to construct from raw tokens (typical for
// tests / in-process dev). Use [NewStaticAuthenticatorFromKeysFile] for
// production deployments that mount a hashed-keys file.
type StaticAuthenticator struct {
	mu       sync.RWMutex
	keys     map[string]Identity // keyed by hex(sha256(rawToken))
	filePath string              // empty when constructed from a map
}

// NewStaticAuthenticator constructs a StaticAuthenticator from a map of
// raw token strings to identities. Tokens are hashed before storage.
// Intended for tests and single-process dev setups; production should
// prefer [NewStaticAuthenticatorFromKeysFile].
func NewStaticAuthenticator(keys map[string]Identity) *StaticAuthenticator {
	hashed := make(map[string]Identity, len(keys))
	for raw, id := range keys {
		hashed[hashToken(raw)] = id
	}
	return &StaticAuthenticator{keys: hashed}
}

// NewStaticAuthenticatorFromKeysFile reads path and constructs a
// StaticAuthenticator. The file format is one entry per line:
//
//	<key-id>:<sha256-of-token-as-hex>
//
// Lines beginning with `#` and blank lines are ignored. The key id is
// applied to Identity.Holder and Identity.Tenant.
//
// The path is retained so [StaticAuthenticator.Reload] can re-read on
// demand (e.g., on SIGHUP) without restarting the server.
func NewStaticAuthenticatorFromKeysFile(path string) (*StaticAuthenticator, error) {
	if path == "" {
		return nil, errors.New("static auth: keys file path is required")
	}
	a := &StaticAuthenticator{filePath: path}
	if err := a.Reload(); err != nil {
		return nil, err
	}
	return a, nil
}

// Reload re-reads the keys file the authenticator was constructed from
// and atomically replaces its in-memory key set on success. On error,
// the existing key set is left unchanged so a malformed update can't
// silently lock the system out.
func (a *StaticAuthenticator) Reload() error {
	if a.filePath == "" {
		return errors.New("static auth: no file path configured for reload")
	}
	keys, err := loadKeysFile(a.filePath)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.keys = keys
	a.mu.Unlock()
	return nil
}

// Authenticate hashes token, looks it up in the key set, and returns the
// matching Identity. Returns a deliberately generic error on any failure
// to avoid leaking which key ids exist.
func (a *StaticAuthenticator) Authenticate(_ context.Context, token string) (*Identity, error) {
	if token == "" {
		return nil, errors.New("static auth: empty token")
	}
	hash := hashToken(token)
	a.mu.RLock()
	id, ok := a.keys[hash]
	a.mu.RUnlock()
	if !ok {
		return nil, errors.New("static auth: unauthorized")
	}
	return &id, nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func loadKeysFile(path string) (map[string]Identity, error) {
	f, err := os.Open(path) //nolint:gosec // path is operator-supplied via flag
	if err != nil {
		return nil, fmt.Errorf("open keys file %q: %w", path, err)
	}
	defer f.Close()

	keys := map[string]Identity{}
	scanner := bufio.NewScanner(f)
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		parts := strings.SplitN(text, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("keys file %q line %d: expected '<key-id>:<sha256-hex>'", path, line)
		}
		keyID := strings.TrimSpace(parts[0])
		hashHex := strings.TrimSpace(parts[1])
		if keyID == "" {
			return nil, fmt.Errorf("keys file %q line %d: empty key id", path, line)
		}
		if !validHex256(hashHex) {
			return nil, fmt.Errorf("keys file %q line %d: hash must be 64 lowercase hex chars", path, line)
		}
		if _, dup := keys[hashHex]; dup {
			return nil, fmt.Errorf("keys file %q line %d: duplicate hash for key id %q", path, line, keyID)
		}
		keys[hashHex] = Identity{Holder: keyID, Tenant: keyID}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan keys file %q: %w", path, err)
	}
	return keys, nil
}

func validHex256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		default:
			return false
		}
	}
	return true
}
