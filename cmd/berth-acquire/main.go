// Command berth-acquire is the injected lease helper. It is added to
// workload pods by the Berth mutating webhook (SKA-439) and gates the pod
// on a Berth lease:
//
//	berth-acquire acquire    blocking init "hold": acquire then exit 0
//	berth-acquire renew       runtime-singleton sidecar: renew + enforce
//	berth-acquire check PATH  liveness probe: exit 0 iff the marker exists
//
// Configuration comes from the BERTH_*/POD_* environment (set by the
// webhook) via acquire.ConfigFromEnv; command-line flags override any
// value when explicitly set, for manual or development runs.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/skaphos/berth/internal/acquire"
)

func main() {
	os.Exit(run(os.Args[1:], os.Getenv, os.Stdout))
}

// run is the testable entrypoint: args without the program name, an
// environment getter, and the stream cobra writes usage/output to.
func run(args []string, getenv func(string) string, stdout io.Writer) int {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	var (
		f              cliFlags
		acquireTimeout time.Duration
	)

	root := &cobra.Command{
		Use:           "berth-acquire",
		Short:         "Berth injected lease helper",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stdout)
	f.bind(root)

	acquireCmd := &cobra.Command{
		Use:   "acquire",
		Short: "Block until the lease is acquired, then exit (init-container hold)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := f.config(cmd, getenv)
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
			cfg, err := f.config(cmd, getenv)
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
			path := markerPath(args, getenv, &f)
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

// markerPath resolves the health-marker path for the check command: an
// explicit argument wins, else <state-dir>/healthy where the state dir
// comes from the flag, then BERTH_STATE_DIR, then the default.
func markerPath(args []string, getenv func(string) string, f *cliFlags) string {
	if len(args) > 0 && args[0] != "" {
		return args[0]
	}
	dir := f.stateDir
	if dir == "" {
		dir = getenv(acquire.EnvStateDir)
	}
	if dir == "" {
		dir = acquire.DefaultStateDir
	}
	return dir + "/healthy"
}

// cliFlags holds the override flags. Each is applied to the env-derived
// config only when explicitly set on the command line.
type cliFlags struct {
	leaseName      string
	leaseNamespace string
	mode           string
	enforce        string
	ttlSeconds     int
	heartbeatSecs  int
	enforceGrace   int
	releaseOnDown  bool
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

func (f *cliFlags) bind(cmd *cobra.Command) {
	pf := cmd.PersistentFlags()
	pf.StringVar(&f.leaseName, "lease-name", "", "Berth lease to acquire (overrides "+acquire.EnvLeaseName+")")
	pf.StringVar(&f.leaseNamespace, "lease-namespace", "", "namespace of the lease (default: pod namespace)")
	pf.StringVar(&f.mode, "mode", "", "startup-gate | runtime-singleton")
	pf.StringVar(&f.enforce, "enforce", "", "probe | signal (runtime-singleton only)")
	pf.IntVar(&f.ttlSeconds, "ttl-seconds", 0, "lease TTL in seconds")
	pf.IntVar(&f.heartbeatSecs, "heartbeat-seconds", 0, "renew interval in seconds (default ttl/3)")
	pf.IntVar(&f.enforceGrace, "enforce-grace-seconds", 0, "signal mode: seconds between SIGTERM and SIGKILL")
	pf.BoolVar(&f.releaseOnDown, "release-on-shutdown", false, "best-effort Release on SIGTERM")
	pf.StringVar(&f.holderIdentity, "holder-identity", "", "explicit holder identity (overrides the mode-specific default)")
	pf.StringVar(&f.clusterID, "cluster-id", "", "cluster-distinct identity folded into the runtime-singleton holder default")
	pf.StringVar(&f.podNamespace, "pod-namespace", "", "pod namespace (downward API)")
	pf.StringVar(&f.podName, "pod-name", "", "pod name (downward API)")
	pf.StringVar(&f.workloadKind, "workload-kind", "", "owning workload kind (used in the holder default)")
	pf.StringVar(&f.workloadName, "workload-name", "", "owning workload name (used in the holder default)")
	pf.StringVar(&f.stateDir, "state-dir", "", "shared volume mount for lease state and the health marker")
	pf.StringVar(&f.apiServer, "api-server", "", "Berth API server base URL")
	pf.StringVar(&f.apiKey, "api-key", "", "static Berth API bearer token (mutually exclusive with --api-key-file)")
	pf.StringVar(&f.apiKeyFile, "api-key-file", "", "path to a refreshing Berth API bearer token (e.g. an OIDC broker output)")
	pf.StringVar(&f.caBundleFile, "ca-bundle-file", "", "PEM CA bundle trusted for the API server TLS certificate")
	pf.StringVar(&f.serverName, "server-name", "", "override the SNI / TLS server name")
	pf.BoolVar(&f.insecure, "insecure-skip-tls-verify", false, "disable API server TLS verification (development only)")
}

// config builds the effective config: the env-derived base with any
// explicitly-set flags layered on top, then validated.
func (f *cliFlags) config(cmd *cobra.Command, getenv func(string) string) (*acquire.Config, error) {
	cfg, err := acquire.ConfigFromEnv(getenv)
	if err != nil {
		return nil, err
	}

	fs := cmd.Flags()
	changed := fs.Changed
	if changed("lease-name") {
		cfg.LeaseName = f.leaseName
	}
	if changed("lease-namespace") {
		cfg.LeaseNamespace = f.leaseNamespace
	}
	if changed("mode") {
		cfg.Mode = acquire.Mode(f.mode)
	}
	if changed("enforce") {
		cfg.Enforce = acquire.Enforce(f.enforce)
	}
	if changed("ttl-seconds") {
		cfg.TTL = time.Duration(f.ttlSeconds) * time.Second
	}
	if changed("heartbeat-seconds") {
		cfg.HeartbeatInterval = time.Duration(f.heartbeatSecs) * time.Second
	}
	if changed("enforce-grace-seconds") {
		cfg.EnforceGrace = time.Duration(f.enforceGrace) * time.Second
	}
	if changed("release-on-shutdown") {
		v := f.releaseOnDown
		cfg.ReleaseOnShutdown = &v
	}
	if changed("holder-identity") {
		cfg.HolderIdentity = f.holderIdentity
	}
	if changed("cluster-id") {
		cfg.ClusterID = f.clusterID
	}
	if changed("pod-namespace") {
		cfg.PodNamespace = f.podNamespace
	}
	if changed("pod-name") {
		cfg.PodName = f.podName
	}
	if changed("workload-kind") {
		cfg.WorkloadKind = f.workloadKind
	}
	if changed("workload-name") {
		cfg.WorkloadName = f.workloadName
	}
	if changed("state-dir") {
		cfg.StateDir = f.stateDir
	}
	if changed("api-server") {
		cfg.APIServer = f.apiServer
	}
	if changed("api-key") {
		cfg.APIKey = f.apiKey
	}
	if changed("api-key-file") {
		cfg.APIKeyFile = f.apiKeyFile
	}
	if changed("ca-bundle-file") {
		cfg.CABundleFile = f.caBundleFile
	}
	if changed("server-name") {
		cfg.ServerName = f.serverName
	}
	if changed("insecure-skip-tls-verify") {
		cfg.InsecureSkipVerify = f.insecure
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}
