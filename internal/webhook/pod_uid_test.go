package webhook

import (
	"context"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/skaphos/berth/internal/acquire"
	"github.com/skaphos/berth/internal/api"
	"github.com/skaphos/berth/internal/auth"
	"github.com/skaphos/berth/internal/lease"
	"github.com/skaphos/berth/pkg/client"
)

// Resolve the downward API at container start, when the API-assigned UID is
// available. Both Pod incarnations intentionally have the same name.
func injectedPodConfig(t *testing.T, uid string) *acquire.Config {
	t.Helper()
	pod := optInPod("prod", map[string]string{AnnLeaseName: "singleton"})
	if err := testInjector().Default(context.Background(), pod); err != nil {
		t.Fatal(err)
	}
	init := findContainer(pod.Spec.InitContainers, InitContainerName)
	values := envMap(init)
	for _, env := range init.Env {
		if env.ValueFrom == nil || env.ValueFrom.FieldRef == nil {
			continue
		}
		switch env.ValueFrom.FieldRef.FieldPath {
		case "metadata.namespace":
			values[env.Name] = "prod"
		case "metadata.name":
			values[env.Name] = "database-0"
		case "metadata.uid":
			values[env.Name] = uid
		}
	}
	cfg, err := acquire.ConfigFromEnv(func(key string) string { return values[key] })
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestRuntimePodIncarnationsCannotShareEpoch(t *testing.T) {
	old := injectedPodConfig(t, "8c21b044-49ae-4db6-9fe3-530fb06cb5ea")
	replacement := injectedPodConfig(t, "d536c7f2-2f21-476b-8e66-3a7bd260ceca")
	var clock atomic.Int64
	clock.Store(time.Now().Unix())
	mgr := lease.NewManager(lease.NewMemStore()).WithClock(func() time.Time { return time.Unix(clock.Load(), 0) })
	authn := auth.NewStaticAuthenticator(map[string]auth.Identity{"token": {Tenant: old.ClusterID}})
	srv := httptest.NewServer(api.NewMux(mgr, authn, nil))
	defer srv.Close()
	lc := client.New(srv.URL, client.WithAPIKey("token"))
	ctx := context.Background()
	first, err := lc.Acquire(ctx, "prod", "singleton", old.Holder(), old.TTL)
	if err != nil || !first.Acquired {
		t.Fatalf("first acquire: %+v %v", first, err)
	}
	other, err := lc.Acquire(ctx, "prod", "singleton", replacement.Holder(), replacement.TTL)
	if err != nil || other.Acquired {
		t.Fatalf("same-name replacement joined live epoch: %+v %v", other, err)
	}
	if res, err := lc.Renew(ctx, "prod", "singleton", replacement.Holder(), first.FencingToken, old.TTL); err != nil || res.Acquired {
		t.Fatalf("replacement renewed old epoch: %+v %v", res, err)
	}
	same := injectedPodConfig(t, "8c21b044-49ae-4db6-9fe3-530fb06cb5ea")
	renewed, err := lc.Renew(ctx, "prod", "singleton", same.Holder(), first.FencingToken, old.TTL)
	if err != nil || !renewed.Acquired || renewed.FencingToken != first.FencingToken {
		t.Fatalf("same Pod renewal: %+v %v", renewed, err)
	}
	clock.Add(int64(old.TTL/time.Second) + 1)
	next, err := lc.Acquire(ctx, "prod", "singleton", replacement.Holder(), replacement.TTL)
	if err != nil || !next.Acquired || next.FencingToken <= first.FencingToken {
		t.Fatalf("replacement epoch: %+v %v", next, err)
	}
	if res, err := lc.Renew(ctx, "prod", "singleton", old.Holder(), first.FencingToken, old.TTL); err != nil || res.Acquired {
		t.Fatalf("old epoch renewed: %+v %v", res, err)
	}
}

func TestBothHelpersReceivePodUIDFieldRef(t *testing.T) {
	pod := optInPod("prod", map[string]string{AnnLeaseName: "singleton"})
	if err := testInjector().Default(context.Background(), pod); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{InitContainerName, SidecarContainerName} {
		container := findContainer(pod.Spec.InitContainers, name)
		source := findEnvSource(container, "POD_UID")
		if source == nil || source.FieldRef == nil || source.FieldRef.FieldPath != "metadata.uid" {
			t.Fatalf("%s lacks runtime metadata.uid fieldRef: %+v", name, source)
		}
	}
}
