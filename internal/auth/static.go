package auth

import (
	"context"
	"errors"
)

// StaticAuthenticator implements [Authenticator] using a fixed map of API
// keys to identities. The key map is copied at construction time and is
// safe for concurrent reads.
type StaticAuthenticator struct {
	keys map[string]Identity
}

// NewStaticAuthenticator creates a [StaticAuthenticator] with a defensive
// copy of the provided key-to-identity map.
func NewStaticAuthenticator(keys map[string]Identity) *StaticAuthenticator {
	copyKeys := make(map[string]Identity, len(keys))
	for k, v := range keys {
		copyKeys[k] = v
	}
	return &StaticAuthenticator{keys: copyKeys}
}

// Authenticate looks up the token in the static key map and returns the
// matching [Identity]. It returns an error if the token is not found.
func (a *StaticAuthenticator) Authenticate(ctx context.Context, token string) (*Identity, error) {
	_ = ctx
	id, ok := a.keys[token]
	if !ok {
		return nil, errors.New("invalid static key")
	}
	return &id, nil
}
