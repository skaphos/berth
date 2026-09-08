package webhook

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
	"k8s.io/apimachinery/pkg/util/yaml"
)

// TestChartPassesHelperPullPolicy is the chart-render regression for #166:
// injection.helper.pullPolicy must reach the operator as
// --injection-helper-image-pull-policy for every allowed value, and the
// schema must reject anything else.
func TestChartPassesHelperPullPolicy(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("Helm is required for chart render regression")
	}
	chart := filepath.Join("..", "..", "deploy", "helm", "berth-operator")
	for _, tc := range []struct {
		name    string
		set     string
		want    string
		invalid bool
	}{
		{name: "default", want: "IfNotPresent"},
		{name: "always", set: "Always", want: "Always"},
		{name: "never", set: "Never", want: "Never"},
		{name: "if-not-present", set: "IfNotPresent", want: "IfNotPresent"},
		{name: "rejected", set: "Sometimes", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"template", "review", chart, "--namespace", "berth-system", "-f", filepath.Join(chart, "ci", "injection-values.yaml")}
			if tc.set != "" {
				args = append(args, "--set", "injection.helper.pullPolicy="+tc.set)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			out, err := exec.CommandContext(ctx, "helm", args...).CombinedOutput()
			if tc.invalid {
				if err == nil {
					t.Fatalf("schema accepted pullPolicy=%q", tc.set)
				}
				return
			}
			if err != nil {
				t.Fatalf("helm template: %v\n%s", err, out)
			}
			dec := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(out), 4096)
			for {
				var d appsv1.Deployment
				err := dec.Decode(&d)
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				if d.Kind != "Deployment" || len(d.Spec.Template.Spec.Containers) == 0 {
					continue
				}
				want := "--injection-helper-image-pull-policy=" + tc.want
				for _, a := range d.Spec.Template.Spec.Containers[0].Args {
					if a == want {
						return
					}
					if strings.HasPrefix(a, "--injection-helper-image-pull-policy=") {
						t.Fatalf("rendered %q, want %q", a, want)
					}
				}
				t.Fatalf("operator args lack %q", want)
			}
			t.Fatal("no operator Deployment in rendered output")
		})
	}
}
