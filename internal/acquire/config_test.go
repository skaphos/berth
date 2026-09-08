package acquire

import (
	"strings"
	"testing"
	"time"

	"github.com/skaphos/berth/internal/auth"
	"github.com/skaphos/berth/internal/tenant"
)

func baseConfig() *Config {
	return &Config{
		LeaseName:    "checkout",
		PodNamespace: "prod",
		PodName:      "checkout-7f6c-j4n8x",
		PodUID:       "8c21b044-49ae-4db6-9fe3-530fb06cb5ea",
		TTL:          30 * time.Second,
		APIServer:    "https://berth.example:8443",
	}
}

func TestApplyDefaults(t *testing.T) {
	c := baseConfig()
	c.ApplyDefaults()

	if c.StateDir != DefaultStateDir {
		t.Errorf("StateDir = %q, want %q", c.StateDir, DefaultStateDir)
	}
	if c.Mode != ModeRuntimeSingleton {
		t.Errorf("Mode = %q, want %q", c.Mode, ModeRuntimeSingleton)
	}
	if c.Enforce != EnforceProbe {
		t.Errorf("Enforce = %q, want %q", c.Enforce, EnforceProbe)
	}
	if c.LeaseNamespace != "prod" {
		t.Errorf("LeaseNamespace = %q, want pod namespace prod", c.LeaseNamespace)
	}
	if c.HeartbeatInterval != 10*time.Second {
		t.Errorf("HeartbeatInterval = %s, want ttl/3 = 10s", c.HeartbeatInterval)
	}
	if c.ReleaseOnShutdown == nil || !*c.ReleaseOnShutdown {
		t.Error("ReleaseOnShutdown should default true for runtime-singleton")
	}
}

func TestApplyDefaultsStartupGateReleaseFalse(t *testing.T) {
	c := baseConfig()
	c.Mode = ModeStartupGate
	c.ApplyDefaults()
	if c.ReleaseOnShutdown == nil || *c.ReleaseOnShutdown {
		t.Error("ReleaseOnShutdown should default false for startup-gate")
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr bool
	}{
		{"valid", func(*Config) {}, false},
		{"no lease name", func(c *Config) { c.LeaseName = "" }, true},
		{"no namespace", func(c *Config) { c.PodNamespace = ""; c.LeaseNamespace = "" }, true},
		{"bad mode", func(c *Config) { c.Mode = "weird" }, true},
		{"bad enforce", func(c *Config) { c.Enforce = "nuke" }, true},
		{"zero ttl", func(c *Config) { c.TTL = 0 }, true},
		{"heartbeat >= ttl", func(c *Config) { c.HeartbeatInterval = 30 * time.Second }, true},
		// Issue #114: a heartbeat merely shorter than the ttl is not enough.
		{"heartbeat just under ttl", func(c *Config) { c.HeartbeatInterval = 29 * time.Second }, true},
		{"heartbeat just over half ttl", func(c *Config) { c.HeartbeatInterval = 16 * time.Second }, true},
		{"heartbeat exactly half ttl", func(c *Config) { c.HeartbeatInterval = 15 * time.Second }, false},
		{"negative grace", func(c *Config) { c.EnforceGrace = -time.Second }, true},
		{"no api server", func(c *Config) { c.APIServer = "" }, true},
		// Issue #115: the bearer token rides on every request, so plaintext
		// endpoints are refused outright rather than at first use.
		{"plaintext api server", func(c *Config) { c.APIServer = "http://berth.example:8443" }, true},
		{"schemeless api server", func(c *Config) { c.APIServer = "berth.example:8443" }, true},
		{"non-http scheme api server", func(c *Config) { c.APIServer = "ftp://berth.example" }, true},
		{"api server without host", func(c *Config) { c.APIServer = "https://" }, true},
		{"unparseable api server", func(c *Config) { c.APIServer = "https://berth.example:%zz" }, true},
		{"key and key file", func(c *Config) { c.APIKey = "k"; c.APIKeyFile = "/f" }, true},
		{"signal without target", func(c *Config) { c.Enforce = EnforceSignal }, true},
		{"signal with target", func(c *Config) { c.Enforce = EnforceSignal; c.SignalTarget = "nginx" }, false},
		{"startup-gate signal without target ok", func(c *Config) { c.Mode = ModeStartupGate; c.Enforce = EnforceSignal }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := baseConfig()
			tt.mutate(c)
			err := c.Validate()
			if tt.wantErr != (err != nil) {
				t.Fatalf("Validate() err = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestHolderRuntimeSingletonIncludesPodName(t *testing.T) {
	c := baseConfig()
	c.Mode = ModeRuntimeSingleton
	c.ClusterID = "east"
	c.WorkloadKind = "deployment"
	c.WorkloadName = "checkout"
	c.ApplyDefaults()

	got := c.Holder()
	// The cluster id ("east") is the tenant-owning root, separated from the rest
	// of the hierarchy by "/" so it passes holder authorization under a tenant
	// equal to the cluster id (see TestHolderIsOwnedByClusterTenant).
	want := "east/prod:deployment:checkout:pod:checkout-7f6c-j4n8x:uid:8c21b044-49ae-4db6-9fe3-530fb06cb5ea"
	if got != want {
		t.Errorf("Holder() = %q, want %q", got, want)
	}
}

func TestHolderRuntimeSingletonWithoutClusterStillUnique(t *testing.T) {
	c := baseConfig()
	c.Mode = ModeRuntimeSingleton
	c.ApplyDefaults()
	// No cluster id / workload info, but the pod name must still be present
	// so replicas never share a holder. The namespace becomes the "/"-rooted
	// tenant owner in the cluster-id's absence.
	got := c.Holder()
	want := "prod/pod:checkout-7f6c-j4n8x:uid:8c21b044-49ae-4db6-9fe3-530fb06cb5ea"
	if got != want {
		t.Errorf("Holder() = %q, want %q", got, want)
	}
}

func TestHolderStartupGatePrefersWorkload(t *testing.T) {
	c := baseConfig()
	c.Mode = ModeStartupGate
	c.WorkloadKind = "deployment"
	c.WorkloadName = "checkout"
	c.ApplyDefaults()

	got := c.Holder()
	want := "prod/deployment:checkout"
	if got != want {
		t.Errorf("Holder() = %q, want %q", got, want)
	}
}

func TestHolderStartupGateRootsAtClusterID(t *testing.T) {
	c := baseConfig()
	c.Mode = ModeStartupGate
	c.ClusterID = "east"
	c.WorkloadKind = "deployment"
	c.WorkloadName = "checkout"
	c.ApplyDefaults()

	got := c.Holder()
	// Same tenant root as runtime-singleton (#158), still workload-scoped:
	// no pod name or UID.
	want := "east/prod:deployment:checkout"
	if got != want {
		t.Errorf("Holder() = %q, want %q", got, want)
	}
}

func TestHolderStartupGateFallsBackToPodNameWithClusterID(t *testing.T) {
	c := baseConfig()
	c.Mode = ModeStartupGate
	c.ClusterID = "east"
	c.ApplyDefaults()

	// Unknown workload: the pod name still supplies a non-empty identity
	// even though the cluster id and namespace already fill two segments.
	got := c.Holder()
	want := "east/prod:checkout-7f6c-j4n8x"
	if got != want {
		t.Errorf("Holder() = %q, want %q", got, want)
	}
}

// TestHolderAuthorizedByClusterTenantInBothModes is the regression guard for
// #158: with a cluster-scoped credential (tenant == cluster id) deployed in a
// namespace whose name differs from the cluster id, the derived default holder
// must pass holder authorization in *both* modes. Before the fix startup-gate
// rooted at the namespace, so the same credential that authorized the
// runtime-singleton sidecar was rejected 403 by the startup-gate init container.
func TestHolderAuthorizedByClusterTenantInBothModes(t *testing.T) {
	authz := tenant.NewDefaultAuthorizer()
	clusterScoped := &auth.Identity{Holder: "east-key", Tenant: "east"}
	namespaceScoped := &auth.Identity{Holder: "prod-key", Tenant: "prod"}

	for _, mode := range []Mode{ModeStartupGate, ModeRuntimeSingleton} {
		t.Run(string(mode), func(t *testing.T) {
			c := baseConfig()
			c.Mode = mode
			c.ClusterID = "east"
			c.WorkloadKind = "deployment"
			c.WorkloadName = "checkout"
			c.ApplyDefaults()
			holder := c.Holder()

			if err := authz.AuthorizeHolder(clusterScoped, holder); err != nil {
				t.Fatalf("cluster-scoped tenant rejected holder %q: %v", holder, err)
			}
			if err := authz.AuthorizeHolder(namespaceScoped, holder); err == nil {
				t.Fatalf("namespace-scoped tenant unexpectedly authorized cluster-rooted holder %q", holder)
			}

			// Without a cluster id the namespace is the root, so a
			// namespace-scoped credential authorizes and the cluster one does not.
			c.ClusterID = ""
			holder = c.Holder()
			if err := authz.AuthorizeHolder(namespaceScoped, holder); err != nil {
				t.Fatalf("namespace-scoped tenant rejected holder %q: %v", holder, err)
			}
			if err := authz.AuthorizeHolder(clusterScoped, holder); err == nil {
				t.Fatalf("cluster-scoped tenant unexpectedly authorized namespace-rooted holder %q", holder)
			}
		})
	}
}

// TestHolderIsOwnedByClusterTenant is the regression guard for #91: an injected
// runtime-singleton helper's derived holder must be owned by a tenant equal to
// its cluster id, or the holder-authorization added in SKA-446 rejects every
// acquire with 403 (the failure that kept TestInjectionGating red). Ownership
// is the "<tenant>/" prefix boundary, so the derived holder must begin with
// "<clusterID>/" while remaining a unique per-pod value.
func TestHolderIsOwnedByClusterTenant(t *testing.T) {
	c := baseConfig()
	c.Mode = ModeRuntimeSingleton
	c.ClusterID = "cluster-east"
	c.WorkloadKind = "deployment"
	c.WorkloadName = "checkout"
	c.ApplyDefaults()

	holder := c.Holder()
	tenant := c.ClusterID
	if holder != tenant && !strings.HasPrefix(holder, tenant+"/") {
		t.Fatalf("Holder() = %q is not owned by tenant %q (want %q or %q/...)", holder, tenant, tenant, tenant)
	}
	if !strings.Contains(holder, c.PodName) {
		t.Errorf("Holder() = %q dropped the pod name %q; replicas would share a holder", holder, c.PodName)
	}
}

func TestHolderExplicitOverride(t *testing.T) {
	c := baseConfig()
	c.HolderIdentity = "my-custom-holder"
	c.ApplyDefaults()
	if got := c.Holder(); got != "my-custom-holder" {
		t.Errorf("Holder() = %q, want explicit override", got)
	}
}

func TestNewClientBuilds(t *testing.T) {
	c := baseConfig()
	if err := c.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	lc, err := c.NewClient()
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if lc == nil {
		t.Fatal("NewClient returned nil client")
	}
}

func TestNewClientWithAPIKeyFileMissing(t *testing.T) {
	c := baseConfig()
	c.APIKeyFile = "/nonexistent/token"
	if _, err := c.NewClient(); err == nil {
		t.Error("expected error for missing api key file")
	}
}

func TestRuntimePodUIDValidation(t *testing.T) {
	for _, uid := range []string{"", "other/uid", "other:uid", "uid ", " uid", "uid\n", "\u00a0uid"} {
		c := baseConfig()
		c.PodUID = uid
		if err := c.Validate(); err == nil {
			t.Fatalf("invalid UID %q accepted in default runtime mode", uid)
		}
	}
	for _, mode := range []Mode{ModeStartupGate, ModeRuntimeSingleton} {
		c := baseConfig()
		c.Mode, c.PodUID = mode, ""
		if mode == ModeRuntimeSingleton {
			c.HolderIdentity = "explicit-holder"
		}
		if err := c.Validate(); err != nil {
			t.Fatalf("mode/override compatibility: %v", err)
		}
	}
}

func TestRuntimeHolderByteLimit(t *testing.T) {
	for _, size := range []int{253, 254} {
		c := baseConfig()
		c.ApplyDefaults()
		// Multibyte names must count bytes, and the UID must remain complete.
		c.WorkloadName = "é"
		c.WorkloadName += strings.Repeat("a", size-len(c.Holder()))
		if len(c.Holder()) != size {
			t.Fatal("invalid boundary fixture")
		}
		if err := c.Validate(); (err != nil) != (size > 253) {
			t.Fatalf("%d-byte holder: %v", size, err)
		}
		if !strings.HasSuffix(c.Holder(), ":uid:"+c.PodUID) {
			t.Fatal("UID was truncated")
		}
	}
}

func TestStartupHolderIgnoresPodIncarnation(t *testing.T) {
	c := baseConfig()
	c.Mode = ModeStartupGate
	first := c.Holder()
	c.PodUID = "replacement-uid"
	if c.Holder() != first {
		t.Fatal("startup-gate holder changed across Pod UIDs")
	}
}
