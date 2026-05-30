package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/skaphos/berth/internal/auth"
	"github.com/skaphos/berth/internal/lease"
)

// authzTestToken authenticates as tenant "team-a"; the default authorizer
// admits holders equal to or scoped under "team-a/".
const authzTestToken = "good-token"

func newAuthzServer(t *testing.T) *httptest.Server {
	t.Helper()
	authn := auth.NewStaticAuthenticator(map[string]auth.Identity{
		authzTestToken: {Holder: "team-a", Tenant: "team-a"},
	})
	mgr := lease.NewManager(lease.NewMemStore())
	srv := httptest.NewServer(NewMux(mgr, authn, nil))
	t.Cleanup(srv.Close)
	return srv
}

func postBearer(t *testing.T, srv *httptest.Server, token, path string, body any) *http.Response {
	t.Helper()
	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL+path, bytes.NewReader(buf))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// TestAuthzDeniesHolderOutsideTenant proves holder-binding is enforced on all
// three mutating endpoints: an authenticated caller cannot act as a holder that
// belongs to another tenant, even with a valid token. Authorization runs before
// the lease manager, so the bogus fencing tokens on renew/release never matter.
func TestAuthzDeniesHolderOutsideTenant(t *testing.T) {
	t.Parallel()

	srv := newAuthzServer(t)

	cases := []struct {
		op   string
		path string
		body any
	}{
		{op: "acquire", path: "/v1alpha1/namespaces/ns/leases/a/acquire", body: AcquireRequest{Holder: "team-b/x", TTLSeconds: 30}},
		{op: "renew", path: "/v1alpha1/namespaces/ns/leases/a/renew", body: RenewRequest{Holder: "team-b/x", FencingToken: 1, TTLSeconds: 30}},
		{op: "release", path: "/v1alpha1/namespaces/ns/leases/a/release", body: ReleaseRequest{Holder: "team-b/x", FencingToken: 1}},
	}
	for _, tc := range cases {
		t.Run(tc.op, func(t *testing.T) {
			t.Parallel()
			resp := postBearer(t, srv, authzTestToken, tc.path, tc.body)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("%s with foreign holder: status = %d, want 403", tc.op, resp.StatusCode)
			}
		})
	}
}

// TestAuthzAllowsInTenantHolderAcrossNamespaces proves the namespace policy is
// permissive (a tenant may operate in any namespace, as the cross-cluster model
// requires) while the holder stays tenant-scoped.
func TestAuthzAllowsInTenantHolderAcrossNamespaces(t *testing.T) {
	t.Parallel()

	srv := newAuthzServer(t)

	for _, ns := range []string{"team-a", "unrelated-namespace"} {
		path := "/v1alpha1/namespaces/" + ns + "/leases/a/acquire"
		resp := postBearer(t, srv, authzTestToken, path, AcquireRequest{Holder: "team-a/active-1", TTLSeconds: 30})
		func() {
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("acquire in %q with in-tenant holder: status = %d, want 200", ns, resp.StatusCode)
			}
		}()
	}
}

// TestAuthzBareTenantHolderAllowed covers the failover shape: a cluster whose
// identity, tenant, and holder all coincide (holder == tenant) is admitted.
func TestAuthzBareTenantHolderAllowed(t *testing.T) {
	t.Parallel()

	srv := newAuthzServer(t)
	resp := postBearer(t, srv, authzTestToken, "/v1alpha1/namespaces/ns/leases/a/acquire",
		AcquireRequest{Holder: "team-a", TTLSeconds: 30})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("acquire with holder == tenant: status = %d, want 200", resp.StatusCode)
	}
}

// TestAuthzNoneModeBypassesHolderBinding confirms that with authentication
// disabled (nil authn → no identity attached) any holder is accepted, preserving
// the auth-mode=none development path.
func TestAuthzNoneModeBypassesHolderBinding(t *testing.T) {
	t.Parallel()

	mgr := lease.NewManager(lease.NewMemStore())
	srv := httptest.NewServer(NewMux(mgr, nil, nil))
	t.Cleanup(srv.Close)

	resp := postBearer(t, srv, "", "/v1alpha1/namespaces/ns/leases/a/acquire",
		AcquireRequest{Holder: "any-unscoped-holder", TTLSeconds: 30})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("none-mode acquire with arbitrary holder: status = %d, want 200", resp.StatusCode)
	}
}
