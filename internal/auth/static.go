package auth

import (
	"context"
	"errors"
)

type StaticAuthenticator struct {
	keys map[string]Identity
}

func NewStaticAuthenticator(keys map[string]Identity) *StaticAuthenticator {
	copyKeys := make(map[string]Identity, len(keys))
	for k, v := range keys {
		copyKeys[k] = v
	}
	return &StaticAuthenticator{keys: copyKeys}
}

func (a *StaticAuthenticator) Authenticate(ctx context.Context, token string) (*Identity, error) {
	_ = ctx
	id, ok := a.keys[token]
	if !ok {
		return nil, errors.New("invalid static key")
	}
	return &id, nil
}
