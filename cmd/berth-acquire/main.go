// Command berth-acquire is the injected lease helper. It is added to
// workload pods by the Berth mutating webhook (SKA-439) and gates the pod
// on a Berth lease:
//
//	berth-acquire acquire   blocking init "hold": acquire then exit 0
//	berth-acquire renew      runtime-singleton sidecar: renew + enforce
//	berth-acquire check PATH liveness probe: exit 0 iff the marker exists
//
// Configuration is taken from flags, which default to BERTH_*/POD_*
// environment variables so the webhook can inject everything via env. See
// docs/design/2026-05-workload-gating-injection-model.md.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/skaphos/berth/internal/acquire"
)

func main() {
	os.Exit(run())
}

func run() int {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	var (
		f              flags
		acquireTimeout time.Duration
	)

	root := &cobra.Command{
		Use:          "berth-acquire",
		Short:        "Berth injected lease helper",
		SilenceUsage: true,
	}
	f.bind(root)

	acquireCmd := &cobra.Command{
		Use:   "acquire",
		Short: "Block until the lease is acquired, then exit (init-container hold)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := f.toConfig()
			if err != nil {
				return err
			}
			lc, err := cfg.NewClient()
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			if acquireTimeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, acquireTimeout)
				defer cancel()
			}
			state := acquire.NewState(cfg.StateDir)
			if _, err := acquire.Hold(ctx, cfg, lc, state, log); err != nil {
				return fmt.Errorf("acquire: %w", err)
			}
			return nil
		},
	}
	acquireCmd.Flags().DurationVar(&acquireTimeout, "acquire-timeout", 0,
		"fail if the lease cannot be acquired within this duration (0 = block forever)")

	renewCmd := &cobra.Command{
		Use:   "renew",
		Short: "Renew the lease and enforce at-most-once (runtime-singleton sidecar)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := f.toConfig()
			if err != nil {
				return err
			}
			if cfg.Mode != acquire.ModeRuntimeSingleton {
				return fmt.Errorf("renew requires mode=%s, got %s", acquire.ModeRuntimeSingleton, cfg.Mode)
			}
			lc, err := cfg.NewClient()
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			state := acquire.NewState(cfg.StateDir)
			return acquire.NewRenewer(cfg, lc, state, log).Run(ctx)
		},
	}

	checkCmd := &cobra.Command{
		Use:   "check [marker-path]",
		Short: "Exit 0 if the health marker exists, 1 otherwise (liveness probe)",
		Args:  cobra.MaximumNArgs(1),
		// The probe runs in the main container, which may be distroless;
		// keep this path free of config/client construction.
		RunE: func(_ *cobra.Command, args []string) error {
			path := args0(args, f.stateDir+"/healthy")
			if _, err := os.Stat(path); err != nil {
				return fmt.Errorf("health marker absent: %w", err)
			}
			return nil
		},
	}

	root.AddCommand(acquireCmd, renewCmd, checkCmd)

	if err := root.Execute(); err != nil {
		log.Error("berth-acquire failed", "error", err)
		return 1
	}
	return 0
}

func args0(args []string, def string) string {
	if len(args) > 0 && args[0] != "" {
		return args[0]
	}
	return def
}

// flags holds the raw flag values. Each defaults to a BERTH_*/POD_*
// environment variable so the webhook can inject configuration via env
// and an operator can still override on the command line.
type flags struct {
	leaseName      string
	leaseNamespace string
	mode           string
	enforce        string
	ttlSeconds     int
	heartbeatSecs  int
	enforceGrace   int
	releaseOnDown  string
	holderIdentity string
	clusterID      string
	podNamespace   string
	podName        string
	workloadKind   string
	workloadName   string
	stateDir       string
	apiServer      string
	apiKey         string
	apiKeyFile     string
	caBundleFile   string
	serverName     string
	insecure       bool
}

func (f *flags) bind(cmd *cobra.Command) {
	pf := cmd.PersistentFlags()
	pf.StringVar(&f.leaseName, "lease-name", os.Getenv("BERTH_LEASE_NAME"), "Berth lease to acquire (required)")
	pf.StringVar(&f.leaseNamespace, "lease-namespace", os.Getenv("BERTH_LEASE_NAMESPACE"), "namespace of the lease (default: pod namespace)")
	pf.StringVar(&f.mode, "mode", os.Getenv("BERTH_MODE"), "startup-gate | runtime-singleton (default runtime-singleton)")
	pf.StringVar(&f.enforce, "enforce", os.Getenv("BERTH_ENFORCE"), "probe | signal (runtime-singleton only; default probe)")
	pf.IntVar(&f.ttlSeconds, "ttl-seconds", envInt("BERTH_TTL_SECONDS", 0), "lease TTL in seconds (required)")
	pf.IntVar(&f.heartbeatSecs, "heartbeat-seconds", envInt("BERTH_HEARTBEAT_SECONDS", 0), "renew interval in seconds (default ttl/3)")
	pf.IntVar(&f.enforceGrace, "enforce-grace-seconds", envInt("BERTH_ENFORCE_GRACE_SECONDS", 0), "signal mode: seconds between SIGTERM and SIGKILL")
	pf.StringVar(&f.releaseOnDown, "release-on-shutdown", os.Getenv("BERTH_RELEASE_ON_SHUTDOWN"), "true | false (default: true for runtime-singleton, false for startup-gate)")
	pf.StringVar(&f.holderIdentity, "holder-identity", os.Getenv("BERTH_HOLDER_IDENTITY"), "explicit holder identity (overrides the mode-specific default)")
	pf.StringVar(&f.clusterID, "cluster-id", os.Getenv("BERTH_CLUSTER_ID"), "cluster-distinct identity folded into the runtime-singleton holder default")
	pf.StringVar(&f.podNamespace, "pod-namespace", os.Getenv("POD_NAMESPACE"), "pod namespace (downward API)")
	pf.StringVar(&f.podName, "pod-name", os.Getenv("POD_NAME"), "pod name (downward API)")
	pf.StringVar(&f.workloadKind, "workload-kind", os.Getenv("BERTH_WORKLOAD_KIND"), "owning workload kind (used in the holder default)")
	pf.StringVar(&f.workloadName, "workload-name", os.Getenv("BERTH_WORKLOAD_NAME"), "owning workload name (used in the holder default)")
	pf.StringVar(&f.stateDir, "state-dir", envOr("BERTH_STATE_DIR", acquire.DefaultStateDir), "shared volume mount for lease state and the health marker")
	pf.StringVar(&f.apiServer, "api-server", os.Getenv("BERTH_API_SERVER"), "Berth API server base URL (required)")
	pf.StringVar(&f.apiKey, "api-key", os.Getenv("BERTH_API_KEY"), "static Berth API bearer token (mutually exclusive with --api-key-file)")
	pf.StringVar(&f.apiKeyFile, "api-key-file", os.Getenv("BERTH_API_KEY_FILE"), "path to a refreshing Berth API bearer token (e.g. an OIDC broker output)")
	pf.StringVar(&f.caBundleFile, "ca-bundle-file", os.Getenv("BERTH_CA_BUNDLE_FILE"), "PEM CA bundle trusted for the API server TLS certificate")
	pf.StringVar(&f.serverName, "server-name", os.Getenv("BERTH_SERVER_NAME"), "override the SNI / TLS server name")
	pf.BoolVar(&f.insecure, "insecure-skip-tls-verify", envBool("BERTH_INSECURE_SKIP_TLS_VERIFY"), "disable API server TLS verification (development only)")
}

func (f *flags) toConfig() (*acquire.Config, error) {
	cfg := &acquire.Config{
		LeaseName:          f.leaseName,
		LeaseNamespace:     f.leaseNamespace,
		Mode:               acquire.Mode(f.mode),
		Enforce:            acquire.Enforce(f.enforce),
		TTL:                time.Duration(f.ttlSeconds) * time.Second,
		HeartbeatInterval:  time.Duration(f.heartbeatSecs) * time.Second,
		EnforceGrace:       time.Duration(f.enforceGrace) * time.Second,
		HolderIdentity:     f.holderIdentity,
		ClusterID:          f.clusterID,
		PodNamespace:       f.podNamespace,
		PodName:            f.podName,
		WorkloadKind:       f.workloadKind,
		WorkloadName:       f.workloadName,
		StateDir:           f.stateDir,
		APIServer:          f.apiServer,
		APIKey:             f.apiKey,
		APIKeyFile:         f.apiKeyFile,
		CABundleFile:       f.caBundleFile,
		ServerName:         f.serverName,
		InsecureSkipVerify: f.insecure,
	}
	switch f.releaseOnDown {
	case "true":
		v := true
		cfg.ReleaseOnShutdown = &v
	case "false":
		v := false
		cfg.ReleaseOnShutdown = &v
	case "":
		// leave nil → mode-specific default
	default:
		return nil, fmt.Errorf("invalid --release-on-shutdown %q (want true or false)", f.releaseOnDown)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envBool(key string) bool {
	v, _ := strconv.ParseBool(os.Getenv(key))
	return v
}
