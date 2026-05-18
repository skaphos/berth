// SPDX-FileCopyrightText: 2026 Skaphos
// SPDX-License-Identifier: MIT

//go:build e2e

// Package e2e exercises the cross-cluster Berth singleton against three
// real kind clusters. The harness (test/e2e/fixtures/up.sh) creates the
// clusters and installs the charts; these tests assume the topology is
// already up. Run via `go -C tools tool task e2e`.
package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	berthv1alpha1 "github.com/skaphos/berth/api/v1alpha1"
)

const (
	contextCoord = "kind-berth-e2e-coord"
	contextEast  = "kind-berth-e2e-east"
	contextWest  = "kind-berth-e2e-west"

	namespace = "berth-system"
)

// clusters carries one controller-runtime client per kind cluster.
// Populated by TestMain; nil if the harness env file is missing.
var clusters struct {
	coord ctrlclient.Client
	east  ctrlclient.Client
	west  ctrlclient.Client
}

// scheme registers the types every test uses. Core types come from
// clientgoscheme; BerthLease from the project's CRD package.
var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(berthv1alpha1.AddToScheme(scheme))
}

func TestMain(m *testing.M) {
	envPath := filepath.Join(repoRoot(), ".tmp", "e2e", "env")
	if _, err := os.Stat(envPath); err != nil {
		fmt.Fprintf(os.Stderr, "e2e harness not up (no %s) — run `go -C tools tool task e2e-up` first\n", envPath)
		os.Exit(1)
	}

	var err error
	clusters.coord, err = buildClient(contextCoord)
	if err != nil {
		fmt.Fprintf(os.Stderr, "build coord client: %v\n", err)
		os.Exit(1)
	}
	clusters.east, err = buildClient(contextEast)
	if err != nil {
		fmt.Fprintf(os.Stderr, "build east client: %v\n", err)
		os.Exit(1)
	}
	clusters.west, err = buildClient(contextWest)
	if err != nil {
		fmt.Fprintf(os.Stderr, "build west client: %v\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

func buildClient(ctxName string) (ctrlclient.Client, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		&clientcmd.ConfigOverrides{CurrentContext: ctxName},
	).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig context %q: %w", ctxName, err)
	}
	return ctrlclient.New(cfg, ctrlclient.Options{Scheme: scheme})
}

// repoRoot walks up from the test file location to the repo root. The
// .tmp/e2e/env file is anchored there.
func repoRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	// test/e2e -> repo root is two levels up
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

// waitFor polls cond every 2s until it returns true or the deadline
// elapses. The failure message is whatever cond returned on its final
// attempt — propagate detail from the caller.
func waitFor(t *testing.T, timeout time.Duration, what string, cond func(context.Context) (bool, string)) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	deadline := time.Now().Add(timeout)
	var lastDetail string
	for {
		ok, detail := cond(ctx)
		if ok {
			return
		}
		lastDetail = detail
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s after %s: %s", what, timeout, strings.TrimSpace(lastDetail))
		}
		select {
		case <-ctx.Done():
			t.Fatalf("context cancelled waiting for %s: %s", what, strings.TrimSpace(lastDetail))
		case <-time.After(2 * time.Second):
		}
	}
}
