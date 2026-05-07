package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/skaphos/berth/internal/auth"
)

func TestChainMiddlewareAppliesInOrder(t *testing.T) {
	t.Parallel()

	base := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("X-Order", "handler")
		w.WriteHeader(http.StatusNoContent)
	})

	first := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("X-Order", "first-before")
			next.ServeHTTP(w, r)
			w.Header().Add("X-Order", "first-after")
		})
	}

	second := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("X-Order", "second-before")
			next.ServeHTTP(w, r)
			w.Header().Add("X-Order", "second-after")
		})
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)

	ChainMiddleware(base, first, second).ServeHTTP(recorder, request)

	got := recorder.Header().Values("X-Order")
	want := []string{"first-before", "second-before", "handler", "second-after", "first-after"}
	if len(got) != len(want) {
		t.Fatalf("header count = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("header[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

type fakeAuthenticator struct {
	identity *auth.Identity
	err      error
	calls    int
	lastTok  string
}

func (f *fakeAuthenticator) Authenticate(_ context.Context, token string) (*auth.Identity, error) {
	f.calls++
	f.lastTok = token
	return f.identity, f.err
}

func TestAuthMiddlewareSucceedsAndAttachesIdentity(t *testing.T) {
	t.Parallel()

	want := &auth.Identity{Holder: "team-a", Tenant: "team-a"}
	authn := &fakeAuthenticator{identity: want}

	var seen *auth.Identity
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = IdentityFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.Header.Set("Authorization", "Bearer raw-token")
	rec := httptest.NewRecorder()

	AuthMiddleware(authn)(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if authn.lastTok != "raw-token" {
		t.Fatalf("authenticator saw token = %q, want raw-token", authn.lastTok)
	}
	if seen == nil || seen.Holder != "team-a" {
		t.Fatalf("downstream identity = %+v, want Holder=team-a", seen)
	}
}

func TestAuthMiddlewareRejectsMissingHeader(t *testing.T) {
	t.Parallel()

	authn := &fakeAuthenticator{identity: &auth.Identity{Holder: "h"}}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("downstream must not be called")
	})

	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	rec := httptest.NewRecorder()
	AuthMiddleware(authn)(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if authn.calls != 0 {
		t.Fatalf("authenticator should not be invoked when header is missing; calls = %d", authn.calls)
	}
}

func TestAuthMiddlewareRejectsWrongScheme(t *testing.T) {
	t.Parallel()

	authn := &fakeAuthenticator{identity: &auth.Identity{Holder: "h"}}
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.Header.Set("Authorization", "Basic dGVzdDp0ZXN0")
	rec := httptest.NewRecorder()

	AuthMiddleware(authn)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("downstream must not be called")
	})).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestAuthMiddlewareRejectsAuthenticatorError(t *testing.T) {
	t.Parallel()

	authn := &fakeAuthenticator{err: errors.New("invalid")}
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.Header.Set("Authorization", "Bearer guess")
	rec := httptest.NewRecorder()

	AuthMiddleware(authn)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("downstream must not be called")
	})).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	// Body must not echo the rejected token or the upstream error string,
	// so it can't be used as an oracle.
	body := rec.Body.String()
	if strings.Contains(body, "guess") {
		t.Fatalf("response body leaked rejected token: %q", body)
	}
	if strings.Contains(body, "invalid") {
		t.Fatalf("response body leaked underlying error: %q", body)
	}
}

func TestIdentityFromContextWithoutMiddlewareReturnsNil(t *testing.T) {
	t.Parallel()
	if got := IdentityFromContext(context.Background()); got != nil {
		t.Fatalf("IdentityFromContext = %+v, want nil", got)
	}
}

func TestChainMiddlewareWithoutMiddlewareReturnsHandler(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)

	called := false
	base := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusAccepted)
	})

	ChainMiddleware(base).ServeHTTP(recorder, request)

	if !called {
		t.Fatal("base handler was not called")
	}
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusAccepted)
	}
}
