package tenant

import (
	"testing"

	"github.com/skaphos/berth/internal/auth"
)

func TestIdentityResolverReturnsTenant(t *testing.T) {
	t.Parallel()

	got, err := IdentityResolver{}.ResolveTenant(&auth.Identity{Holder: "h", Tenant: "team-a"})
	if err != nil {
		t.Fatalf("ResolveTenant: %v", err)
	}
	if got != "team-a" {
		t.Fatalf("tenant = %q, want team-a", got)
	}
}

func TestIdentityResolverFailsClosed(t *testing.T) {
	t.Parallel()

	if _, err := (IdentityResolver{}).ResolveTenant(nil); err == nil {
		t.Fatal("nil identity must error")
	}
	if _, err := (IdentityResolver{}).ResolveTenant(&auth.Identity{Holder: "h"}); err == nil {
		t.Fatal("empty tenant must error")
	}
}

func TestDefaultAuthorizerNamespaceIsPermissive(t *testing.T) {
	t.Parallel()

	a := NewDefaultAuthorizer()
	id := &auth.Identity{Holder: "east", Tenant: "east"}
	// Any namespace is allowed for an identified caller — the cross-cluster model
	// has distinct tenants contend for the same namespace.
	for _, ns := range []string{"east", "west", "some-other-namespace"} {
		if err := a.AuthorizeNamespace(id, ns); err != nil {
			t.Fatalf("AuthorizeNamespace(%q) = %v, want allow", ns, err)
		}
	}
}

func TestDefaultAuthorizerNamespaceRejectsUntenantedIdentity(t *testing.T) {
	t.Parallel()

	a := NewDefaultAuthorizer()
	if err := a.AuthorizeNamespace(&auth.Identity{Holder: "h"}, "ns"); err == nil {
		t.Fatal("identity without a tenant must be denied")
	}
}

func TestDefaultAuthorizerHolder(t *testing.T) {
	t.Parallel()

	a := NewDefaultAuthorizer()
	id := &auth.Identity{Holder: "team-a", Tenant: "team-a"}

	cases := []struct {
		name    string
		holder  string
		wantErr bool
	}{
		{name: "bare tenant", holder: "team-a", wantErr: false},
		{name: "tenant-scoped sub-holder", holder: "team-a/active-000001", wantErr: false},
		{name: "tenant-scoped nested", holder: "team-a/active-000001-r3", wantErr: false},
		{name: "other tenant", holder: "team-b", wantErr: true},
		{name: "other tenant sub-holder", holder: "team-b/x", wantErr: true},
		{name: "prefix without separator", holder: "team-april", wantErr: true},
		{name: "empty holder", holder: "", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := a.AuthorizeHolder(id, tc.holder)
			if tc.wantErr && err == nil {
				t.Fatalf("AuthorizeHolder(%q) = nil, want deny", tc.holder)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("AuthorizeHolder(%q) = %v, want allow", tc.holder, err)
			}
		})
	}
}

func TestDefaultAuthorizerHolderRejectsUntenantedIdentity(t *testing.T) {
	t.Parallel()

	a := NewDefaultAuthorizer()
	if err := a.AuthorizeHolder(&auth.Identity{Holder: "h"}, "h"); err == nil {
		t.Fatal("identity without a tenant must be denied")
	}
}
