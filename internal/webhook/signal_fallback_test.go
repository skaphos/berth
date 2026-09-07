package webhook

import (
	"context"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/skaphos/berth/internal/acquire"
	corev1 "k8s.io/api/core/v1"
)

func TestRuntimeRequiresExclusiveLiveness(t *testing.T) {
	for _, enforce := range []string{"", "probe", "signal"} {
		for _, kind := range []string{"liveness", "startup"} {
			t.Run(enforce+"/"+kind, func(t *testing.T) {
				pod := optInPod("prod", map[string]string{AnnLeaseName: "test", AnnEnforce: enforce, AnnSignalTarget: "app"})
				probe := &corev1.Probe{ProbeHandler: corev1.ProbeHandler{Exec: &corev1.ExecAction{Command: []string{"true"}}}}
				if kind == "liveness" {
					pod.Spec.Containers[0].LivenessProbe = probe
				} else {
					pod.Spec.Containers[0].StartupProbe = probe
				}
				if err := testInjector().Default(context.Background(), pod); err == nil {
					t.Fatal("runtime accepted a competing probe")
				}
				pod.Annotations[AnnMode] = string(acquire.ModeStartupGate)
				if err := testInjector().Default(context.Background(), pod); err != nil {
					t.Fatalf("startup-gate compatibility: %v", err)
				}
			})
		}
	}
}

func TestFinalAdmissionRequiresFreshnessFallback(t *testing.T) {
	for _, enforce := range []string{"probe", "signal"} {
		for _, change := range []string{"none", "missing", "command", "delay", "termination-grace", "startup", "subpath", "shadow", "writable", "late-container", "volume-source"} {
			t.Run(enforce+"/"+change, func(t *testing.T) {
				inj := testInjector()
				pod := optInPod("prod", map[string]string{AnnLeaseName: "test", AnnEnforce: enforce, AnnSignalTarget: "app"})
				if err := inj.Default(context.Background(), pod); err != nil {
					t.Fatal(err)
				}
				c := &pod.Spec.Containers[0]
				switch change {
				case "volume-source":
					pod.Spec.Volumes[0].VolumeSource = corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{}}
				case "missing":
					c.LivenessProbe = nil
				case "command":
					c.LivenessProbe.Exec.Command = []string{"true"}
				case "delay":
					c.LivenessProbe.InitialDelaySeconds = 10000
				case "termination-grace":
					grace := int64(30)
					c.LivenessProbe.TerminationGracePeriodSeconds = &grace
				case "startup":
					c.StartupProbe = c.LivenessProbe.DeepCopy()
				case "subpath":
					c.VolumeMounts[0].SubPath = "untrusted"
				case "shadow":
					c.VolumeMounts = append(c.VolumeMounts, corev1.VolumeMount{Name: "other", MountPath: "/berth/./check"})
				case "writable":
					c.VolumeMounts[0].ReadOnly = false
				case "late-container":
					pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{Name: "late", Image: "example"})
				}
				_, err := inj.ValidateCreate(context.Background(), pod)
				if change == "none" && err != nil {
					t.Fatalf("valid Pod rejected: %v", err)
				}
				if change != "none" && err == nil {
					t.Fatal("final admission lost mandatory fallback")
				}
			})
		}
	}
}

func TestRuntimeRejectsUnboundedProbeTTL(t *testing.T) {
	ttl := int64(math.MaxInt64)/int64(time.Second) + 1
	inj := testInjector()
	pod := optInPod("prod", map[string]string{AnnLeaseName: "test", AnnTTLSeconds: strconv.FormatInt(ttl, 10)})
	if err := inj.Default(context.Background(), pod); err == nil {
		t.Fatal("overflowing TTL admitted")
	}
	inj.cfg.DefaultTTLSeconds = int(ttl)
	if err := inj.cfg.Validate(); err == nil {
		t.Fatal("overflowing default TTL admitted")
	}
}

func TestSignalDefaultRequiresLiveness(t *testing.T) {
	inj := testInjector()
	inj.cfg.DefaultEnforce = acquire.EnforceSignal
	pod := optInPod("prod", map[string]string{AnnLeaseName: "test", AnnSignalTarget: "app"})
	pod.Spec.Containers[0].StartupProbe = &corev1.Probe{}
	if err := inj.Default(context.Background(), pod); err == nil {
		t.Fatal("default signal bypassed probe requirement")
	}
}

func TestRuntimeRejectsPositiveWrappedTTL(t *testing.T) {
	inj := testInjector()
	pod := optInPod("prod", map[string]string{AnnLeaseName: "test", AnnTTLSeconds: "4294967326"})
	if err := inj.Default(context.Background(), pod); err == nil {
		t.Fatal("API TTL wraps to 30s while fallback waits 136 years")
	}
	inj.cfg.DefaultTTLSeconds = 4294967326
	if err := inj.cfg.Validate(); err == nil {
		t.Fatal("default API TTL wraps to 30s")
	}
}
