package operator

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestChartRejectsRemovedInstallCRDsValue covers #165: the chart's schema
// root allows unknown keys, so merely dropping installCRDs from the schema
// would let a stale override be silently ignored. The key must fail
// validation explicitly, naming itself, whatever value it carries.
func TestChartRejectsRemovedInstallCRDsValue(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("Helm is required for chart render regression")
	}
	chart := filepath.Join("..", "..", "deploy", "helm", "berth-operator")
	for _, value := range []string{"true", "false"} {
		t.Run(value, func(t *testing.T) {
			args := []string{"template", "review", chart, "--namespace", "berth-system",
				"--set", "berth.apiServer=https://berth.example:8443", "--set", "clusterID=east",
				"--set", "installCRDs=" + value}
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			out, err := exec.CommandContext(ctx, "helm", args...).CombinedOutput()
			if err == nil {
				t.Fatalf("installCRDs=%s was accepted; the removed value must fail schema validation", value)
			}
			if !strings.Contains(string(out), "installCRDs") {
				t.Fatalf("validation error does not name installCRDs:\n%s", out)
			}
		})
	}
}
