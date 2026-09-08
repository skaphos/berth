package operator

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
)

// TestChartMetricsBindAddress is the chart-render regression for #164: a
// disabled metrics listener must not emit a container port, host:port syntax
// must parse, unsupported syntax must fail rendering, and every rendered
// container port must be a valid Kubernetes port.
func TestChartMetricsBindAddress(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("Helm is required for chart render regression")
	}
	chart := filepath.Join("..", "..", "deploy", "helm", "berth-operator")
	for _, tc := range []struct {
		name       string
		args       []string
		wantPort   int32
		disabled   bool
		invalidMsg string
	}{
		{name: "default", wantPort: 8080},
		{name: "host-port", args: []string{"--set", "metrics.bindAddress=127.0.0.1:9090"}, wantPort: 9090},
		{name: "ipv6-any", args: []string{"--set", "metrics.bindAddress=[::]:9091"}, wantPort: 9091},
		{name: "bare-port-string", args: []string{"--set-string", "metrics.bindAddress=9092"}, wantPort: 9092},
		{name: "bare-port-number", args: []string{"--set", "metrics.bindAddress=9093"}, wantPort: 9093},
		{name: "leading-zeros-normalized", args: []string{"--set-string", "metrics.bindAddress=:08080"}, wantPort: 8080},
		{name: "all-zeros-out-of-range", args: []string{"--set-string", "metrics.bindAddress=:000"}, invalidMsg: "outside 1-65535"},
		{name: "url-rejected", args: []string{"--set", "metrics.bindAddress=http://127.0.0.1:9090"}, invalidMsg: "not a supported bind address"},
		{name: "unbracketed-ipv6-rejected", args: []string{"--set", "metrics.bindAddress=2001:db8::1:9090"}, invalidMsg: "not a supported bind address"},
		{name: "trailing-colon-rejected", args: []string{"--set", "metrics.bindAddress=127.0.0.1:"}, invalidMsg: "not a supported bind address"},
		{name: "empty-brackets-rejected", args: []string{"--set", "metrics.bindAddress=[]:9090"}, invalidMsg: "not a supported bind address"},
		{name: "inner-whitespace-rejected", args: []string{"--set", "metrics.bindAddress=127.0.0.1 :9090"}, invalidMsg: "not a supported bind address"},
		{name: "bracket-whitespace-rejected", args: []string{"--set", "metrics.bindAddress=[: :]:9090"}, invalidMsg: "not a supported bind address"},
		{name: "disabled-number", args: []string{"--set", "metrics.bindAddress=0"}, disabled: true},
		{name: "disabled-string", args: []string{"--set-string", "metrics.bindAddress=0"}, disabled: true},
		{name: "empty", args: []string{"--set", "metrics.bindAddress="}, disabled: true},
		{name: "garbage", args: []string{"--set", "metrics.bindAddress=metrics"}, invalidMsg: "not a supported bind address"},
		{name: "out-of-range", args: []string{"--set", "metrics.bindAddress=:70000"}, invalidMsg: "outside 1-65535"},
		{name: "disabled-with-servicemonitor", args: []string{"--set", "metrics.bindAddress=0", "--set", "metrics.serviceMonitor.enabled=true"}, invalidMsg: "disables the metrics listener"},
		{name: "health-disabled", args: []string{"--set", "healthProbe.bindAddress=0"}, invalidMsg: "healthProbe.bindAddress must be a real listener"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"template", "review", chart, "--namespace", "berth-system",
				"--set", "berth.apiServer=https://berth.example:8443", "--set", "clusterID=east"}
			args = append(args, tc.args...)
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			out, err := exec.CommandContext(ctx, "helm", args...).CombinedOutput()
			if tc.invalidMsg != "" {
				if err == nil || !strings.Contains(string(out), tc.invalidMsg) {
					t.Fatalf("expected render failure containing %q, got err=%v\n%s", tc.invalidMsg, err, out)
				}
				return
			}
			if err != nil {
				t.Fatalf("helm template: %v\n%s", err, out)
			}
			ports := operatorContainerPorts(t, out)
			var metrics *corev1.ContainerPort
			for i := range ports {
				p := ports[i]
				if p.ContainerPort < 1 || p.ContainerPort > 65535 {
					t.Errorf("port %q = %d is outside 1-65535", p.Name, p.ContainerPort)
				}
				if p.Name == "metrics" {
					metrics = &ports[i]
				}
			}
			switch {
			case tc.disabled && metrics != nil:
				t.Fatalf("metrics disabled but container port rendered: %+v", *metrics)
			case !tc.disabled && metrics == nil:
				t.Fatal("metrics container port missing")
			case !tc.disabled && metrics.ContainerPort != tc.wantPort:
				t.Fatalf("metrics containerPort = %d, want %d", metrics.ContainerPort, tc.wantPort)
			}
		})
	}
}

// operatorContainerPorts returns the ports of the first container of the
// operator Deployment in a rendered manifest stream.
func operatorContainerPorts(t *testing.T, manifests []byte) []corev1.ContainerPort {
	t.Helper()
	dec := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(manifests), 4096)
	for {
		var d appsv1.Deployment
		err := dec.Decode(&d)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if d.Kind == "Deployment" && len(d.Spec.Template.Spec.Containers) > 0 {
			return d.Spec.Template.Spec.Containers[0].Ports
		}
	}
	t.Fatal("no Deployment with containers in rendered output")
	return nil
}
