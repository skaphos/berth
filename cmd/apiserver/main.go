package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/skaphos/berth/internal/api"
	"github.com/skaphos/berth/internal/lease"
)

func main() {
	var (
		listenAddr string
		tlsCert    string
		tlsKey     string
		kubeconfig string
	)

	flag.StringVar(&listenAddr, "listen-addr", ":8443", "address to listen on")
	flag.StringVar(&tlsCert, "tls-cert-file", "", "path to TLS certificate file")
	flag.StringVar(&tlsKey, "tls-key-file", "", "path to TLS key file")
	flag.StringVar(&kubeconfig, "kubeconfig", "", "path to kubeconfig file")
	flag.Parse()

	_ = kubeconfig // Reserved for Phase 1 Kubernetes client wiring.

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	mgr := lease.NewManager(lease.NewMemStore())

	srv := api.NewServer(
		api.WithAddress(listenAddr),
		api.WithTLSFiles(tlsCert, tlsKey),
		api.WithHandler(api.NewMux(mgr)),
	)

	if err := srv.Start(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("api server exited", "error", err)
		os.Exit(1)
	}
}
