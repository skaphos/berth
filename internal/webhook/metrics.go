package webhook

import (
	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

// admissionPath labels which admission route a rejection came from. The two
// are worth separating: a rejection on pod creation blocks a workload from
// starting, while one on the ephemeral-containers subresource only refuses a
// debug session on a pod that is running fine.
const (
	admissionPathPods                = "pods"
	admissionPathEphemeralContainers = "pods/ephemeralcontainers"
)

// rejectionsTotal counts admissions refused by the reserved-state-volume
// rule. The synchronous admission error explains a single request to whoever
// submitted it; this is the fleet-wide view, which is what an operator needs
// during and after the upgrade that introduces the rule.
//
// Labels are deliberately limited to reason and admission path. Pod and
// namespace names would be unbounded: a rejected pod is typically recreated
// by its controller in a hot loop, minting a new series per attempt.
var rejectionsTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "berth_webhook_admission_rejections_total",
		Help: "Admissions rejected by the Berth injection webhook, by reason and admission path.",
	},
	[]string{"reason", "path"},
)

func init() {
	ctrlmetrics.Registry.MustRegister(rejectionsTotal)
}

// recordRejection increments the counter for one refused admission.
func recordRejection(reason RejectReason, path string) {
	rejectionsTotal.WithLabelValues(string(reason), path).Inc()
}
