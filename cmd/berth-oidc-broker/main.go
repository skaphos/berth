// Command berth-oidc-broker is a small reference token broker for the
// Berth operator. It performs the OAuth2 client-credentials grant
// against an OIDC provider and writes the resulting access token to a
// file, refreshing before expiry.
//
// Designed to run as a sidecar to the operator: both containers share
// an emptyDir Memory volume; the broker writes to <output>, the
// operator reads it via --berth-api-key-file.
//
// Example:
//
//	berth-oidc-broker \
//	    --oidc-issuer-url=https://your-org.okta.com/oauth2/default \
//	    --oidc-client-id=$OKTA_CLIENT_ID \
//	    --oidc-client-secret-file=/etc/berth/secret \
//	    --oidc-audience=berth-api \
//	    --output=/var/run/berth/token
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2/clientcredentials"
)

const (
	defaultRefreshSkew = 60 * time.Second
	defaultMinRefresh  = 30 * time.Second
	discoveryTimeout   = 30 * time.Second
)

func main() {
	os.Exit(run())
}

func run() int {
	var (
		issuerURL        string
		tokenURLOverride string
		clientID         string
		clientSecret     string
		clientSecretFile string
		audience         string
		scopes           string
		outputPath       string
		refreshSkew      time.Duration
		minRefresh       time.Duration
	)
	flag.StringVar(&issuerURL, "oidc-issuer-url", "",
		"OIDC issuer URL (used to discover the token endpoint via /.well-known/openid-configuration)")
	flag.StringVar(&tokenURLOverride, "oidc-token-url", "",
		"OAuth2 token endpoint URL; overrides discovery from --oidc-issuer-url")
	flag.StringVar(&clientID, "oidc-client-id", "", "OAuth2 client id (required)")
	flag.StringVar(&clientSecret, "oidc-client-secret", "",
		"OAuth2 client secret. Prefer --oidc-client-secret-file in production so the secret "+
			"never appears on the command line or in process listings.")
	flag.StringVar(&clientSecretFile, "oidc-client-secret-file", "",
		"path to a file containing the OAuth2 client secret")
	flag.StringVar(&audience, "oidc-audience", "",
		"audience parameter sent on token requests (some IdPs — Auth0, certain Okta authorization servers — require this)")
	flag.StringVar(&scopes, "oidc-scopes", "", "comma-separated OAuth2 scopes")
	flag.StringVar(&outputPath, "output", "",
		"path where the current access token is written; written atomically via temp-file + rename (required)")
	flag.DurationVar(&refreshSkew, "refresh-skew", defaultRefreshSkew,
		"refresh the token this far before its declared expiry")
	flag.DurationVar(&minRefresh, "min-refresh-interval", defaultMinRefresh,
		"do not refresh (or retry on failure) more often than this")
	flag.Parse()

	if err := validateArgs(clientID, outputPath, issuerURL, tokenURLOverride, clientSecret, clientSecretFile); err != nil {
		slog.Error("invalid configuration", "error", err)
		return 2
	}

	secret, err := resolveSecret(clientSecret, clientSecretFile)
	if err != nil {
		slog.Error("load client secret", "error", err)
		return 2
	}

	tokenURL, err := resolveTokenURL(issuerURL, tokenURLOverride)
	if err != nil {
		slog.Error("resolve token endpoint", "error", err)
		return 1
	}

	cfg := &clientcredentials.Config{
		ClientID:     clientID,
		ClientSecret: secret,
		TokenURL:     tokenURL,
		Scopes:       parseScopes(scopes),
	}
	if audience != "" {
		cfg.EndpointParams = url.Values{"audience": {audience}}
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	slog.Info("token broker starting",
		"token_url", tokenURL,
		"output", outputPath,
		"refresh_skew", refreshSkew,
		"min_refresh", minRefresh)

	return runLoop(ctx, cfg, outputPath, refreshSkew, minRefresh)
}

func runLoop(ctx context.Context, cfg *clientcredentials.Config, outputPath string, refreshSkew, minRefresh time.Duration) int {
	for {
		if ctx.Err() != nil {
			return 0
		}
		token, err := cfg.Token(ctx)
		if err != nil {
			slog.Error("fetch token", "error", err)
			if !sleep(ctx, minRefresh) {
				return 0
			}
			continue
		}
		if err := writeTokenAtomic(outputPath, token.AccessToken); err != nil {
			slog.Error("write token file", "error", err, "path", outputPath)
			if !sleep(ctx, minRefresh) {
				return 0
			}
			continue
		}
		wait := nextRefresh(token.Expiry, refreshSkew, minRefresh)
		slog.Info("token refreshed", "expires_at", token.Expiry, "next_refresh_in", wait)
		if !sleep(ctx, wait) {
			return 0
		}
	}
}

// nextRefresh computes how long to wait before the next refresh. A
// zero or past expiry (some IdPs return tokens without `expires_in`)
// falls back to minRefresh so we don't spin.
func nextRefresh(expiry time.Time, skew, minRefresh time.Duration) time.Duration {
	if expiry.IsZero() {
		return minRefresh
	}
	wait := time.Until(expiry) - skew
	if wait < minRefresh {
		wait = minRefresh
	}
	return wait
}

// sleep blocks for d or until ctx is canceled. Returns true if it
// completed normally, false if canceled.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// writeTokenAtomic writes token to path via a sibling temp file and
// rename, so a reader (the operator's FileTokenSource) never observes
// a partially-written file.
func writeTokenAtomic(path, token string) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".token-")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmp := f.Name()
	cleanup := func() { _ = os.Remove(tmp) }
	if _, err := f.WriteString(token); err != nil {
		_ = f.Close()
		cleanup()
		return fmt.Errorf("write: %w", err)
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		cleanup()
		return fmt.Errorf("chmod: %w", err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		cleanup()
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

func validateArgs(clientID, outputPath, issuerURL, tokenURL, secret, secretFile string) error {
	if clientID == "" {
		return errors.New("--oidc-client-id is required")
	}
	if outputPath == "" {
		return errors.New("--output is required")
	}
	if issuerURL == "" && tokenURL == "" {
		return errors.New("either --oidc-issuer-url or --oidc-token-url is required")
	}
	if secret == "" && secretFile == "" {
		return errors.New("either --oidc-client-secret or --oidc-client-secret-file is required")
	}
	if secret != "" && secretFile != "" {
		return errors.New("--oidc-client-secret and --oidc-client-secret-file are mutually exclusive")
	}
	return nil
}

func resolveSecret(literal, file string) (string, error) {
	if literal != "" {
		return literal, nil
	}
	data, err := os.ReadFile(file) //nolint:gosec // operator-supplied path
	if err != nil {
		return "", fmt.Errorf("read %q: %w", file, err)
	}
	secret := strings.TrimSpace(string(data))
	if secret == "" {
		return "", fmt.Errorf("client secret file %q is empty", file)
	}
	return secret, nil
}

func resolveTokenURL(issuerURL, override string) (string, error) {
	if override != "" {
		return override, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), discoveryTimeout)
	defer cancel()
	provider, err := oidc.NewProvider(ctx, issuerURL)
	if err != nil {
		return "", fmt.Errorf("discover %q: %w", issuerURL, err)
	}
	var meta struct {
		TokenEndpoint string `json:"token_endpoint"`
	}
	if err := provider.Claims(&meta); err != nil {
		return "", fmt.Errorf("read discovery claims: %w", err)
	}
	if meta.TokenEndpoint == "" {
		return "", fmt.Errorf("discovery doc for %q is missing token_endpoint", issuerURL)
	}
	return meta.TokenEndpoint, nil
}

func parseScopes(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
