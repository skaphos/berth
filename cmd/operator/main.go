package main

import (
	"flag"
	"os"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	berthv1alpha1 "github.com/skaphos/berth/api/v1alpha1"
	"github.com/skaphos/berth/internal/operator"
	"github.com/skaphos/berth/pkg/client"
)

const tokenFileCacheTTL = time.Second

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
		ts, err := operator.NewFileTokenSource(apiKeyFile, tokenFileCacheTTL)
		if err != nil {
			ctrl.Log.Error(err, "load Berth API key file")
			return 1
		}
		clientOpts = append(clientOpts, client.WithAPIKeyFunc(ts.Get))
	case apiKey != "":
		clientOpts = append(clientOpts, client.WithAPIKey(apiKey))
	}

	tlsCfg, err := operator.LoadTLSConfig(caBundleFile, serverName, insecureSkipVerify)
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

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		ctrl.Log.Error(err, "unable to set up health check")
		return 1
	}

	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		ctrl.Log.Error(err, "unable to set up ready check")
		return 1
	}

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		ctrl.Log.Error(err, "manager exited")
		return 1
	}

	return 0
}
