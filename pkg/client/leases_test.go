package client

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/skaphos/berth/internal/api"
	"github.com/skaphos/berth/internal/auth"
	"github.com/skaphos/berth/internal/lease"
)

func newTestClientServer(t *testing.T) (*Client, func()) {
	t.Helper()
	mgr := lease.NewManager(lease.NewMemStore())
	// nil authn — these tests focus on the wire contract, not auth.
	srv := httptest.NewServer(api.NewMux(mgr, nil))
	c := New(srv.URL)
	return c, srv.Close
}

func TestClientAcquireRenewRelease(t *testing.T) {
	t.Parallel()

	c, cleanup := newTestClientServer(t)
	defer cleanup()
	ctx := context.Background()

	res, err := c.Acquire(ctx, "ns", "a", "h1", 30*time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if !res.Acquired || res.FencingToken != 1 {
		t.Fatalf("Acquire = %+v, want Acquired+token=1", res)
	}

	renewed, err := c.Renew(ctx, "ns", "a", "h1", res.FencingToken, 30*time.Second)
	if err != nil {
		t.Fatalf("Renew: %v", err)
	}
	if !renewed.Acquired {
		t.Fatal("Renew should succeed")
	}

	if err := c.Release(ctx, "ns", "a", "h1", res.FencingToken); err != nil {
		t.Fatalf("Release: %v", err)
	}
}

func TestClientReleaseStaleReturnsErrConflict(t *testing.T) {
	t.Parallel()

	c, cleanup := newTestClientServer(t)
	defer cleanup()
	ctx := context.Background()

	first, err := c.Acquire(ctx, "ns", "a", "h1", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Release(ctx, "ns", "a", "h1", first.FencingToken); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Acquire(ctx, "ns", "a", "h2", 30*time.Second); err != nil {
		t.Fatal(err)
	}

	err = c.Release(ctx, "ns", "a", "h1", first.FencingToken)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
}

func TestClientWithAPIKeyAuthenticatesAgainstStaticKeys(t *testing.T) {
	t.Parallel()

	authn := auth.NewStaticAuthenticator(map[string]auth.Identity{
		"good-token": {Holder: "team-a", Tenant: "team-a"},
	})
	mgr := lease.NewManager(lease.NewMemStore())
	srv := httptest.NewServer(api.NewMux(mgr, authn))
	defer srv.Close()

	good := New(srv.URL, WithAPIKey("good-token"))
	if _, err := good.Acquire(context.Background(), "ns", "x", "h1", 30*time.Second); err != nil {
		t.Fatalf("Acquire with valid key: %v", err)
	}

	bad := New(srv.URL, WithAPIKey("wrong-token"))
	if _, err := bad.Acquire(context.Background(), "ns", "x", "h2", 30*time.Second); err == nil {
		t.Fatal("Acquire with wrong key must fail")
	}

	noKey := New(srv.URL)
	if _, err := noKey.Acquire(context.Background(), "ns", "x", "h3", 30*time.Second); err == nil {
		t.Fatal("Acquire without key must fail")
	}
}

func TestClientPathEscapesNamespaceAndName(t *testing.T) {
	t.Parallel()

	c, cleanup := newTestClientServer(t)
	defer cleanup()

	// Names with slashes/spaces would otherwise fail to route. The lease
	// keys are still valid in the store; the wire format must escape them.
	res, err := c.Acquire(context.Background(), "team a", "service/x", "h", 30*time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if !res.Acquired {
		t.Fatal("expected Acquired=true")
	}
}
