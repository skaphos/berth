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
// must parse, unsupported syntax must fail rendering, every rendered
// container port must be a valid Kubernetes port, and the
// --metrics-bind-address argument handed to the operator must be a form the
// listener can actually bind (a bare port becomes ":<port>").
func TestChartMetricsBindAddress(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("Helm is required for chart render regression")
	}
	chart := filepath.Join("..", "..", "deploy", "helm", "berth-operator")
	for _, tc := range []struct {
		name       string
		args       []string
		wantPort   int32
		wantAddr   string // expected --metrics-bind-address value
		disabled   bool
		invalidMsg string
	}{
		{name: "default", wantPort: 8080, wantAddr: ":8080"},
		{name: "host-port", args: []string{"--set", "metrics.bindAddress=127.0.0.1:9090"}, wantPort: 9090, wantAddr: "127.0.0.1:9090"},
		{name: "ipv6-any", args: []string{"--set", "metrics.bindAddress=[::]:9091"}, wantPort: 9091, wantAddr: "[::]:9091"},
		{name: "bare-port-string", args: []string{"--set-string", "metrics.bindAddress=9092"}, wantPort: 9092, wantAddr: ":9092"},
		{name: "bare-port-number", args: []string{"--set", "metrics.bindAddress=9093"}, wantPort: 9093, wantAddr: ":9093"},
		{name: "leading-zeros-normalized", args: []string{"--set-string", "metrics.bindAddress=:08080"}, wantPort: 8080, wantAddr: ":8080"},
		{name: "host-leading-zeros-normalized", args: []string{"--set-string", "metrics.bindAddress=127.0.0.1:08080"}, wantPort: 8080, wantAddr: "127.0.0.1:8080"},
		{name: "all-zeros-out-of-range", args: []string{"--set-string", "metrics.bindAddress=:000"}, invalidMsg: "outside 1-65535"},
		{name: "url-rejected", args: []string{"--set", "metrics.bindAddress=http://127.0.0.1:9090"}, invalidMsg: "not a supported bind address"},
		{name: "unbracketed-ipv6-rejected", args: []string{"--set", "metrics.bindAddress=2001:db8::1:9090"}, invalidMsg: "not a supported bind address"},
		{name: "trailing-colon-rejected", args: []string{"--set", "metrics.bindAddress=127.0.0.1:"}, invalidMsg: "not a supported bind address"},
		{name: "empty-brackets-rejected", args: []string{"--set", "metrics.bindAddress=[]:9090"}, invalidMsg: "not a supported bind address"},
		{name: "inner-whitespace-rejected", args: []string{"--set", "metrics.bindAddress=127.0.0.1 :9090"}, invalidMsg: "not a supported bind address"},
		{name: "bracket-whitespace-rejected", args: []string{"--set", "metrics.bindAddress=[: :]:9090"}, invalidMsg: "not a supported bind address"},
		{name: "disabled-number", args: []string{"--set", "metrics.bindAddress=0"}, disabled: true, wantAddr: "0"},
		{name: "disabled-string", args: []string{"--set-string", "metrics.bindAddress=0"}, disabled: true, wantAddr: "0"},
		{name: "empty", args: []string{"--set", "metrics.bindAddress="}, disabled: true, wantAddr: "0"},
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
			c := operatorContainer(t, out)

			var metrics *corev1.ContainerPort
			for i := range c.Ports {
				p := c.Ports[i]
				if p.ContainerPort < 1 || p.ContainerPort > 65535 {
					t.Errorf("port %q = %d is outside 1-65535", p.Name, p.ContainerPort)
				}
				if p.Name == "metrics" {
					metrics = &c.Ports[i]
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

			if got := flagValue(c.Args, "--metrics-bind-address"); got != tc.wantAddr {
				t.Fatalf("--metrics-bind-address = %q, want %q", got, tc.wantAddr)
			}
			// The health probe flag goes through the same normalization.
			if got := flagValue(c.Args, "--health-probe-bind-address"); got != ":8081" {
				t.Fatalf("--health-probe-bind-address = %q, want :8081", got)
			}
		})
	}
}

// TestChartHealthProbeBindAddressNormalized confirms the health-probe flag
// gets the same bare-port normalization as metrics.
func TestChartHealthProbeBindAddressNormalized(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("Helm is required for chart render regression")
	}
	chart := filepath.Join("..", "..", "deploy", "helm", "berth-operator")
	args := []string{"template", "review", chart, "--namespace", "berth-system",
		"--set", "berth.apiServer=https://berth.example:8443", "--set", "clusterID=east",
		"--set", "healthProbe.bindAddress=9099"}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, "helm", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("helm template: %v\n%s", err, out)
	}
	c := operatorContainer(t, out)
	if got := flagValue(c.Args, "--health-probe-bind-address"); got != ":9099" {
		t.Fatalf("--health-probe-bind-address = %q, want :9099", got)
	}
	for _, p := range c.Ports {
		if p.Name == "health" && p.ContainerPort != 9099 {
			t.Fatalf("health containerPort = %d, want 9099", p.ContainerPort)
		}
	}
}

// operatorContainer returns the first container of the operator Deployment
// in a rendered manifest stream.
func operatorContainer(t *testing.T, manifests []byte) corev1.Container {
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
			return d.Spec.Template.Spec.Containers[0]
		}
	}
	t.Fatal("no Deployment with containers in rendered output")
	return corev1.Container{}
}

// flagValue returns the value of a --flag=value argument, or "" if absent.
func flagValue(args []string, flag string) string {
	for _, a := range args {
		if strings.HasPrefix(a, flag+"=") {
			return strings.TrimPrefix(a, flag+"=")
		}
	}
	return ""
}
