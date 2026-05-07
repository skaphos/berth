package main

import (
	"flag"
	"os"

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

func main() {
	os.Exit(run())
}

func run() int {
	var (
		metricsAddr  string
		probeAddr    string
		apiServerURL string
		apiKey       string
		clusterID    string
	)

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "address for the metrics endpoint")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "address for the health probe endpoint")
	flag.StringVar(&apiServerURL, "berth-api-server", "", "Berth API server base URL (required)")
	flag.StringVar(&apiKey, "berth-api-key", "", "Berth API key for bearer authentication")
	flag.StringVar(&clusterID, "cluster-id", "",
		"cluster-distinct identity used as the holder for every Acquire call, overriding "+
			"spec.HolderIdentity on the BerthLease. Required for the cross-cluster singleton "+
			"pattern; leave empty to fall back to spec.HolderIdentity.")
	flag.Parse()

	if apiServerURL == "" {
		ctrl.Log.Error(nil, "--berth-api-server is required")
		return 1
	}

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

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

	leaseClient := client.New(apiServerURL, client.WithAPIKey(apiKey))

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
