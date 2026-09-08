package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
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
	rc := runLoop(ctx, cfg, testLoopConfig(out))
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

// testLoopConfig returns a loop configuration that refreshes once and then
// waits effectively forever, for tests that cancel after the first write.
func testLoopConfig(out string) loopConfig {
	return loopConfig{
		outputPath:       out,
		refreshSkew:      0,
		minRefresh:       24 * time.Hour,
		fetchTimeout:     5 * time.Second,
		maxRetryInterval: 24 * time.Hour,
	}
}

// TestRunLoopRecoversFromStalledTokenEndpoint covers #159: an endpoint that
// accepts the request and never answers must be abandoned at the fetch
// deadline, the last good token file must survive the outage untouched, and
// the loop must pick up the new token once the endpoint recovers.
func TestRunLoopRecoversFromStalledTokenEndpoint(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	out := filepath.Join(dir, "token")
	if err := os.WriteFile(out, []byte("previous-good-token"), 0o600); err != nil {
		t.Fatal(err)
	}

	const stalls = 2
	var calls, stalledCanceled atomic.Int32
	stop := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Drain the body first: the server only watches for a closed client
		// connection (and cancels r.Context) once the request body is consumed.
		_ = r.ParseForm()
		if calls.Add(1) <= stalls {
			// Accept the request, then hold it until the client gives up.
			select {
			case <-r.Context().Done():
				stalledCanceled.Add(1)
			case <-stop:
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "recovered-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(stop) }) // runs before srv.Close so stalled handlers can never wedge shutdown

	cfg := &clientcredentials.Config{ClientID: "test", ClientSecret: "secret", TokenURL: srv.URL}
	lc := loopConfig{
		outputPath:       out,
		minRefresh:       20 * time.Millisecond,
		fetchTimeout:     100 * time.Millisecond,
		maxRetryInterval: 50 * time.Millisecond,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)
	go func() { done <- runLoop(ctx, cfg, lc) }()

	deadline := time.Now().Add(5 * time.Second)
	for {
		got, err := os.ReadFile(out)
		if err != nil {
			t.Fatalf("token file must exist throughout the outage: %v", err)
		}
		if string(got) == "recovered-token" {
			break
		}
		if string(got) != "previous-good-token" {
			t.Fatalf("token file = %q; the last good token must be preserved while the endpoint stalls", got)
		}
		if time.Now().After(deadline) {
			t.Fatalf("token file = %q, loop never recovered after the endpoint came back", got)
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case rc := <-done:
		if rc != 0 {
			t.Fatalf("runLoop exit = %d, want 0", rc)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runLoop did not exit after cancel")
	}
	for stalledCanceled.Load() != stalls {
		if time.Now().After(deadline) {
			t.Fatalf("stalled requests canceled = %d, want %d", stalledCanceled.Load(), stalls)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestRunLoopCancelUnblocksStalledFetch covers process shutdown while a fetch
// is stalled: the in-flight request must be canceled by the process context
// even though its own deadline is far away, and no token may be written.
func TestRunLoopCancelUnblocksStalledFetch(t *testing.T) {
	t.Parallel()

	out := filepath.Join(t.TempDir(), "token")
	inFlight := make(chan struct{}, 1)
	stop := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm() // see TestRunLoopRecoversFromStalledTokenEndpoint
		select {
		case inFlight <- struct{}{}:
		default:
		}
		select {
		case <-r.Context().Done():
		case <-stop:
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(stop) })

	cfg := &clientcredentials.Config{ClientID: "test", ClientSecret: "secret", TokenURL: srv.URL}
	lc := loopConfig{outputPath: out, minRefresh: time.Hour, fetchTimeout: time.Hour, maxRetryInterval: time.Hour}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() { done <- runLoop(ctx, cfg, lc) }()

	select {
	case <-inFlight:
	case <-time.After(5 * time.Second):
		t.Fatal("token request never reached the endpoint")
	}
	cancel()
	select {
	case rc := <-done:
		if rc != 0 {
			t.Fatalf("runLoop exit = %d, want 0", rc)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("runLoop stayed blocked in a stalled fetch after cancel")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("a token file was written despite no successful fetch: %v", err)
	}
}

func TestNextRetryBackoff(t *testing.T) {
	t.Parallel()

	tests := []struct {
		current, limit, want time.Duration
	}{
		{30 * time.Second, 5 * time.Minute, time.Minute},
		{2 * time.Minute, 5 * time.Minute, 4 * time.Minute},
		{4 * time.Minute, 5 * time.Minute, 5 * time.Minute},
		{5 * time.Minute, 5 * time.Minute, 5 * time.Minute},
		{time.Duration(1<<62) + 1, 5 * time.Minute, 5 * time.Minute}, // overflow guard
	}
	for _, tt := range tests {
		if got := nextRetry(tt.current, tt.limit); got != tt.want {
			t.Errorf("nextRetry(%s, %s) = %s, want %s", tt.current, tt.limit, got, tt.want)
		}
	}
}

func TestLoopConfigValidate(t *testing.T) {
	t.Parallel()

	valid := loopConfig{outputPath: "/t", minRefresh: 30 * time.Second, fetchTimeout: 30 * time.Second, maxRetryInterval: 5 * time.Minute}
	if err := valid.validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	for name, mutate := range map[string]func(*loopConfig){
		"zero fetch timeout":          func(lc *loopConfig) { lc.fetchTimeout = 0 },
		"zero min refresh":            func(lc *loopConfig) { lc.minRefresh = 0 },
		"max retry below min refresh": func(lc *loopConfig) { lc.maxRetryInterval = time.Second },
		"negative fetch timeout":      func(lc *loopConfig) { lc.fetchTimeout = -time.Second },
		"negative refresh skew":       func(lc *loopConfig) { lc.refreshSkew = -time.Second },
	} {
		lc := valid
		mutate(&lc)
		if err := lc.validate(); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

// Exercise startup failures through the CLI wiring, including discovery through
// the upgraded OIDC client. No token output may be created on these failures.
func TestRunRejectsInvalidStartup(t *testing.T) {
	originalFlags, originalArgs := flag.CommandLine, os.Args
	t.Cleanup(func() { flag.CommandLine, os.Args = originalFlags, originalArgs })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "discovery unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	dir := t.TempDir()
	secret := filepath.Join(dir, "secret")
	if err := os.WriteFile(secret, []byte("test-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		args []string
		want int
	}{
		{"missing configuration", nil, 2},
		{"unreadable secret", []string{"--oidc-client-id=test", "--oidc-token-url=" + srv.URL, "--oidc-client-secret-file=" + filepath.Join(dir, "missing")}, 2},
		{"discovery unavailable", []string{"--oidc-client-id=test", "--oidc-issuer-url=" + srv.URL, "--oidc-client-secret-file=" + secret}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "token")
			flag.CommandLine = flag.NewFlagSet("berth-oidc-broker", flag.ContinueOnError)
			os.Args = append([]string{"berth-oidc-broker", "--output=" + output}, tc.args...)
			if got := run(); got != tc.want {
				t.Fatalf("run() = %d, want %d", got, tc.want)
			}
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatalf("failed startup created token output: %v", err)
			}
		})
	}
}
