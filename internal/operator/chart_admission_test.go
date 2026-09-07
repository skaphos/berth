package operator

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
)

func TestChartRequiresManagedAdmission(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("Helm is required for chart render regression")
	}
	chart := filepath.Join("..", "..", "deploy", "helm", "berth-operator")
	for _, tc := range []struct {
		name                          string
		args                          []string
		enabled, invalid, certManager bool
	}{
		{name: "lease-only"},
		{name: "missing-tls", enabled: true, invalid: true},
		{name: "manual-tls", enabled: true, args: []string{"--set", "injection.webhook.tls.existingSecret=serving-cert", "--set", "injection.webhook.tls.caBundle=dGVzdA=="}},
		{name: "cert-manager", enabled: true, certManager: true, args: []string{"--set", "injection.webhook.tls.certManager.enabled=true", "--set", "injection.webhook.tls.certManager.issuerRef.name=issuer"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"template", "review", chart, "--namespace", "berth-system", "-f", filepath.Join(chart, "ci", "injection-values.yaml"), "--set", "injection.enabled=false", "--set", "injection.webhook.tls.certManager.enabled=false"}
			if tc.enabled {
				args = append(args, "--set", "workloadManagement.enabled=true")
			}
			args = append(args, tc.args...)
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			out, err := exec.CommandContext(ctx, "helm", args...).CombinedOutput()
			if tc.invalid {
				if err == nil || !strings.Contains(string(out), "serving certificate") {
					t.Fatalf("missing required TLS did not fail rendering: %v\n%s", err, out)
				}
				return
			}
			if err != nil {
				t.Fatalf("helm template: %v\n%s", err, out)
			}
			configs := map[string]admissionv1.ValidatingWebhookConfiguration{}
			dec := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(out), 4096)
			for {
				var cfg admissionv1.ValidatingWebhookConfiguration
				err := dec.Decode(&cfg)
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				if cfg.Kind == "MutatingWebhookConfiguration" || cfg.Kind == "ValidatingWebhookConfiguration" {
					configs[cfg.Kind] = cfg
				}
			}
			if !tc.enabled {
				if len(configs) != 0 {
					t.Fatal("lease-only mode installed admission")
				}
				return
			}
			if len(configs) != 2 || !strings.Contains(string(out), "--enable-workload-admission") || !strings.Contains(string(out), "kind: Service") {
				t.Fatal("managed mode requires both admission phases, service and operator flag without helper injection")
			}
			for kind, cfg := range configs {
				if len(cfg.Webhooks) != 1 {
					t.Fatal("unexpected admission scope")
				}
				w := cfg.Webhooks[0]
				selector := &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "kubernetes.io/metadata.name", Operator: metav1.LabelSelectorOpNotIn, Values: []string{"berth-system"}}}}
				if w.FailurePolicy == nil || *w.FailurePolicy != admissionv1.Fail || w.ObjectSelector != nil || !reflect.DeepEqual(w.NamespaceSelector, selector) {
					t.Fatal("managed admission can be bypassed or prevents operator namespace recovery")
				}
				if tc.certManager {
					if cfg.Annotations["cert-manager.io/inject-ca-from"] == "" {
						t.Fatal("CA injection missing")
					}
				} else if string(w.ClientConfig.CABundle) != "test" {
					t.Fatal("manual CA bundle missing")
				}
				covered := map[string][]admissionv1.OperationType{}
				for _, rule := range w.Rules {
					for _, resource := range rule.Resources {
						covered[resource] = rule.Operations
					}
				}
				ops := []admissionv1.OperationType{admissionv1.Create}
				if kind == "ValidatingWebhookConfiguration" {
					ops = append(ops, admissionv1.Update)
					if !reflect.DeepEqual(covered["pods/binding"], []admissionv1.OperationType{admissionv1.Create}) {
						t.Fatal("scheduler binding validation missing")
					}
				}
				for _, resource := range []string{"deployments", "statefulsets", "replicasets", "cronjobs", "jobs", "pods"} {
					if !reflect.DeepEqual(covered[resource], ops) {
						t.Fatalf("%s does not cover %s operations", kind, resource)
					}
				}
			}
		})
	}
}
