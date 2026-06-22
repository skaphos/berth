package k8s

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// DefaultQPS and DefaultBurst raise client-go's stock rate limiter (QPS 5 /
// Burst 10), which throttles the k8s lease store rather than any real
// resource: every heartbeat fans renew plus standby contention across all
// leases at once, exceeding Burst=10 at roughly eight leases, after which
// acquires serialize behind the client-side limiter and hit their deadline
// while the API server sits idle. These defaults are sized for the ~400 req/s
// combined write load of the 2,000-lease target, with burst headroom for
// cold-start and failover storms. See SKA-457.
const (
	DefaultQPS   float32 = 100
	DefaultBurst int     = 200
)

// ClientConfig tunes the Kubernetes client built by [NewClientset].
type ClientConfig struct {
	// QPS is the client-go steady-state rate limit in queries/second. A
	// non-positive value falls back to [DefaultQPS].
	QPS float32
	// Burst is the client-go burst budget above QPS. A non-positive value
	// falls back to [DefaultBurst].
	Burst int
}

// NewClientset builds a Kubernetes clientset. When kubeconfig is empty, it
// uses in-cluster configuration; otherwise it reads the specified kubeconfig
// file. The client-go rate limiter is always set from cc (or the raised
// package defaults) so the lease store is never silently capped at client-go's
// stock QPS=5/Burst=10.
func NewClientset(kubeconfig string, cc ClientConfig) (*kubernetes.Clientset, error) {
	cfg, err := buildConfig(kubeconfig, cc)
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(cfg)
}

// buildConfig loads the REST config (in-cluster or from kubeconfig) and applies
// the client-go rate limiter from cc, falling back to the raised package
// defaults when a field is non-positive. Split out from [NewClientset] so the
// rate-limit resolution is unit-testable without a live API server.
func buildConfig(kubeconfig string, cc ClientConfig) (*rest.Config, error) {
	var (
		cfg *rest.Config
		err error
	)

	if kubeconfig == "" {
		cfg, err = rest.InClusterConfig()
	} else {
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	if err != nil {
		return nil, err
	}

	cfg.QPS = DefaultQPS
	if cc.QPS > 0 {
		cfg.QPS = cc.QPS
	}
	cfg.Burst = DefaultBurst
	if cc.Burst > 0 {
		cfg.Burst = cc.Burst
	}

	return cfg, nil
}
