package auth

import (
	"context"
	"testing"
)

func TestNoOpAuthenticatorAcceptsEverything(t *testing.T) {
	t.Parallel()

	a := NoOpAuthenticator{}

	id, err := a.Authenticate(context.Background(), "")
	if err != nil {
		t.Fatalf("Authenticate empty: %v", err)
	}
	if id == nil || id.Holder != "anonymous" {
		t.Fatalf("Identity = %+v, want Holder=anonymous", id)
	}

	id, err = a.Authenticate(context.Background(), "any-token")
	if err != nil {
		t.Fatalf("Authenticate any: %v", err)
	}
	if id == nil || id.Tenant != "anonymous" {
		t.Fatalf("Identity = %+v, want Tenant=anonymous", id)
	}
}

// Compile-time assertion that NoOpAuthenticator satisfies Authenticator.
var _ Authenticator = NoOpAuthenticator{}
