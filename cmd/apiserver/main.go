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
	"syscall"

	"github.com/skaphos/berth/internal/api"
	"github.com/skaphos/berth/internal/auth"
	"github.com/skaphos/berth/internal/k8s"
	"github.com/skaphos/berth/internal/lease"
)

const (
	authModeNone        = "none"
	authModeStaticKeys  = "static-keys"
	exitCodeConfigError = 1
)

func main() {
	os.Exit(run())
}

func run() int {
	var (
		listenAddr             string
		tlsCert                string
		tlsKey                 string
		coordinationKubeconfig string
		coordinationNamespace  string
		authMode               string
		apiKeysFile            string
	)

	flag.StringVar(&listenAddr, "listen-addr", ":8443", "address to listen on")
	flag.StringVar(&tlsCert, "tls-cert-file", "", "path to TLS certificate file")
	flag.StringVar(&tlsKey, "tls-key-file", "", "path to TLS private key file")
	flag.StringVar(&coordinationKubeconfig, "coordination-kubeconfig", "",
		"path to a kubeconfig pointing at the coordination cluster (empty = in-cluster config)")
	flag.StringVar(&coordinationNamespace, "coordination-namespace", "",
		"namespace in the coordination cluster where Berth Lease objects are stored. "+
			"When empty, the API server falls back to an in-memory store (dev only — state is lost on restart).")
	flag.StringVar(&authMode, "auth-mode", "",
		"authentication mode: 'none' or 'static-keys'. Defaults to 'static-keys' when "+
			"--coordination-namespace is set; defaults to 'none' otherwise.")
	flag.StringVar(&apiKeysFile, "api-keys-file", "",
		"path to a file of '<key-id>:<sha256-hex>' entries; required when --auth-mode=static-keys. "+
			"SIGHUP reloads the file in place.")
	flag.Parse()

	authMode = resolveAuthMode(authMode, coordinationNamespace)

	store, err := buildStore(coordinationKubeconfig, coordinationNamespace)
	if err != nil {
		slog.Error("build lease store", "error", err)
		return exitCodeConfigError
	}
	mgr := lease.NewManager(store)

	authn, err := buildAuthenticator(authMode, apiKeysFile)
	if err != nil {
		slog.Error("build authenticator", "error", err)
		return exitCodeConfigError
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go watchSIGHUP(ctx, authn)

	srv := api.NewServer(
		api.WithAddress(listenAddr),
		api.WithTLSFiles(tlsCert, tlsKey),
		api.WithHandler(api.NewMux(mgr, authn)),
	)

	if err := srv.Start(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("api server exited", "error", err)
		return 1
	}
	return 0
}

// resolveAuthMode applies the default-when-unset rule: production
// (coordination namespace set) defaults to static-keys; dev defaults to
// none. An explicit --auth-mode value always wins.
func resolveAuthMode(authMode, coordinationNamespace string) string {
	if authMode != "" {
		return authMode
	}
	if coordinationNamespace == "" {
		return authModeNone
	}
	return authModeStaticKeys
}

// buildStore selects the lease.Store backend. When coordinationNamespace
// is non-empty, a K8sLeaseStore is built against the coordination
// cluster. Otherwise an in-memory store is used and a warning is logged.
func buildStore(kubeconfig, namespace string) (lease.Store, error) {
	if namespace == "" {
		slog.Warn("running with in-memory lease store; state will not survive restart, do not use in production")
		return lease.NewMemStore(), nil
	}
	clientset, err := k8s.NewClientset(kubeconfig)
	if err != nil {
		return nil, err
	}
	return lease.NewK8sLeaseStore(clientset, namespace)
}

// buildAuthenticator returns the [auth.Authenticator] for the chosen
// mode, or nil for the explicit no-auth case (which results in
// unauthenticated lease endpoints — used only for dev). Returns an error
// for unknown modes or missing required configuration.
func buildAuthenticator(mode, keysFile string) (auth.Authenticator, error) {
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
	default:
		return nil, fmt.Errorf("--auth-mode must be %q or %q, got %q", authModeNone, authModeStaticKeys, mode)
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
