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
	"github.com/skaphos/berth/internal/k8s"
	"github.com/skaphos/berth/internal/lease"
)

func main() {
	var (
		listenAddr             string
		tlsCert                string
		tlsKey                 string
		coordinationKubeconfig string
		coordinationNamespace  string
	)

	flag.StringVar(&listenAddr, "listen-addr", ":8443", "address to listen on")
	flag.StringVar(&tlsCert, "tls-cert-file", "", "path to TLS certificate file")
	flag.StringVar(&tlsKey, "tls-key-file", "", "path to TLS key file")
	flag.StringVar(&coordinationKubeconfig, "coordination-kubeconfig", "",
		"path to a kubeconfig pointing at the coordination cluster (empty = in-cluster config)")
	flag.StringVar(&coordinationNamespace, "coordination-namespace", "",
		"namespace in the coordination cluster where Berth Lease objects are stored. "+
			"When empty, the API server falls back to an in-memory store (dev only — state is lost on restart).")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	store, err := buildStore(coordinationKubeconfig, coordinationNamespace)
	if err != nil {
		slog.Error("build lease store", "error", err)
		os.Exit(1)
	}
	mgr := lease.NewManager(store)

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

// buildStore selects the lease.Store backend. When coordinationNamespace is
// non-empty, a K8sLeaseStore is built against the coordination cluster.
// Otherwise an in-memory store is used and a warning is logged.
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
