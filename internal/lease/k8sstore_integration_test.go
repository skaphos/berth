package lease

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// Integration tests in this file run against a real kube-apiserver, because
// the fake clientset does not validate Lease fields and does not implement
// resourceVersion concurrency. They share one control plane: starting envtest
// per test dominates their runtime. Every test named *APIStorage is gated on
// BERTH_ENVTEST/KUBEBUILDER_ASSETS and is run as a group in CI.

var (
	envOnce   sync.Once
	envShared *envtest.Environment
	envCfg    *rest.Config
	envErr    error
)

// envtestConfig lazily starts the shared control plane, or skips the calling
// test when envtest is not enabled for this run.
func envtestConfig(t *testing.T) *rest.Config {
	t.Helper()
	assets := os.Getenv("KUBEBUILDER_ASSETS")
	if assets == "" && os.Getenv("BERTH_ENVTEST") != "1" {
		t.Skip("set KUBEBUILDER_ASSETS or BERTH_ENVTEST=1 to run real Kubernetes storage tests")
	}
	envOnce.Do(func() {
		envShared = &envtest.Environment{
			BinaryAssetsDirectory:       assets,
			DownloadBinaryAssets:        assets == "",
			DownloadBinaryAssetsVersion: "1.37.0",
		}
		envCfg, envErr = envShared.Start()
	})
	if envErr != nil {
		t.Fatalf("start envtest: %v", envErr)
	}
	return envCfg
}

func TestMain(m *testing.M) {
	code := m.Run()
	if envShared != nil {
		if err := envShared.Stop(); err != nil {
			fmt.Fprintf(os.Stderr, "envtest stop: %v\n", err)
		}
	}
	os.Exit(code)
}

// apiStore binds a store to a namespace of its own on the shared control
// plane so integration tests cannot observe one another's leases.
func apiStore(t *testing.T, cfg *rest.Config, namespace string) (*K8sLeaseStore, kubernetes.Interface) {
	t.Helper()
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
	if _, err := client.CoreV1().Namespaces().Create(t.Context(), ns, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatal(err)
	}
	store, err := NewK8sLeaseStore(client, namespace)
	if err != nil {
		t.Fatal(err)
	}
	return store, client
}

// The fake client does not validate coordination Lease fields. Exercise both
// tombstone writers against an API server so invalid releases cannot pass CI.
func TestK8sTombstonesAPIStorage(t *testing.T) {
	const namespace = "berth-tombstones"
	store, _ := apiStore(t, envtestConfig(t), namespace)
	ctx := t.Context()

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

// TestK8sConcurrentCASAPIStorage covers issue #168. The fake-client tests
// establish sequential version behavior only; the guarantee the k8s backend
// actually rests on is the real apiserver rejecting an update that carries a
// superseded resourceVersion. Both layers are checked here: the raw object
// boundary and the Store.Put contract under concurrency.
func TestK8sConcurrentCASAPIStorage(t *testing.T) {
	const namespace = "berth-concurrent-cas"
	store, client := apiStore(t, envtestConfig(t), namespace)
	ctx := t.Context()

	t.Run("raw resourceVersion is single-use", func(t *testing.T) {
		key := Key{Namespace: "tenant", Name: "raw"}
		rec := sampleRecord()
		rec.Key = key
		if err := store.Put(ctx, 0, rec); err != nil {
			t.Fatal(err)
		}
		leases := client.CoordinationV1().Leases(namespace)
		observed, err := leases.Get(ctx, k8sLeaseName(key), metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}

		// Two writes derived from one read therefore carry one
		// resourceVersion. The apiserver must commit exactly the first.
		first := observed.DeepCopy()
		first.Annotations["example.com/writer"] = "first"
		if _, err := leases.Update(ctx, first, metav1.UpdateOptions{}); err != nil {
			t.Fatalf("first update must commit: %v", err)
		}
		second := observed.DeepCopy()
		second.Annotations["example.com/writer"] = "second"
		_, err = leases.Update(ctx, second, metav1.UpdateOptions{})
		if !apierrors.IsConflict(err) {
			t.Fatalf("second update err = %v, want a 409 Conflict", err)
		}
		got, err := leases.Get(ctx, k8sLeaseName(key), metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if got.Annotations["example.com/writer"] != "first" {
			t.Fatalf("stale write overwrote the committed one: %v", got.Annotations)
		}
	})

	t.Run("concurrent Put grants exactly one writer", func(t *testing.T) {
		key := Key{Namespace: "tenant", Name: "concurrent"}
		rec := sampleRecord()
		rec.Key = key
		if err := store.Put(ctx, 0, rec); err != nil {
			t.Fatal(err)
		}
		observed := readK8sRecord(t, ctx, store, key)

		// Every writer submits against the one version it observed, so at
		// most one can satisfy the compare-and-swap.
		const writers = 4
		var wg sync.WaitGroup
		results := make([]error, writers)
		holders := []string{"west", "north", "south", "central"}
		for i := range writers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				candidate := *rec
				candidate.Holder = holders[i]
				candidate.FencingToken = rec.FencingToken + 1
				results[i] = store.Put(ctx, observed.Version, &candidate)
			}()
		}
		wg.Wait()

		winners := 0
		for i, err := range results {
			switch {
			case err == nil:
				winners++
			case errors.Is(err, ErrConflict):
			default:
				t.Fatalf("writer %d: unexpected error %v", i, err)
			}
		}
		if winners != 1 {
			t.Fatalf("winners = %d, want exactly 1", winners)
		}
		got := readK8sRecord(t, ctx, store, key)
		if got.Version != observed.Version+1 {
			t.Fatalf("version = %d, want %d: a losing write committed", got.Version, observed.Version+1)
		}
	})
}

// interceptFirstPut runs hook exactly once, immediately before the first PUT
// the wrapped client issues. K8sLeaseStore.Put has no seam between its
// internal Get and Update, so this is how a competing write is landed inside
// that window deterministically against a real control plane. Creates are
// POSTs and so pass through untouched.
type interceptFirstPut struct {
	rt   http.RoundTripper
	once sync.Once
	hook func()
}

func (i *interceptFirstPut) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method == http.MethodPut {
		i.once.Do(i.hook)
	}
	return i.rt.RoundTrip(req)
}

// interceptedStore returns a store whose first update races hook.
func interceptedStore(t *testing.T, cfg *rest.Config, namespace string, hook func()) *K8sLeaseStore {
	t.Helper()
	wrapped := rest.CopyConfig(cfg)
	wrapped.Wrap(func(rt http.RoundTripper) http.RoundTripper {
		return &interceptFirstPut{rt: rt, hook: hook}
	})
	client, err := kubernetes.NewForConfig(wrapped)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewK8sLeaseStore(client, namespace)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

// TestK8sMetadataConflictAPIStorage covers issue #121 against a real
// apiserver, and with it the distinction issue #120 depends on. A write that
// moves only Kubernetes metadata must not cost the holder its lease; a write
// that moves the Berth record version must, and the loser must be told who
// actually holds the lease now.
func TestK8sMetadataConflictAPIStorage(t *testing.T) {
	const namespace = "berth-metadata-conflict"
	cfg := envtestConfig(t)
	plain, client := apiStore(t, cfg, namespace)
	ctx := t.Context()
	base := time.Now().UTC().Truncate(time.Second)

	t.Run("metadata-only write does not lose the lease", func(t *testing.T) {
		key := Key{Namespace: "tenant", Name: "metadata-only"}
		patched := make(chan struct{})
		store := interceptedStore(t, cfg, namespace, func() {
			// A policy controller labels the Lease. resourceVersion moves;
			// the Berth version annotation does not.
			_, err := client.CoordinationV1().Leases(namespace).Patch(ctx, k8sLeaseName(key), types.MergePatchType,
				[]byte(`{"metadata":{"labels":{"example.com/policy":"applied"}}}`), metav1.PatchOptions{})
			if err != nil {
				t.Errorf("interleaved metadata patch: %v", err)
			}
			close(patched)
		})
		manager := NewManager(store).WithClock(func() time.Time { return base })

		held, err := manager.Acquire(ctx, key, "east", 30*time.Second)
		if err != nil || !held.Acquired {
			t.Fatalf("acquire: %+v, %v", held, err)
		}
		renewed, err := manager.Renew(ctx, key, "east", held.FencingToken, 30*time.Second)
		if err != nil {
			t.Fatalf("renew: %v", err)
		}
		select {
		case <-patched:
		default:
			t.Fatal("the interleaved patch never ran; the test proves nothing")
		}
		if !renewed.Acquired {
			t.Fatal("holder lost its lease to a metadata-only write (issue #121)")
		}
		if renewed.Holder != "east" || renewed.FencingToken != held.FencingToken {
			t.Fatalf("renewal changed ownership: %+v", renewed)
		}

		got := readK8sRecord(t, ctx, plain, key)
		if got.Version != 2 {
			t.Fatalf("version = %d, want 2 (acquire then renew, no extra write)", got.Version)
		}
		stored, err := client.CoordinationV1().Leases(namespace).Get(ctx, k8sLeaseName(key), metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if stored.Labels["example.com/policy"] != "applied" {
			t.Fatalf("the retry clobbered external metadata: %v", stored.Labels)
		}
	})

	t.Run("real reclaim still takes the lease and is reported", func(t *testing.T) {
		key := Key{Namespace: "tenant", Name: "real-conflict"}
		var reclaimed AcquireResult
		store := interceptedStore(t, cfg, namespace, func() {
			// A standby reclaims the expired lease in the same window. This
			// moves the Berth record version, so the renewal must lose.
			var err error
			reclaimed, err = NewManager(plain).
				WithClock(func() time.Time { return base.Add(time.Hour) }).
				Acquire(ctx, key, "west", 30*time.Second)
			if err != nil || !reclaimed.Acquired {
				t.Errorf("interleaved reclaim: %+v, %v", reclaimed, err)
			}
		})
		manager := NewManager(store).WithClock(func() time.Time { return base })

		held, err := manager.Acquire(ctx, key, "east", 30*time.Second)
		if err != nil || !held.Acquired {
			t.Fatalf("acquire: %+v, %v", held, err)
		}
		lost, err := manager.Renew(ctx, key, "east", held.FencingToken, 30*time.Second)
		if err != nil {
			t.Fatalf("renew: %v", err)
		}
		if lost.Acquired {
			t.Fatal("renewal survived a genuine reclaim: the version predicate was relaxed")
		}
		// Issue #120: the loser is told who won, with real times.
		if lost.Holder != "west" {
			t.Fatalf("holder = %q, want west (the reclaimer)", lost.Holder)
		}
		if lost.FencingToken != reclaimed.FencingToken {
			t.Fatalf("fencing token = %d, want %d", lost.FencingToken, reclaimed.FencingToken)
		}
		if lost.AcquiredAt.IsZero() || lost.ExpiresAt.IsZero() {
			t.Fatalf("lost renewal reported zero times: %+v", lost)
		}
	})
}

func readK8sRecord(t *testing.T, ctx context.Context, store *K8sLeaseStore, key Key) *Record {
	t.Helper()
	rec, err := store.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}
