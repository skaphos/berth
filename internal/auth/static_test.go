package auth

import (
	"context"
	"testing"
)

func TestNewStaticAuthenticatorCopiesKeys(t *testing.T) {
	t.Parallel()

	keys := map[string]Identity{
		"token": {Holder: "holder-a", Tenant: "tenant-a"},
	}

	authenticator := NewStaticAuthenticator(keys)
	keys["token"] = Identity{Holder: "holder-b", Tenant: "tenant-b"}

	identity, err := authenticator.Authenticate(context.Background(), "token")
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if identity.Holder != "holder-a" || identity.Tenant != "tenant-a" {
		t.Fatalf("identity = %+v, want original copy", identity)
	}
}

func TestStaticAuthenticatorAuthenticate(t *testing.T) {
	t.Parallel()

	authenticator := NewStaticAuthenticator(map[string]Identity{
		"token": {Holder: "holder-a", Tenant: "tenant-a", Raw: "token"},
	})

	t.Run("valid token", func(t *testing.T) {
		identity, err := authenticator.Authenticate(context.Background(), "token")
		if err != nil {
			t.Fatalf("Authenticate() error = %v", err)
		}
		if identity.Raw != "token" {
			t.Fatalf("raw = %q, want %q", identity.Raw, "token")
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		identity, err := authenticator.Authenticate(context.Background(), "missing")
		if err == nil {
			t.Fatal("expected error for invalid token")
		}
		if identity != nil {
			t.Fatalf("identity = %+v, want nil", identity)
		}
	})
}
