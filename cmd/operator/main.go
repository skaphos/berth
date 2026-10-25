package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
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
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	// flag.CommandLine carries controller-runtime's --kubeconfig (registered
	// in its init), so parsing it here keeps that flag working; ctrl.GetConfigOrDie
	// reads the parsed value below.
	cfg, err := parseConfig(flag.CommandLine, os.Args[1:])
	if err != nil {
		ctrl.Log.Error(err, "invalid operator configuration")
		return 1
	}

	if cfg.insecureSkipVerify {
		ctrl.Log.Info("WARNING: --berth-insecure-skip-tls-verify is set; the Berth API server certificate will not be verified")
	}

	if cfg.apiKey != "" {
		ctrl.Log.Info("WARNING: --berth-api-key passes the API token on the command line, where it is " +
			"visible in process listings (ps, /proc/<pid>/cmdline); prefer --berth-api-key-file in production")
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(berthv1alpha1.AddToScheme(scheme))

	// When --metrics-secure is set, serve /metrics over HTTPS (auto self-signed
	// cert) and gate every scrape on Kubernetes authn (TokenReview) + authz
	// (SubjectAccessReview). Off by default: the endpoint stays plain HTTP on a
	// separate port, to be restricted with a NetworkPolicy.
	metricsOpts := metricsserver.Options{BindAddress: cfg.metricsAddr}
	if cfg.secureMetrics {
		metricsOpts.SecureServing = true
		metricsOpts.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                        scheme,
		Metrics:                       metricsOpts,
		HealthProbeBindAddress:        cfg.probeAddr,
		LeaderElection:                cfg.leaderElect,
		LeaderElectionID:              cfg.leaderElectionID,
		LeaderElectionReleaseOnCancel: true,
		LeaseDuration:                 &cfg.leaderElectionLeaseDuration,
		RenewDeadline:                 &cfg.leaderElectionRenewDeadline,
		RetryPeriod:                   &cfg.leaderElectionRetryPeriod,
	})
	if err != nil {
		ctrl.Log.Error(err, "unable to create manager")
		return 1
	}

	clientOpts := []client.Option{}
	switch {
	case cfg.apiKeyFile != "":
		ts, err := clientauth.NewFileTokenSource(cfg.apiKeyFile, tokenFileCacheTTL)
		if err != nil {
			ctrl.Log.Error(err, "load Berth API key file")
			return 1
		}
		clientOpts = append(clientOpts, client.WithAPIKeyFunc(ts.Get))
	case cfg.apiKey != "":
		clientOpts = append(clientOpts, client.WithAPIKey(cfg.apiKey))
	}

	tlsCfg, err := clientauth.LoadTLSConfig(cfg.caBundleFile, cfg.serverName, cfg.insecureSkipVerify)
	if err != nil {
		ctrl.Log.Error(err, "load Berth API server TLS config")
		return 1
	}
	clientOpts = append(clientOpts, client.WithTLSConfig(tlsCfg))

	leaseClient := client.New(cfg.apiServerURL, clientOpts...)

	directClient, err := ctrlclient.New(mgr.GetConfig(), ctrlclient.Options{Scheme: scheme})
	if err != nil {
		ctrl.Log.Error(err, "create direct Kubernetes client")
		return 1
	}
	if cfg.managedWorkloads {
		admissionClient, err := ctrlclient.New(mgr.GetConfig(), ctrlclient.Options{Scheme: scheme})
		if err != nil {
			ctrl.Log.Error(err, "create admission Kubernetes client")
			return 1
		}
		if err := operator.SetupWorkloadAdmission(mgr, admissionClient, cfg.operatorUser); err != nil {
			ctrl.Log.Error(err, "set up required workload admission")
			return 1
		}
	}
	reconciler := &operator.BerthLeaseReconciler{
		Client:            directClient,
		ManagedWorkloads:  cfg.managedWorkloads,
		OperatorNamespace: cfg.operatorNamespace,
		Log:               ctrl.Log.WithName("controllers").WithName("BerthLease"),
		LeaseClient:       leaseClient,
		ClusterIdentity:   cfg.clusterID,
	}
	if err := reconciler.SetupWithManager(mgr); err != nil {
		ctrl.Log.Error(err, "unable to create controller", "controller", "BerthLease")
		return 1
	}

	if cfg.enableWebhook {
		if err := webhook.SetupWithManager(mgr, cfg.injectorConfig); err != nil {
			ctrl.Log.Error(err, "unable to set up injection webhook")
			return 1
		}
		ctrl.Log.Info("berth-acquire injection webhook enabled", "helperImage", cfg.injectorConfig.HelperImage)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		ctrl.Log.Error(err, "unable to set up health check")
		return 1
	}

	// Lease-only readiness checks the central API. Managed mode must keep its
	// admission endpoint and cleanup available during a central API outage, so
	// it checks the local serving endpoint instead.
	if err := mgr.AddReadyzCheck("berth-api", func(req *http.Request) error {
		// Admission and cleanup must stay reachable during central API outages.
		if cfg.managedWorkloads {
			return mgr.GetWebhookServer().StartedChecker()(req)
		}
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

// operatorConfig is the parsed, validated result of the operator's CLI flags.
// Splitting it out of run() lets tests exercise flag validation and
// InjectorConfig assembly without standing up a controller manager (which
// requires a live cluster).
type operatorConfig struct {
	metricsAddr        string
	probeAddr          string
	apiServerURL       string
	apiKey             string
	apiKeyFile         string
	clusterID          string
	caBundleFile       string
	serverName         string
	insecureSkipVerify bool
	enableWebhook      bool
	managedWorkloads   bool
	operatorUser       string
	operatorNamespace  string
	secureMetrics      bool

	leaderElect                 bool
	leaderElectionID            string
	leaderElectionLeaseDuration time.Duration
	leaderElectionRenewDeadline time.Duration
	leaderElectionRetryPeriod   time.Duration

	// injectorConfig is assembled from the injection-* flags and consumed
	// only when enableWebhook is set.
	injectorConfig webhook.InjectorConfig
}

// parseConfig registers the operator's flags on fs, parses args, and validates
// them into an operatorConfig. Production passes flag.CommandLine (so
// controller-runtime's --kubeconfig, registered there, is still parsed) with
// os.Args[1:]; tests pass an isolated FlagSet and explicit args. It mutates no
// global state beyond the flags it registers on fs.
func parseConfig(fs *flag.FlagSet, args []string) (*operatorConfig, error) {
	var (
		metricsAddr        string
		probeAddr          string
		apiServerURL       string
		apiKey             string
		apiKeyFile         string
		clusterID          string
		caBundleFile       string
		enableWebhook      bool
		managedWorkloads   bool
		operatorUser       string
		operatorNamespace  string
		injHelperImage     string
		injPullPolicy      string
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
		secureMetrics      bool

		leaderElect                 bool
		leaderElectionID            string
		leaderElectionLeaseDuration time.Duration
		leaderElectionRenewDeadline time.Duration
		leaderElectionRetryPeriod   time.Duration
	)

	fs.StringVar(&metricsAddr, "metrics-bind-address", ":8080",
		"address for the metrics endpoint (set 0 to disable). Plain HTTP unless --metrics-secure is set.")
	fs.BoolVar(&secureMetrics, "metrics-secure", false,
		"serve /metrics over HTTPS authenticated via Kubernetes TokenReview and authorized via "+
			"SubjectAccessReview. Off by default (plain HTTP on a separate port; restrict it with a "+
			"NetworkPolicy). Enable for an authenticated endpoint; scrapers then need RBAC for /metrics.")
	fs.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "address for the health probe endpoint")
	fs.StringVar(&apiServerURL, "berth-api-server", "", "Berth API server base URL (required)")
	fs.StringVar(&apiKey, "berth-api-key", "",
		"static Berth API bearer token. Mutually exclusive with --berth-api-key-file.")
	fs.StringVar(&apiKeyFile, "berth-api-key-file", "",
		"path to a file containing the Berth API bearer token. Re-read on each request "+
			"(cached briefly), so an external token broker (typically an OIDC sidecar) can "+
			"refresh it without restarting the operator. Mutually exclusive with --berth-api-key.")
	fs.StringVar(&clusterID, "cluster-id", "",
		"cluster-distinct identity used as the holder for every Acquire call, overriding "+
			"spec.HolderIdentity on the BerthLease. Required for the cross-cluster singleton "+
			"pattern; leave empty to fall back to spec.HolderIdentity.")
	fs.StringVar(&caBundleFile, "berth-ca-bundle-file", "",
		"path to a PEM file with extra CA certificates trusted when verifying the Berth API "+
			"server TLS certificate. The bundle is appended to the system trust store, so "+
			"private and public CAs can coexist.")
	fs.StringVar(&serverName, "berth-server-name", "",
		"override the SNI / TLS certificate name used when connecting to the Berth API server. "+
			"Defaults to the host in --berth-api-server.")
	fs.BoolVar(&insecureSkipVerify, "berth-insecure-skip-tls-verify", false,
		"disable Berth API server TLS certificate verification. Development only — never "+
			"set this in production.")
	fs.BoolVar(&managedWorkloads, "enable-workload-admission", false, "enable mandatory fail-closed admission for BerthLease workload targets")
	fs.StringVar(&operatorUser, "workload-operator-user", "", "Kubernetes service-account username permitted to ungate managed Pods")
	fs.StringVar(&operatorNamespace, "workload-operator-namespace", "", "operator namespace excluded from workload management to avoid admission bootstrap deadlock")
	fs.BoolVar(&enableWebhook, "enable-injection-webhook", false,
		"serve the berth-acquire pod-injection mutating webhook from this operator.")
	fs.StringVar(&injHelperImage, "injection-helper-image", "",
		"berth-acquire image injected into opted-in pods. Required when --enable-injection-webhook is set.")
	fs.StringVar(&injPullPolicy, "injection-helper-image-pull-policy", string(corev1.PullIfNotPresent),
		"image pull policy for both injected helper containers: Always, IfNotPresent, or Never.")
	fs.StringVar(&injControlPlaneNS, "injection-control-plane-namespaces", "berth-system",
		"comma-separated namespaces the webhook never mutates (the Berth control plane).")
	fs.StringVar(&injAPIKeyFile, "injection-helper-api-key-file", "",
		"path the injected helper reads its bearer token from inside the workload pod. "+
			"Set together with --injection-helper-api-key-secret.")
	fs.StringVar(&injAPIKeySecret, "injection-helper-api-key-secret", "",
		"name of a Secret in the workload namespace holding the helper's bearer token; "+
			"the webhook mounts it at --injection-helper-api-key-file.")
	fs.StringVar(&injAPIKeySecretKey, "injection-helper-api-key-secret-key", "token",
		"data key within --injection-helper-api-key-secret containing the token.")
	fs.StringVar(&injCABundleFile, "injection-helper-ca-bundle-file", "",
		"path the injected helper reads its API server CA bundle from inside the workload pod. "+
			"Set together with --injection-helper-ca-bundle-configmap.")
	fs.StringVar(&injCABundleCM, "injection-helper-ca-bundle-configmap", "",
		"name of a ConfigMap in the workload namespace holding the CA bundle; "+
			"the webhook mounts it at --injection-helper-ca-bundle-file.")
	fs.StringVar(&injCABundleKey, "injection-helper-ca-bundle-key", "ca.crt",
		"data key within --injection-helper-ca-bundle-configmap containing the CA bundle.")
	fs.StringVar(&injStateDir, "injection-state-dir", acquire.DefaultStateDir,
		"shared volume mount path the injected init container and sidecar use.")
	fs.StringVar(&injDefaultMode, "injection-default-mode", string(acquire.ModeRuntimeSingleton),
		"default berth.skaphos.io/mode for pods that omit the annotation.")
	fs.StringVar(&injDefaultEnforce, "injection-default-enforce", string(acquire.EnforceProbe),
		"default berth.skaphos.io/enforce for pods that omit the annotation.")
	fs.IntVar(&injDefaultTTLSecs, "injection-default-ttl-seconds", 30,
		"default berth.skaphos.io/ttl-seconds for pods that omit the annotation.")
	fs.BoolVar(&leaderElect, "leader-elect", false,
		"enable leader election so only one operator replica reconciles at a time. Leave false for "+
			"the single-replica default; set true when running more than one replica for in-cluster HA.")
	fs.StringVar(&leaderElectionID, "leader-election-id", "berth-operator-leader",
		"name of the coordination.k8s.io/Lease used to elect the active replica. Must be unique per "+
			"operator deployment within its namespace.")
	fs.DurationVar(&leaderElectionLeaseDuration, "leader-election-lease-duration", 15*time.Second,
		"duration non-leader candidates wait before force-acquiring leadership.")
	fs.DurationVar(&leaderElectionRenewDeadline, "leader-election-renew-deadline", 10*time.Second,
		"duration the acting leader retries refreshing its lease before giving up leadership.")
	fs.DurationVar(&leaderElectionRetryPeriod, "leader-election-retry-period", 2*time.Second,
		"interval between leader-election attempts.")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	if apiServerURL == "" {
		return nil, errors.New("--berth-api-server is required")
	}
	// Checked here rather than only in the injection webhook's config: the
	// operator's own lease client uses this URL with the same credentials
	// attached, and it is built whether or not the webhook is enabled. Gating
	// the check on --enable-webhook would leave the operator's own traffic
	// sending its bearer token in cleartext.
	if err := acquire.ValidateAPIServerURL(apiServerURL); err != nil {
		return nil, fmt.Errorf("--berth-api-server: %w", err)
	}
	if apiKey != "" && apiKeyFile != "" {
		return nil, errors.New("--berth-api-key and --berth-api-key-file are mutually exclusive")
	}

	if managedWorkloads {
		parts := strings.Split(operatorUser, ":")
		if len(parts) != 4 || parts[0] != "system" || parts[1] != "serviceaccount" || parts[2] != operatorNamespace || parts[2] == "" || parts[3] == "" {
			return nil, errors.New("workload admission requires its operator namespace and matching service-account username")
		}
	}
	return &operatorConfig{
		metricsAddr:        metricsAddr,
		probeAddr:          probeAddr,
		apiServerURL:       apiServerURL,
		apiKey:             apiKey,
		apiKeyFile:         apiKeyFile,
		clusterID:          clusterID,
		caBundleFile:       caBundleFile,
		serverName:         serverName,
		insecureSkipVerify: insecureSkipVerify,
		enableWebhook:      enableWebhook,
		managedWorkloads:   managedWorkloads,
		operatorUser:       operatorUser,
		operatorNamespace:  operatorNamespace,
		secureMetrics:      secureMetrics,

		leaderElect:                 leaderElect,
		leaderElectionID:            leaderElectionID,
		leaderElectionLeaseDuration: leaderElectionLeaseDuration,
		leaderElectionRenewDeadline: leaderElectionRenewDeadline,
		leaderElectionRetryPeriod:   leaderElectionRetryPeriod,

		injectorConfig: webhook.InjectorConfig{
			HelperImage:     injHelperImage,
			ImagePullPolicy: corev1.PullPolicy(injPullPolicy),
			APIServer:       apiServerURL,
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
		},
	}, nil
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
