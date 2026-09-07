package lease

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// The fake client does not validate coordination Lease fields. Exercise both
// tombstone writers against an API server so invalid releases cannot pass CI.
func TestK8sTombstonesAPIStorage(t *testing.T) {
	assets := os.Getenv("KUBEBUILDER_ASSETS")
	if assets == "" && os.Getenv("BERTH_ENVTEST") != "1" {
		t.Skip("set KUBEBUILDER_ASSETS or BERTH_ENVTEST=1 to run real Kubernetes storage tests")
	}
	env := &envtest.Environment{
		BinaryAssetsDirectory:       assets,
		DownloadBinaryAssets:        assets == "",
		DownloadBinaryAssetsVersion: "1.37.0",
	}
	cfg, err := env.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := env.Stop(); err != nil {
			t.Error(err)
		}
	})
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	if _, err := client.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	store, err := NewK8sLeaseStore(client, testNamespace)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("create-tombstone", func(t *testing.T) {
		rec := tombstoneFrom(sampleRecord(), time.Now())
		if err := store.Put(ctx, 0, rec); err != nil {
			t.Fatal(err)
		}
		got := readK8sRecord(t, ctx, store, rec.Key)
		if !got.Tombstone() || got.TTL != 0 || got.FencingToken != rec.FencingToken {
			t.Fatalf("created tombstone corrupted: %+v", got)
		}
	})
	for _, mode := range []string{"release", "gc"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Second)
			manager := NewManager(store).WithClock(func() time.Time { return now })
			key := Key{Namespace: "tenant", Name: mode}
			held, err := manager.Acquire(ctx, key, "east", 30*time.Second)
			if err != nil || !held.Acquired {
				t.Fatalf("acquire: %+v, %v", held, err)
			}
			before := readK8sRecord(t, ctx, store, key)
			if mode == "release" {
				if err := manager.Release(ctx, key, "east", held.FencingToken); err != nil {
					t.Fatalf("release: %v", err)
				}
			} else {
				now = now.Add(2 * time.Minute)
				gc := NewTTLEnforcer(store, time.Second, time.Second)
				gc.now = func() time.Time { return now }
				gc.collect(ctx)
			}
			tombstone := readK8sRecord(t, ctx, store, key)
			if !tombstone.Tombstone() || tombstone.TTL != 0 || tombstone.FencingToken != held.FencingToken || tombstone.Version != before.Version+1 {
				t.Fatalf("invalid tombstone: %+v", tombstone)
			}
			// Repeated release must neither delete history nor consume a version.
			if err := manager.Release(ctx, key, "east", held.FencingToken); err != nil {
				t.Fatal(err)
			}
			if got := readK8sRecord(t, ctx, store, key); got.Version != tombstone.Version {
				t.Fatalf("repeated release changed version: %+v", got)
			}
			// Reclaim at the same instant: no artificial one-second delay.
			next, err := manager.Acquire(ctx, key, "west", 30*time.Second)
			if err != nil || !next.Acquired || next.FencingToken != held.FencingToken+1 {
				t.Fatalf("reacquire: %+v, %v", next, err)
			}
			if err := store.Put(ctx, before.Version, before); !errors.Is(err, ErrConflict) {
				t.Fatalf("stale write accepted: %v", err)
			}
			got := readK8sRecord(t, ctx, store, key)
			if got.Holder != "west" || got.TTL != 30*time.Second || got.FencingToken != next.FencingToken {
				t.Fatalf("reclaimed record corrupted: %+v", got)
			}
		})
	}
}

func readK8sRecord(t *testing.T, ctx context.Context, store *K8sLeaseStore, key Key) *Record {
	t.Helper()
	rec, err := store.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}
