package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// testIssuer is a self-contained OIDC issuer for tests: it generates an
// RSA keypair, serves /.well-known/openid-configuration and a JWKS, and
// can mint signed JWTs on demand.
type testIssuer struct {
	t      *testing.T
	srv    *httptest.Server
	signer jose.Signer
	keyID  string
	priv   *rsa.PrivateKey
	pub    *rsa.PublicKey
}

func newTestIssuer(t *testing.T) *testIssuer {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa: %v", err)
	}
	keyID := "berth-test-key"
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: priv},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", keyID),
	)
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}

	iss := &testIssuer{
		t:      t,
		signer: signer,
		keyID:  keyID,
		priv:   priv,
		pub:    &priv.PublicKey,
	}

	mux := http.NewServeMux()
	iss.srv = httptest.NewServer(mux)
	t.Cleanup(iss.srv.Close)

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                iss.srv.URL,
			"jwks_uri":                              iss.srv.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
			// fields go-oidc requires for discovery validation
			"authorization_endpoint": iss.srv.URL + "/auth",
			"token_endpoint":         iss.srv.URL + "/token",
			"response_types_supported": []string{"id_token"},
			"subject_types_supported":  []string{"public"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		jwks := jose.JSONWebKeySet{
			Keys: []jose.JSONWebKey{{
				Key:       iss.pub,
				KeyID:     iss.keyID,
				Algorithm: string(jose.RS256),
				Use:       "sig",
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwks)
	})

	return iss
}

func (i *testIssuer) URL() string { return i.srv.URL }

// mint issues a signed JWT with the given standard claims and arbitrary
// extra claims.
func (i *testIssuer) mint(t *testing.T, claims jwt.Claims, extras map[string]any) string {
	t.Helper()
	if claims.Issuer == "" {
		claims.Issuer = i.srv.URL
	}
	if claims.IssuedAt == nil {
		now := jwt.NewNumericDate(time.Now())
		claims.IssuedAt = now
	}
	if claims.Expiry == nil {
		exp := jwt.NewNumericDate(time.Now().Add(time.Hour))
		claims.Expiry = exp
	}

	builder := jwt.Signed(i.signer).Claims(claims)
	if len(extras) > 0 {
		builder = builder.Claims(extras)
	}
	tok, err := builder.Serialize()
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	return tok
}

func newOIDCAuth(t *testing.T, iss *testIssuer, audience string, opts ...func(*OIDCConfig)) *OIDCAuthenticator {
	t.Helper()
	cfg := OIDCConfig{
		IssuerURL: iss.URL(),
		Audience:  audience,
	}
	for _, o := range opts {
		o(&cfg)
	}
	a, err := NewOIDCAuthenticator(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewOIDCAuthenticator: %v", err)
	}
	return a
}

func TestOIDCAuthenticatorHappyPath(t *testing.T) {
	t.Parallel()

	iss := newTestIssuer(t)
	a := newOIDCAuth(t, iss, "berth-api")

	tok := iss.mint(t, jwt.Claims{
		Subject:  "operator@cluster-east",
		Audience: jwt.Audience{"berth-api"},
	}, nil)

	id, err := a.Authenticate(context.Background(), tok)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if id.Holder != "operator@cluster-east" {
		t.Fatalf("Holder = %q, want operator@cluster-east", id.Holder)
	}
	if id.Tenant != "operator@cluster-east" {
		t.Fatalf("Tenant = %q, want operator@cluster-east (default sub)", id.Tenant)
	}
}

func TestOIDCAuthenticatorRejectsExpiredToken(t *testing.T) {
	t.Parallel()

	iss := newTestIssuer(t)
	a := newOIDCAuth(t, iss, "berth-api")

	exp := jwt.NewNumericDate(time.Now().Add(-time.Minute))
	tok := iss.mint(t, jwt.Claims{
		Subject:  "x",
		Audience: jwt.Audience{"berth-api"},
		Expiry:   exp,
	}, nil)

	if _, err := a.Authenticate(context.Background(), tok); err == nil {
		t.Fatal("expected expired token to be rejected")
	}
}

func TestOIDCAuthenticatorRejectsWrongAudience(t *testing.T) {
	t.Parallel()

	iss := newTestIssuer(t)
	a := newOIDCAuth(t, iss, "berth-api")

	tok := iss.mint(t, jwt.Claims{
		Subject:  "x",
		Audience: jwt.Audience{"some-other-api"},
	}, nil)

	if _, err := a.Authenticate(context.Background(), tok); err == nil {
		t.Fatal("expected wrong-audience token to be rejected")
	}
}

func TestOIDCAuthenticatorRejectsWrongIssuer(t *testing.T) {
	t.Parallel()

	iss := newTestIssuer(t)
	a := newOIDCAuth(t, iss, "berth-api")

	tok := iss.mint(t, jwt.Claims{
		Subject:  "x",
		Issuer:   "https://attacker.example.com",
		Audience: jwt.Audience{"berth-api"},
	}, nil)

	if _, err := a.Authenticate(context.Background(), tok); err == nil {
		t.Fatal("expected wrong-issuer token to be rejected")
	}
}

func TestOIDCAuthenticatorRejectsWrongSignature(t *testing.T) {
	t.Parallel()

	iss := newTestIssuer(t)
	a := newOIDCAuth(t, iss, "berth-api")

	// Mint a token with a *different* keypair (not in the issuer's JWKS).
	otherPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	otherSigner, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: otherPriv},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "rogue-key"),
	)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := jwt.Signed(otherSigner).Claims(jwt.Claims{
		Issuer:   iss.URL(),
		Subject:  "x",
		Audience: jwt.Audience{"berth-api"},
		Expiry:   jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}).Serialize()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := a.Authenticate(context.Background(), tok); err == nil {
		t.Fatal("expected token signed with unknown key to be rejected")
	}
}

func TestOIDCAuthenticatorRejectsEmptyToken(t *testing.T) {
	t.Parallel()
	iss := newTestIssuer(t)
	a := newOIDCAuth(t, iss, "berth-api")
	if _, err := a.Authenticate(context.Background(), ""); err == nil {
		t.Fatal("expected empty token to be rejected")
	}
}

func TestOIDCAuthenticatorRequiredClaimEnforcement(t *testing.T) {
	t.Parallel()

	iss := newTestIssuer(t)
	a := newOIDCAuth(t, iss, "berth-api", func(c *OIDCConfig) {
		c.RequiredClaims = map[string]string{"groups": "berth-clients"}
	})

	good := iss.mint(t, jwt.Claims{
		Subject:  "x",
		Audience: jwt.Audience{"berth-api"},
	}, map[string]any{"groups": []string{"berth-clients", "infra"}})
	if _, err := a.Authenticate(context.Background(), good); err != nil {
		t.Fatalf("token with required claim must be accepted: %v", err)
	}

	noClaim := iss.mint(t, jwt.Claims{
		Subject:  "x",
		Audience: jwt.Audience{"berth-api"},
	}, nil)
	if _, err := a.Authenticate(context.Background(), noClaim); err == nil {
		t.Fatal("token missing required claim must be rejected")
	}

	wrongValue := iss.mint(t, jwt.Claims{
		Subject:  "x",
		Audience: jwt.Audience{"berth-api"},
	}, map[string]any{"groups": []string{"some-other-group"}})
	if _, err := a.Authenticate(context.Background(), wrongValue); err == nil {
		t.Fatal("token with wrong claim value must be rejected")
	}
}

func TestOIDCAuthenticatorCustomUsernameAndTenantClaims(t *testing.T) {
	t.Parallel()

	iss := newTestIssuer(t)
	a := newOIDCAuth(t, iss, "berth-api", func(c *OIDCConfig) {
		c.UsernameClaim = "preferred_username"
		c.TenantClaim = "groups"
	})

	tok := iss.mint(t, jwt.Claims{
		Subject:  "uuid-1234",
		Audience: jwt.Audience{"berth-api"},
	}, map[string]any{
		"preferred_username": "alice",
		"groups":             []string{"team-alpha", "team-beta"},
	})

	id, err := a.Authenticate(context.Background(), tok)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if id.Holder != "alice" {
		t.Fatalf("Holder = %q, want alice", id.Holder)
	}
	if id.Tenant != "team-alpha" {
		t.Fatalf("Tenant = %q, want team-alpha (first element of array claim)", id.Tenant)
	}
}

func TestOIDCAuthenticatorErrorMessageIsGeneric(t *testing.T) {
	t.Parallel()

	iss := newTestIssuer(t)
	a := newOIDCAuth(t, iss, "berth-api")

	_, err := a.Authenticate(context.Background(), "obviously-not-a-jwt")
	if err == nil {
		t.Fatal("expected error")
	}
	// Must not echo the rejected token.
	if strings.Contains(err.Error(), "obviously-not-a-jwt") {
		t.Fatalf("error leaked the input token: %v", err)
	}
}

func TestNewOIDCAuthenticatorRejectsMissingConfig(t *testing.T) {
	t.Parallel()

	if _, err := NewOIDCAuthenticator(context.Background(), OIDCConfig{Audience: "x"}); err == nil {
		t.Fatal("expected error when issuer URL is missing")
	}
	if _, err := NewOIDCAuthenticator(context.Background(), OIDCConfig{IssuerURL: "https://example"}); err == nil {
		t.Fatal("expected error when audience is missing")
	}
}

func TestNewOIDCAuthenticatorFailsOnUnreachableIssuer(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	// Use a port we can be confident is closed.
	_, err := NewOIDCAuthenticator(ctx, OIDCConfig{
		IssuerURL: "http://127.0.0.1:1",
		Audience:  "berth-api",
	})
	if err == nil {
		t.Fatal("expected discovery to fail against an unreachable issuer")
	}
}

// Compile-time assertion the type satisfies Authenticator.
var _ Authenticator = (*OIDCAuthenticator)(nil)
