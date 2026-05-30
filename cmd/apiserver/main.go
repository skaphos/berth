package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/skaphos/berth/internal/api"
	"github.com/skaphos/berth/internal/auth"
	"github.com/skaphos/berth/internal/lease"
	"github.com/skaphos/berth/internal/metrics"
)

const (
	authModeNone        = "none"
	authModeStaticKeys  = "static-keys"
	authModeOIDC        = "oidc"
	exitCodeConfigError = 1

	oidcDiscoveryTimeout = 30 * time.Second
)

func main() {
	os.Exit(run())
}

func run() int {
	var (
		listenAddr        string
		metricsAddr       string
		tlsCert           string
		tlsKey            string
		storeCfg          storeConfig
		authMode          string
		apiKeysFile       string
		oidcIssuerURL     string
		oidcAudience      string
		oidcJWKSURL       string
		oidcUsernameClaim string
		oidcTenantClaim   string
	)
	oidcRequiredClaims := map[string]string{}

	flag.StringVar(&listenAddr, "listen-addr", ":8443", "address to listen on")
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080",
		"address for the unauthenticated Prometheus metrics endpoint (/metrics); empty disables it. "+
			"Served on a separate plain-HTTP port off the TLS/auth path, matching the operator; "+
			"restrict it to the monitoring stack with a NetworkPolicy.")
	flag.StringVar(&tlsCert, "tls-cert-file", "", "path to TLS certificate file")
	flag.StringVar(&tlsKey, "tls-key-file", "", "path to TLS private key file")
	flag.StringVar(&storeCfg.backend, "store-backend", storeBackendUnset,
		"lease store backend: 'mem' (in-memory; dev only), 'k8s' (coordination.k8s.io/v1.Lease in a separate "+
			"cluster), or 'sql' (Postgres/MariaDB/SQLite). When unset, the legacy heuristic applies (empty "+
			"--coordination-namespace → 'mem', set → 'k8s') and a deprecation warning is logged.")
	flag.StringVar(&storeCfg.coordinationKubeconfig, "coordination-kubeconfig", "",
		"path to a kubeconfig pointing at the coordination cluster (empty = in-cluster config). "+
			"Only valid with --store-backend=k8s.")
	flag.StringVar(&storeCfg.coordinationNamespace, "coordination-namespace", "",
		"namespace in the coordination cluster where Berth Lease objects are stored. "+
			"Required when --store-backend=k8s.")
	flag.StringVar(&storeCfg.sqlDriver, "sql-driver", "",
		"SQL driver: 'postgres', 'mysql', or 'sqlite'. Required when --store-backend=sql.")
	flag.StringVar(&storeCfg.sqlDSN, "sql-dsn", "",
		"SQL DSN (e.g. 'postgres://user:pass@host:5432/berth?sslmode=require'). "+
			"Mutually exclusive with --sql-dsn-file. Only valid with --store-backend=sql.")
	flag.StringVar(&storeCfg.sqlDSNFile, "sql-dsn-file", "",
		"path to a file containing the SQL DSN. Read once at startup; restart the API server after "+
			"credential rotation. Mutually exclusive with --sql-dsn. "+
			"Only valid with --store-backend=sql.")
	flag.StringVar(&storeCfg.sqlMigrate, "sql-migrate", "",
		"schema migration policy: 'auto' (apply pending migrations at startup) or 'off' "+
			"(fail fast on schema drift). Only valid with --store-backend=sql. "+
			"Defaults to 'auto' when --store-backend=sql.")
	flag.StringVar(&authMode, "auth-mode", "",
		"authentication mode: 'none', 'static-keys', or 'oidc'. Defaults to 'static-keys' when "+
			"the resolved store backend is 'k8s' or 'sql'; defaults to 'none' for the 'mem' backend.")
	flag.StringVar(&apiKeysFile, "api-keys-file", "",
		"path to a file of '<key-id>:<sha256-hex>' entries; required when --auth-mode=static-keys. "+
			"SIGHUP reloads the file in place.")
	flag.StringVar(&oidcIssuerURL, "oidc-issuer-url", "",
		"OIDC issuer URL (e.g. https://your-org.okta.com/oauth2/default, https://pingfed.example.com); "+
			"required when --auth-mode=oidc")
	flag.StringVar(&oidcAudience, "oidc-audience", "",
		"expected JWT 'aud' claim value; required when --auth-mode=oidc")
	flag.StringVar(&oidcJWKSURL, "oidc-jwks-url", "",
		"override the JWKS URL discovered from the issuer (rarely needed)")
	flag.StringVar(&oidcUsernameClaim, "oidc-username-claim", "",
		"JWT claim copied into the authenticated identity's holder field (default 'sub')")
	flag.StringVar(&oidcTenantClaim, "oidc-tenant-claim", "",
		"JWT claim copied into the authenticated identity's tenant field (default 'sub'); "+
			"array-valued claims use the first element")
	flag.Func("oidc-required-claim",
		"key=value claim that must be present (string or array-of-strings); repeatable",
		func(s string) error {
			parts := strings.SplitN(s, "=", 2)
			if len(parts) != 2 || parts[0] == "" {
				return fmt.Errorf("expected key=value, got %q", s)
			}
			oidcRequiredClaims[parts[0]] = parts[1]
			return nil
		})
	flag.Parse()

	backend, err := resolveStoreBackend(storeCfg)
	if err != nil {
		slog.Error("resolve store backend", "error", err)
		return exitCodeConfigError
	}
	if err := validateStoreConfig(backend, storeCfg); err != nil {
		slog.Error("validate store flags", "error", err)
		return exitCodeConfigError
	}

	authMode = resolveAuthMode(authMode, backend)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	store, err := buildStore(ctx, backend, storeCfg)
	if err != nil {
		slog.Error("build lease store", "error", err)
		return exitCodeConfigError
	}

	var met *metrics.Metrics
	if metricsAddr != "" {
		met = metrics.New()
		store = met.WrapStore(backend, store)
	}
	mgr := lease.NewManager(store)

	authn, err := buildAuthenticator(authMode, apiKeysFile, oidcConfig{
		issuerURL:      oidcIssuerURL,
		audience:       oidcAudience,
		jwksURL:        oidcJWKSURL,
		usernameClaim:  oidcUsernameClaim,
		tenantClaim:    oidcTenantClaim,
		requiredClaims: oidcRequiredClaims,
	})
	if err != nil {
		slog.Error("build authenticator", "error", err)
		return exitCodeConfigError
	}

	go watchSIGHUP(ctx, authn)

	var handler http.Handler = api.NewMux(mgr, authn)
	if met != nil {
		handler = api.MetricsMiddleware(met)(handler)
		go serveMetrics(ctx, met, metricsAddr)
	}
	// Logging is the outermost wrapper: it owns the request correlation id and
	// observes the final status, including auth rejections, for every request.
	handler = api.LoggingMiddleware(slog.Default())(handler)

	srv := api.NewServer(
		api.WithAddress(listenAddr),
		api.WithTLSFiles(tlsCert, tlsKey),
		api.WithHandler(handler),
	)

	if err := srv.Start(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("api server exited", "error", err)
		return 1
	}
	return 0
}

// serveMetrics runs the unauthenticated Prometheus endpoint until ctx is
// canceled. A failure here is logged but never tears down the API server: the
// lease service must keep running even if metrics scraping is unavailable.
func serveMetrics(ctx context.Context, met *metrics.Metrics, addr string) {
	slog.Info("metrics endpoint listening", "addr", addr, "path", "/metrics")
	if err := met.Serve(ctx, addr); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("metrics server exited", "error", err)
	}
}

// resolveAuthMode applies the default-when-unset rule: a persistent store
// backend (k8s or sql) defaults to static-keys; the in-memory dev backend
// defaults to none. An explicit --auth-mode value always wins.
func resolveAuthMode(authMode, backend string) string {
	if authMode != "" {
		return authMode
	}
	if backend == storeBackendMem {
		return authModeNone
	}
	return authModeStaticKeys
}

// oidcConfig groups the OIDC-related flag values for buildAuthenticator.
type oidcConfig struct {
	issuerURL      string
	audience       string
	jwksURL        string
	usernameClaim  string
	tenantClaim    string
	requiredClaims map[string]string
}

// buildAuthenticator returns the [auth.Authenticator] for the chosen
// mode, or nil for the explicit no-auth case (which results in
// unauthenticated lease endpoints — used only for dev). Returns an error
// for unknown modes or missing required configuration.
func buildAuthenticator(mode, keysFile string, oidcCfg oidcConfig) (auth.Authenticator, error) {
	switch mode {
	case authModeNone:
		slog.Warn("running with --auth-mode=none; the API server will accept all lease requests without authentication; do not use in production")
		return nil, nil
	case authModeStaticKeys:
		if keysFile == "" {
			return nil, errors.New("--api-keys-file is required when --auth-mode=static-keys")
		}
		a, err := auth.NewStaticAuthenticatorFromKeysFile(keysFile)
		if err != nil {
			return nil, fmt.Errorf("load static keys: %w", err)
		}
		slog.Info("authenticator configured", "mode", authModeStaticKeys, "keys-file", keysFile)
		return a, nil
	case authModeOIDC:
		if oidcCfg.issuerURL == "" {
			return nil, errors.New("--oidc-issuer-url is required when --auth-mode=oidc")
		}
		if oidcCfg.audience == "" {
			return nil, errors.New("--oidc-audience is required when --auth-mode=oidc")
		}
		ctx, cancel := context.WithTimeout(context.Background(), oidcDiscoveryTimeout)
		defer cancel()
		a, err := auth.NewOIDCAuthenticator(ctx, auth.OIDCConfig{
			IssuerURL:      oidcCfg.issuerURL,
			Audience:       oidcCfg.audience,
			JWKSURL:        oidcCfg.jwksURL,
			UsernameClaim:  oidcCfg.usernameClaim,
			TenantClaim:    oidcCfg.tenantClaim,
			RequiredClaims: oidcCfg.requiredClaims,
		})
		if err != nil {
			return nil, fmt.Errorf("init oidc authenticator: %w", err)
		}
		slog.Info("authenticator configured",
			"mode", authModeOIDC,
			"issuer", oidcCfg.issuerURL,
			"audience", oidcCfg.audience,
			"required-claims", len(oidcCfg.requiredClaims))
		return a, nil
	default:
		return nil, fmt.Errorf("--auth-mode must be %q, %q, or %q; got %q",
			authModeNone, authModeStaticKeys, authModeOIDC, mode)
	}
}

// watchSIGHUP listens for SIGHUP and triggers a reload on authenticators
// that support it (currently [auth.StaticAuthenticator]). Returns when
// ctx is canceled.
func watchSIGHUP(ctx context.Context, authn auth.Authenticator) {
	type reloader interface{ Reload() error }
	r, ok := authn.(reloader)
	if !ok {
		return
	}
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP)
	defer signal.Stop(sigCh)

	for {
		select {
		case <-ctx.Done():
			return
		case <-sigCh:
			if err := r.Reload(); err != nil {
				slog.Error("reload api keys", "error", err)
				continue
			}
			slog.Info("reloaded api keys")
		}
	}
}
