package auth

import "context"

type Identity struct {
	Holder string
	Tenant string
	Raw    string
}

type Authenticator interface {
	Authenticate(ctx context.Context, token string) (*Identity, error)
}
