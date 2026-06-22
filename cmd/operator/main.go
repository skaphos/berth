package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	berthv1alpha1 "github.com/skaphos/berth/api/v1alpha1"
	"github.com/skaphos/berth/internal/acquire"
	"github.com/skaphos/berth/internal/clientauth"
	"github.com/skaphos/berth/internal/operator"
	"github.com/skaphos/berth/internal/webhook"
	"github.com/skaphos/berth/pkg/client"
)

const tokenFileCacheTTL = time.Second

// readyzPingTimeout bounds the operator's readiness probe of the central
// Berth API so a hung connection cannot block the readyz handler.
const readyzPingTimeout = 5 * time.Second

func main() {
	os.Exit(run())
}

func run() int {
	var (
		metricsAddr        string
		probeAddr          string
		apiServerURL       string
		apiKey             string
		apiKeyFile         string
		clusterID          string
		caBundleFile       string
		enableWebhook      bool
		injHelperImage     string
		injControlPlaneNS  string
		injAPIKeyFile      string
		injAPIKeySecret    string
		injAPIKeySecretKey string
		injCABundleFile    string
		injCABundleCM      string
		injCABundleKey     string
		injStateDir        string
		injDefaultMode     string
		injDefaultEnforce  string
		injDefaultTTLSecs  int
		serverName         string
		insecureSkipVerify bool
	)

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "address for the metrics endpoint")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "address for the health probe endpoint")
	flag.StringVar(&apiServerURL, "berth-api-server", "", "Berth API server base URL (required)")
	flag.StringVar(&apiKey, "berth-api-key", "",
		"static Berth API bearer token. Mutually exclusive with --berth-api-key-file.")
	flag.StringVar(&apiKeyFile, "berth-api-key-file", "",
		"path to a file containing the Berth API bearer token. Re-read on each request "+
			"(cached briefly), so an external token broker (typically an OIDC sidecar) can "+
			"refresh it without restarting the operator. Mutually exclusive with --berth-api-key.")
	flag.StringVar(&clusterID, "cluster-id", "",
		"cluster-distinct identity used as the holder for every Acquire call, overriding "+
			"spec.HolderIdentity on the BerthLease. Required for the cross-cluster singleton "+
			"pattern; leave empty to fall back to spec.HolderIdentity.")
	flag.StringVar(&caBundleFile, "berth-ca-bundle-file", "",
		"path to a PEM file with extra CA certificates trusted when verifying the Berth API "+
			"server TLS certificate. The bundle is appended to the system trust store, so "+
			"private and public CAs can coexist.")
	flag.StringVar(&serverName, "berth-server-name", "",
		"override the SNI / TLS certificate name used when connecting to the Berth API server. "+
			"Defaults to the host in --berth-api-server.")
	flag.BoolVar(&insecureSkipVerify, "berth-insecure-skip-tls-verify", false,
		"disable Berth API server TLS certificate verification. Development only — never "+
			"set this in production.")
	flag.BoolVar(&enableWebhook, "enable-injection-webhook", false,
		"serve the berth-acquire pod-injection mutating webhook from this operator.")
	flag.StringVar(&injHelperImage, "injection-helper-image", "",
		"berth-acquire image injected into opted-in pods. Required when --enable-injection-webhook is set.")
	flag.StringVar(&injControlPlaneNS, "injection-control-plane-namespaces", "berth-system",
		"comma-separated namespaces the webhook never mutates (the Berth control plane).")
	flag.StringVar(&injAPIKeyFile, "injection-helper-api-key-file", "",
		"path the injected helper reads its bearer token from inside the workload pod. "+
			"Set together with --injection-helper-api-key-secret.")
	flag.StringVar(&injAPIKeySecret, "injection-helper-api-key-secret", "",
		"name of a Secret in the workload namespace holding the helper's bearer token; "+
			"the webhook mounts it at --injection-helper-api-key-file.")
	flag.StringVar(&injAPIKeySecretKey, "injection-helper-api-key-secret-key", "token",
		"data key within --injection-helper-api-key-secret containing the token.")
	flag.StringVar(&injCABundleFile, "injection-helper-ca-bundle-file", "",
		"path the injected helper reads its API server CA bundle from inside the workload pod. "+
			"Set together with --injection-helper-ca-bundle-configmap.")
	flag.StringVar(&injCABundleCM, "injection-helper-ca-bundle-configmap", "",
		"name of a ConfigMap in the workload namespace holding the CA bundle; "+
			"the webhook mounts it at --injection-helper-ca-bundle-file.")
	flag.StringVar(&injCABundleKey, "injection-helper-ca-bundle-key", "ca.crt",
		"data key within --injection-helper-ca-bundle-configmap containing the CA bundle.")
	flag.StringVar(&injStateDir, "injection-state-dir", acquire.DefaultStateDir,
		"shared volume mount path the injected init container and sidecar use.")
	flag.StringVar(&injDefaultMode, "injection-default-mode", string(acquire.ModeRuntimeSingleton),
		"default berth.skaphos.io/mode for pods that omit the annotation.")
	flag.StringVar(&injDefaultEnforce, "injection-default-enforce", string(acquire.EnforceProbe),
		"default berth.skaphos.io/enforce for pods that omit the annotation.")
	flag.IntVar(&injDefaultTTLSecs, "injection-default-ttl-seconds", 30,
		"default berth.skaphos.io/ttl-seconds for pods that omit the annotation.")
	flag.Parse()

	if apiServerURL == "" {
		ctrl.Log.Error(nil, "--berth-api-server is required")
		return 1
	}
	if apiKey != "" && apiKeyFile != "" {
		ctrl.Log.Error(nil, "--berth-api-key and --berth-api-key-file are mutually exclusive")
		return 1
	}

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	if insecureSkipVerify {
		ctrl.Log.Info("WARNING: --berth-insecure-skip-tls-verify is set; the Berth API server certificate will not be verified")
	}

	if apiKey != "" {
		ctrl.Log.Info("WARNING: --berth-api-key passes the API token on the command line, where it is " +
			"visible in process listings (ps, /proc/<pid>/cmdline); prefer --berth-api-key-file in production")
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(berthv1alpha1.AddToScheme(scheme))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
	})
	if err != nil {
		ctrl.Log.Error(err, "unable to create manager")
		return 1
	}

	clientOpts := []client.Option{}
	switch {
	case apiKeyFile != "":
		ts, err := clientauth.NewFileTokenSource(apiKeyFile, tokenFileCacheTTL)
		if err != nil {
			ctrl.Log.Error(err, "load Berth API key file")
			return 1
		}
		clientOpts = append(clientOpts, client.WithAPIKeyFunc(ts.Get))
	case apiKey != "":
		clientOpts = append(clientOpts, client.WithAPIKey(apiKey))
	}

	tlsCfg, err := clientauth.LoadTLSConfig(caBundleFile, serverName, insecureSkipVerify)
	if err != nil {
		ctrl.Log.Error(err, "load Berth API server TLS config")
		return 1
	}
	clientOpts = append(clientOpts, client.WithTLSConfig(tlsCfg))

	leaseClient := client.New(apiServerURL, clientOpts...)

	reconciler := &operator.BerthLeaseReconciler{
		Client:          mgr.GetClient(),
		Log:             ctrl.Log.WithName("controllers").WithName("BerthLease"),
		LeaseClient:     leaseClient,
		ClusterIdentity: clusterID,
	}
	if err := reconciler.SetupWithManager(mgr); err != nil {
		ctrl.Log.Error(err, "unable to create controller", "controller", "BerthLease")
		return 1
	}

	if enableWebhook {
		injCfg := webhook.InjectorConfig{
			HelperImage: injHelperImage,
			APIServer:   apiServerURL,
			// Auth/CA file paths and the in-workload-namespace sources the
			// webhook mounts at them. These are the injected helper's own
			// paths, distinct from the operator's --berth-ca-bundle-file.
			APIKeyFile:             injAPIKeyFile,
			APIKeySecretName:       injAPIKeySecret,
			APIKeySecretKey:        injAPIKeySecretKey,
			CABundleFile:           injCABundleFile,
			CABundleConfigMapName:  injCABundleCM,
			CABundleKey:            injCABundleKey,
			ServerName:             serverName,
			ClusterID:              clusterID,
			InsecureSkipVerify:     insecureSkipVerify,
			ControlPlaneNamespaces: splitCSV(injControlPlaneNS),
			DefaultTTLSeconds:      injDefaultTTLSecs,
			DefaultMode:            acquire.Mode(injDefaultMode),
			DefaultEnforce:         acquire.Enforce(injDefaultEnforce),
			StateDir:               injStateDir,
		}
		if err := webhook.SetupWithManager(mgr, injCfg); err != nil {
			ctrl.Log.Error(err, "unable to set up injection webhook")
			return 1
		}
		ctrl.Log.Info("berth-acquire injection webhook enabled", "helperImage", injHelperImage)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		ctrl.Log.Error(err, "unable to set up health check")
		return 1
	}

	// Readiness gates on reachability of the central Berth API: if the
	// operator cannot reach it, it cannot reconcile leases and should be
	// drained rather than reported ready. The probe context bounds a hung
	// connection so the readyz handler cannot block indefinitely.
	if err := mgr.AddReadyzCheck("berth-api", func(req *http.Request) error {
		ctx, cancel := context.WithTimeout(req.Context(), readyzPingTimeout)
		defer cancel()
		return leaseClient.Ping(ctx)
	}); err != nil {
		ctrl.Log.Error(err, "unable to set up ready check")
		return 1
	}

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		ctrl.Log.Error(err, "manager exited")
		return 1
	}

	return 0
}

// splitCSV splits a comma-separated flag value into trimmed, non-empty
// entries.
func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
