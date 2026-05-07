package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
)

// OIDCConfig configures an [OIDCAuthenticator].
type OIDCConfig struct {
	// IssuerURL is the OIDC provider's issuer (e.g. "https://your-org.okta.com/oauth2/default",
	// "https://pingfed.example.com"). The provider's discovery document is
	// fetched from <IssuerURL>/.well-known/openid-configuration unless
	// JWKSURL overrides it.
	IssuerURL string

	// Audience is the expected `aud` claim value. JWTs not issued for this
	// audience are rejected.
	Audience string

	// JWKSURL, when non-empty, overrides the JWKS URL discovered from the
	// issuer. Useful when a provider serves discovery and JWKS at different
	// hosts (some legacy deployments).
	JWKSURL string

	// RequiredClaims is an optional set of claim/value pairs that must all
	// be present on the token for authentication to succeed. The check
	// matches both string-valued and string-array-valued claims (so
	// `{"groups": "berth-clients"}` is satisfied when the `groups` claim is
	// either the literal string "berth-clients" or a JSON array containing it).
	RequiredClaims map[string]string

	// UsernameClaim is the JWT claim copied into [Identity.Holder]. Default: "sub".
	UsernameClaim string

	// TenantClaim is the JWT claim copied into [Identity.Tenant]. When the
	// claim is array-valued the first element is used. Default: "sub".
	TenantClaim string
}

// OIDCAuthenticator validates JWTs issued by an OIDC provider. The
// signature is verified against the provider's JWKS (cached and refreshed
// on `kid` miss by the underlying go-oidc library); the standard JWT
// claims (iss, aud, exp, nbf) are validated; and any caller-supplied
// required claims are enforced.
//
// Construct via [NewOIDCAuthenticator]. The constructor performs an HTTP
// fetch against the issuer's discovery endpoint, so the API server fails
// fast at startup if the issuer is unreachable or misconfigured.
type OIDCAuthenticator struct {
	verifier       *oidc.IDTokenVerifier
	requiredClaims map[string]string
	usernameClaim  string
	tenantClaim    string
}

// NewOIDCAuthenticator constructs an OIDCAuthenticator. ctx is used for
// the discovery + JWKS HTTP fetches; pass a context with a sensible
// timeout in production.
func NewOIDCAuthenticator(ctx context.Context, cfg OIDCConfig) (*OIDCAuthenticator, error) {
	if cfg.IssuerURL == "" {
		return nil, errors.New("oidc: issuer URL is required")
	}
	if cfg.Audience == "" {
		return nil, errors.New("oidc: audience is required")
	}

	jwksURL := cfg.JWKSURL
	if jwksURL == "" {
		provider, err := oidc.NewProvider(ctx, cfg.IssuerURL)
		if err != nil {
			return nil, fmt.Errorf("oidc: discover provider %q: %w", cfg.IssuerURL, err)
		}
		var meta struct {
			JWKSURL string `json:"jwks_uri"`
		}
		if err := provider.Claims(&meta); err != nil {
			return nil, fmt.Errorf("oidc: read discovery claims: %w", err)
		}
		if meta.JWKSURL == "" {
			return nil, fmt.Errorf("oidc: provider %q discovery doc missing jwks_uri", cfg.IssuerURL)
		}
		jwksURL = meta.JWKSURL
	}
	keySet := oidc.NewRemoteKeySet(ctx, jwksURL)

	verifier := oidc.NewVerifier(cfg.IssuerURL, keySet, &oidc.Config{
		ClientID: cfg.Audience,
	})

	return &OIDCAuthenticator{
		verifier:       verifier,
		requiredClaims: cfg.RequiredClaims,
		usernameClaim:  defaultStr(cfg.UsernameClaim, "sub"),
		tenantClaim:    defaultStr(cfg.TenantClaim, "sub"),
	}, nil
}

// Authenticate verifies token. Returns a deliberately generic error on
// any failure to avoid signaling whether a key id is known, an audience
// is wrong, etc.
func (a *OIDCAuthenticator) Authenticate(ctx context.Context, token string) (*Identity, error) {
	if token == "" {
		return nil, errors.New("oidc: empty token")
	}
	idToken, err := a.verifier.Verify(ctx, token)
	if err != nil {
		return nil, errors.New("oidc: unauthorized")
	}

	claims := map[string]any{}
	if err := idToken.Claims(&claims); err != nil {
		return nil, errors.New("oidc: unauthorized")
	}

	for k, want := range a.requiredClaims {
		if !claimContains(claims, k, want) {
			return nil, errors.New("oidc: unauthorized")
		}
	}

	return &Identity{
		Holder: claimAsString(claims, a.usernameClaim),
		Tenant: claimAsString(claims, a.tenantClaim),
	}, nil
}

// claimContains reports whether claims[key] equals want (string-valued)
// or contains want (string-array-valued).
func claimContains(claims map[string]any, key, want string) bool {
	v, ok := claims[key]
	if !ok {
		return false
	}
	switch v := v.(type) {
	case string:
		return v == want
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && s == want {
				return true
			}
		}
	}
	return false
}

// claimAsString returns the string form of claims[key]. For array-valued
// claims, returns the first element. Returns "" if the claim is absent
// or not stringy.
func claimAsString(claims map[string]any, key string) string {
	v, ok := claims[key]
	if !ok {
		return ""
	}
	switch v := v.(type) {
	case string:
		return v
	case []any:
		if len(v) > 0 {
			if s, ok := v[0].(string); ok {
				return s
			}
		}
	}
	return ""
}

func defaultStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
