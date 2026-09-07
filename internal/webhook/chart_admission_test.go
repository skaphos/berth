package webhook

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
)

func TestChartRegistersFinalAdmission(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("Helm is required for chart render regression")
	}
	chart := filepath.Join("..", "..", "deploy", "helm", "berth-operator")
	for _, tc := range []struct {
		name      string
		args      []string
		enabled   bool
		manualTLS bool
	}{
		{name: "disabled", args: []string{"--set", "injection.enabled=false"}},
		{name: "cert-manager", enabled: true},
		{name: "existing-cert", enabled: true, manualTLS: true, args: []string{"--set", "injection.webhook.tls.certManager.enabled=false", "--set", "injection.webhook.tls.existingSecret=serving-cert", "--set", "injection.webhook.tls.caBundle=dGVzdA==", "--set", "injection.webhook.servicePort=9444", "--set", "injection.webhook.timeoutSeconds=7", "--set", "injection.webhook.namespaceSelector.matchLabels.team=platform"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"template", "review", chart, "--namespace", "berth-system", "-f", filepath.Join(chart, "ci", "injection-values.yaml")}
			args = append(args, tc.args...)
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			out, err := exec.CommandContext(ctx, "helm", args...).CombinedOutput()
			if err != nil {
				t.Fatalf("helm template: %v\n%s", err, out)
			}
			// Both webhook kinds share the fields validated below.
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
					t.Fatal("disabled injection registered webhooks")
				}
				return
			}
			mut, mok := configs["MutatingWebhookConfiguration"]
			val, vok := configs["ValidatingWebhookConfiguration"]
			if !mok || !vok || len(mut.Webhooks) != 1 || len(val.Webhooks) != 1 {
				t.Fatal("missing admission phase")
			}
			m, v := mut.Webhooks[0], val.Webhooks[0]
			if v.ClientConfig.Service == nil || v.ClientConfig.Service.Path == nil || *v.ClientConfig.Service.Path != "/validate--v1-pod" {
				t.Fatal("validator endpoint not registered")
			}
			if v.FailurePolicy == nil || *v.FailurePolicy != admissionv1.Fail {
				t.Fatal("validator is not fail closed")
			}
			if len(v.Rules) != 1 || !reflect.DeepEqual(v.Rules[0].Operations, []admissionv1.OperationType{admissionv1.Create}) || !reflect.DeepEqual(v.Rules[0].Resources, []string{"pods"}) {
				t.Fatal("validator must gate Pod creation only")
			}
			if !reflect.DeepEqual(m.NamespaceSelector, v.NamespaceSelector) || !reflect.DeepEqual(m.ObjectSelector, v.ObjectSelector) || !reflect.DeepEqual(m.TimeoutSeconds, v.TimeoutSeconds) || !reflect.DeepEqual(m.ClientConfig.CABundle, v.ClientConfig.CABundle) || !reflect.DeepEqual(m.ClientConfig.Service.Port, v.ClientConfig.Service.Port) {
				t.Fatal("admission phases differ in scope, TLS, port or timeout")
			}
			if tc.manualTLS {
				if string(v.ClientConfig.CABundle) != "test" || *v.ClientConfig.Service.Port != 9444 {
					t.Fatal("manual TLS/port overrides lost")
				}
			} else if val.Annotations["cert-manager.io/inject-ca-from"] == "" || val.Annotations["cert-manager.io/inject-ca-from"] != mut.Annotations["cert-manager.io/inject-ca-from"] {
				t.Fatal("validator lacks certificate injection")
			}
		})
	}
}
