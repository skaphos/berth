package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2/clientcredentials"
)

func TestValidateArgs(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		clientID, outputPath, issuerURL, tokenURL, secret, secretFile string
		wantErr                                                       bool
	}{
		"happy issuer + secret":        {clientID: "x", outputPath: "/tmp/t", issuerURL: "https://i", secret: "s"},
		"happy token-url + file":       {clientID: "x", outputPath: "/tmp/t", tokenURL: "https://i/token", secretFile: "/etc/secret"},
		"missing client id":            {outputPath: "/tmp/t", issuerURL: "https://i", secret: "s", wantErr: true},
		"missing output":               {clientID: "x", issuerURL: "https://i", secret: "s", wantErr: true},
		"no issuer or token url":       {clientID: "x", outputPath: "/tmp/t", secret: "s", wantErr: true},
		"no secret":                    {clientID: "x", outputPath: "/tmp/t", issuerURL: "https://i", wantErr: true},
		"both literal and file secret": {clientID: "x", outputPath: "/tmp/t", issuerURL: "https://i", secret: "s", secretFile: "/etc/secret", wantErr: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := validateArgs(tc.clientID, tc.outputPath, tc.issuerURL, tc.tokenURL, tc.secret, tc.secretFile)
			if tc.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestResolveSecretFromLiteral(t *testing.T) {
	t.Parallel()
	got, err := resolveSecret("literal", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "literal" {
		t.Fatalf("got %q, want literal", got)
	}
}

func TestResolveSecretFromFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "secret")
	if err := os.WriteFile(path, []byte("  abcd1234\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := resolveSecret("", path)
	if err != nil {
		t.Fatal(err)
	}
	if got != "abcd1234" {
		t.Fatalf("got %q, want abcd1234 (whitespace trimmed)", got)
	}
}

func TestResolveSecretFromMissingFile(t *testing.T) {
	t.Parallel()
	if _, err := resolveSecret("", "/nonexistent/secret"); err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveSecretFromEmptyFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "secret")
	if err := os.WriteFile(path, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveSecret("", path); err == nil {
		t.Fatal("expected error for empty secret file")
	}
}

func TestResolveTokenURLOverride(t *testing.T) {
	t.Parallel()
	got, err := resolveTokenURL("ignored", "https://issuer.example.com/token")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://issuer.example.com/token" {
		t.Fatalf("got %q, want override", got)
	}
}

func TestResolveTokenURLViaDiscovery(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                srv.URL,
			"token_endpoint":                        srv.URL + "/oauth/token",
			"jwks_uri":                              srv.URL + "/jwks",
			"authorization_endpoint":                srv.URL + "/auth",
			"id_token_signing_alg_values_supported": []string{"RS256"},
			"response_types_supported":              []string{"id_token"},
			"subject_types_supported":               []string{"public"},
		})
	})

	got, err := resolveTokenURL(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != srv.URL+"/oauth/token" {
		t.Fatalf("got %q, want %s/oauth/token", got, srv.URL)
	}
}

func TestNextRefresh(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		expiry     time.Time
		skew       time.Duration
		minRefresh time.Duration
		want       time.Duration
		atLeast    bool // true if want is a lower bound (within reasonable wall-clock jitter)
	}{
		"zero expiry falls back to min": {
			expiry: time.Time{}, skew: 30 * time.Second, minRefresh: 60 * time.Second,
			want: 60 * time.Second,
		},
		"past expiry returns min": {
			expiry: time.Now().Add(-time.Minute), skew: 0, minRefresh: 30 * time.Second,
			want: 30 * time.Second,
		},
		"normal expiry": {
			expiry: time.Now().Add(2 * time.Hour), skew: time.Minute, minRefresh: 30 * time.Second,
			want: 2*time.Hour - time.Minute, atLeast: true,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := nextRefresh(tc.expiry, tc.skew, tc.minRefresh)
			if tc.atLeast {
				if got > tc.want+5*time.Second || got < tc.want-5*time.Second {
					t.Fatalf("got %v, want ~%v", got, tc.want)
				}
				return
			}
			if got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestWriteTokenAtomic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "token")

	if err := writeTokenAtomic(path, "the-jwt"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "the-jwt" {
		t.Fatalf("file contents = %q, want the-jwt", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("mode = %v, want 0600", mode)
	}

	// Write a second token; rename must succeed even with the existing file.
	if err := writeTokenAtomic(path, "the-jwt-v2"); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "the-jwt-v2" {
		t.Fatalf("file contents after rotation = %q, want the-jwt-v2", got)
	}

	// No leftover temp files in the dir.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".token-") {
			t.Fatalf("leftover temp file: %s", e.Name())
		}
	}
}

func TestParseScopes(t *testing.T) {
	t.Parallel()
	cases := map[string][]string{
		"":                nil,
		"openid":          {"openid"},
		"openid,profile":  {"openid", "profile"},
		" openid , email": {"openid", "email"},
	}
	for in, want := range cases {
		if got := parseScopes(in); !reflect.DeepEqual(got, want) {
			t.Fatalf("parseScopes(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestRunLoopFetchesAndWritesToken exercises the broker against a fake
// token endpoint (just enough OAuth2 to satisfy clientcredentials.Token).
func TestRunLoopFetchesAndWritesToken(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	out := filepath.Join(dir, "token")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "client_credentials" {
			http.Error(w, "wrong grant", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": fmt.Sprintf("jwt-%d", time.Now().UnixNano()),
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer srv.Close()

	cfg := &clientcredentials.Config{
		ClientID:       "test",
		ClientSecret:   "secret",
		TokenURL:       srv.URL,
		EndpointParams: url.Values{"audience": {"berth-api"}},
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		// Let the loop fetch + write once, then cancel.
		for i := 0; i < 50; i++ {
			if _, err := os.Stat(out); err == nil {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		cancel()
	}()
	rc := runLoop(ctx, cfg, out, 0, 24*time.Hour)
	if rc != 0 {
		t.Fatalf("runLoop exit = %d, want 0", rc)
	}

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got), "jwt-") {
		t.Fatalf("token file = %q, want a jwt-... value", got)
	}
}
