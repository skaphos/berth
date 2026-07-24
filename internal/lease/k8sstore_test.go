package lease

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	apimachineryvalidation "k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

const testNamespace = "berth-system"

func newK8sStore(t *testing.T, seed ...*coordinationv1.Lease) (*K8sLeaseStore, kubernetes.Interface) {
	t.Helper()
	client := fake.NewSimpleClientset()
	for _, s := range seed {
		if _, err := client.CoordinationV1().Leases(s.Namespace).Create(context.Background(), s, metav1.CreateOptions{}); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	store, err := NewK8sLeaseStore(client, testNamespace)
	if err != nil {
		t.Fatal(err)
	}
	return store, client
}

func sampleRecord() *Record {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	return &Record{
		Key:          Key{Namespace: "tenant-a", Name: "ingest"},
		Holder:       "cluster-east",
		TTL:          30 * time.Second,
		AcquiredAt:   now,
		RenewedAt:    now,
		FencingToken: 1,
	}
}

func TestNewK8sLeaseStoreValidatesArgs(t *testing.T) {
	t.Parallel()

	if _, err := NewK8sLeaseStore(nil, "ns"); err == nil {
		t.Fatal("expected error for nil client")
	}
	if _, err := NewK8sLeaseStore(fake.NewSimpleClientset(), ""); err == nil {
		t.Fatal("expected error for empty namespace")
	}
}

func TestK8sStoreGetNotFound(t *testing.T) {
	t.Parallel()

	store, _ := newK8sStore(t)
	if _, err := store.Get(context.Background(), Key{Namespace: "x", Name: "y"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestK8sStorePutCreateThenGetRoundTrips(t *testing.T) {
	t.Parallel()

	store, _ := newK8sStore(t)
	rec := sampleRecord()

	if err := store.Put(context.Background(), 0, rec); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := store.Get(context.Background(), rec.Key)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Holder != rec.Holder ||
		got.TTL != rec.TTL ||
		got.FencingToken != rec.FencingToken ||
		got.Key != rec.Key {
		t.Fatalf("round-trip mismatch:\n got = %+v\nwant = %+v", got, rec)
	}
	if !got.AcquiredAt.Equal(rec.AcquiredAt) || !got.RenewedAt.Equal(rec.RenewedAt) {
		t.Fatalf("times mismatch: got acquired=%v renewed=%v want %v / %v",
			got.AcquiredAt, got.RenewedAt, rec.AcquiredAt, rec.RenewedAt)
	}
}

func TestK8sStorePutCreateConflictsWhenPresent(t *testing.T) {
	t.Parallel()

	store, _ := newK8sStore(t)
	rec := sampleRecord()
	if err := store.Put(context.Background(), 0, rec); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(context.Background(), 0, rec); !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
}

func TestK8sStorePutCASRequiresMatchingVersion(t *testing.T) {
	t.Parallel()

	store, _ := newK8sStore(t)
	first := sampleRecord()
	if err := store.Put(context.Background(), 0, first); err != nil {
		t.Fatal(err)
	}

	next := *first
	next.FencingToken = 2
	if err := store.Put(context.Background(), 99, &next); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale CAS err = %v, want ErrConflict", err)
	}

	if err := store.Put(context.Background(), 1, &next); err != nil {
		t.Fatalf("matching CAS: %v", err)
	}

	got, err := store.Get(context.Background(), next.Key)
	if err != nil {
		t.Fatal(err)
	}
	if got.FencingToken != 2 {
		t.Fatalf("FencingToken = %d, want 2", got.FencingToken)
	}
	if got.Version != 2 {
		t.Fatalf("Version after update = %d, want 2", got.Version)
	}
}

// TestK8sStoreLegacyObjectReadsAsVersionOne covers upgrade-in-place: Lease
// objects written before the version annotation existed must read as
// version 1 and accept exactly one CAS write at that version.
func TestK8sStoreLegacyObjectReadsAsVersionOne(t *testing.T) {
	t.Parallel()

	legacy := leaseFromRecord(sampleRecord(), testNamespace, 1)
	delete(legacy.Annotations, versionAnnotation)
	store, _ := newK8sStore(t, legacy)

	got, err := store.Get(context.Background(), sampleRecord().Key)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 1 {
		t.Fatalf("legacy Version = %d, want 1", got.Version)
	}

	upd := *got
	upd.RenewedAt = got.RenewedAt.Add(time.Minute)
	if err := store.Put(context.Background(), 1, &upd); err != nil {
		t.Fatalf("first post-upgrade CAS: %v", err)
	}
	if err := store.Put(context.Background(), 1, &upd); !errors.Is(err, ErrConflict) {
		t.Fatalf("second CAS at version 1 = %v, want ErrConflict", err)
	}
}

func TestK8sStorePutCASOnAbsentReturnsConflict(t *testing.T) {
	t.Parallel()

	store, _ := newK8sStore(t)
	rec := sampleRecord()
	if err := store.Put(context.Background(), 1, rec); !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
}

// TestK8sLeaseNameInjectiveForValidKeys is the property behind the key
// validation rules: over ValidateKey-accepted keys, the "<ns>.<name>"
// encoding can never collide, because a valid namespace contains no dot and
// the first dot is therefore always the separator. The historical collision
// pair ("a","b.c") vs ("a.b","c") is representable only because the second
// key's namespace is now rejected.
func TestK8sLeaseNameInjectiveForValidKeys(t *testing.T) {
	t.Parallel()

	if err := ValidateKey(Key{Namespace: "a.b", Name: "c"}); err == nil {
		t.Fatal("dotted namespace must be rejected by ValidateKey")
	}

	keys := []Key{
		{Namespace: "a", Name: "b.c"},
		{Namespace: "a", Name: "b"},
		{Namespace: "tenant-a", Name: "ingest"},
		{Namespace: "tenant-a", Name: "ingest.shard-1"},
		{Namespace: "tenant", Name: "a.ingest"},
	}
	seen := make(map[string]Key, len(keys))
	for _, k := range keys {
		if err := ValidateKey(k); err != nil {
			t.Fatalf("fixture key %v must be valid: %v", k, err)
		}
		name := k8sLeaseName(k)
		if errs := apimachineryvalidation.IsDNS1123Subdomain(name); len(errs) > 0 {
			t.Fatalf("encoded name %q is not a valid object name: %v", name, errs)
		}
		if prev, dup := seen[name]; dup {
			t.Fatalf("keys %v and %v collide into object name %q", prev, k, name)
		}
		seen[name] = k
	}
}

func TestK8sStoreListFiltersByLabel(t *testing.T) {
	t.Parallel()

	// Pre-seed an unrelated Lease in the same namespace to confirm the
	// label selector excludes it.
	foreign := &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foreign-component-lease",
			Namespace: testNamespace,
			Labels:    map[string]string{"app": "something-else"},
		},
	}
	store, _ := newK8sStore(t, foreign)

	rec := sampleRecord()
	if err := store.Put(context.Background(), 0, rec); err != nil {
		t.Fatal(err)
	}
	rec2 := *rec
	rec2.Key = Key{Namespace: "tenant-b", Name: "egress"}
	if err := store.Put(context.Background(), 0, &rec2); err != nil {
		t.Fatal(err)
	}

	got, err := store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("List len = %d, want 2 (foreign Lease must be filtered out)", len(got))
	}
}

func TestK8sStoreEncodesNameAndAnnotates(t *testing.T) {
	t.Parallel()

	store, client := newK8sStore(t)
	rec := sampleRecord()
	if err := store.Put(context.Background(), 0, rec); err != nil {
		t.Fatal(err)
	}

	persisted, err := client.CoordinationV1().Leases(testNamespace).Get(context.Background(), "tenant-a.ingest", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Annotations[tenantNamespaceAnnotation] != "tenant-a" {
		t.Fatalf("tenant annotation = %q, want tenant-a", persisted.Annotations[tenantNamespaceAnnotation])
	}
	if persisted.Annotations[leaseNameAnnotation] != "ingest" {
		t.Fatalf("lease-name annotation = %q, want ingest", persisted.Annotations[leaseNameAnnotation])
	}
	if persisted.Labels[managedByLabel] != managedByValue {
		t.Fatalf("managed-by label = %q, want %q", persisted.Labels[managedByLabel], managedByValue)
	}
	if persisted.Annotations[versionAnnotation] != "1" {
		t.Fatalf("version annotation = %q, want 1", persisted.Annotations[versionAnnotation])
	}
	if persisted.Spec.HolderIdentity == nil || *persisted.Spec.HolderIdentity != "cluster-east" {
		t.Fatalf("holderIdentity = %v, want cluster-east", persisted.Spec.HolderIdentity)
	}
	if persisted.Spec.LeaseTransitions == nil || *persisted.Spec.LeaseTransitions != 1 {
		t.Fatalf("leaseTransitions = %v, want 1", persisted.Spec.LeaseTransitions)
	}
}

// TestK8sStoreSurvivesAlreadyExistsRace verifies that two concurrent
// creators don't both succeed. The fake clientset's Create is the only
// path that natively enforces uniqueness, so this test exercises the race
// at the Create boundary.
func TestK8sStoreSurvivesAlreadyExistsRace(t *testing.T) {
	t.Parallel()

	store, _ := newK8sStore(t)
	rec := sampleRecord()

	if err := store.Put(context.Background(), 0, rec); err != nil {
		t.Fatal(err)
	}

	// Simulate a second creator. AlreadyExists must surface as ErrConflict.
	err := store.Put(context.Background(), 0, rec)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict (or wrapping AlreadyExists)", err)
	}
	// And of course the underlying error chain must not contain the original
	// kube-apiserver-style error directly — that's an implementation detail.
	if apierrors.IsAlreadyExists(err) {
		t.Fatal("Put must not leak apierrors.AlreadyExists; surface ErrConflict instead")
	}
}

// Compile-time assertion that K8sLeaseStore satisfies Store.
var _ Store = (*K8sLeaseStore)(nil)

// TestK8sStorePing covers the Ping implementation added to support
// readiness probes (SKA-450). It verifies success, error propagation from
// the underlying List, and context cancellation. The reactor-based error
// test also exercises the code path that performs a bounded List (Limit:1).
func TestK8sStorePing(t *testing.T) {
	t.Parallel()

	t.Run("reports reachable", func(t *testing.T) {
		t.Parallel()

		store, _ := newK8sStore(t)
		if err := store.Ping(context.Background()); err != nil {
			t.Fatalf("ping healthy store: %v", err)
		}
	})

	t.Run("propagates list errors", func(t *testing.T) {
		t.Parallel()

		client := fake.NewSimpleClientset()
		client.PrependReactor("list", "leases", func(action k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, fmt.Errorf("simulated apiserver unavailable")
		})
		store, err := NewK8sLeaseStore(client, testNamespace)
		if err != nil {
			t.Fatal(err)
		}

		err = store.Ping(context.Background())
		if err == nil {
			t.Fatal("expected error from Ping")
		}
		if !strings.Contains(err.Error(), "k8s lease store: ping") {
			t.Fatalf("err = %v, want wrapped 'k8s lease store: ping' prefix", err)
		}
		if !strings.Contains(err.Error(), "simulated apiserver unavailable") {
			t.Fatalf("err = %v, want to contain the injected List error", err)
		}
	})

	t.Run("propagates context cancellation (simulated via reactor)", func(t *testing.T) {
		t.Parallel()

		client := fake.NewSimpleClientset()
		client.PrependReactor("list", "leases", func(action k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, context.Canceled
		})
		store, err := NewK8sLeaseStore(client, testNamespace)
		if err != nil {
			t.Fatal(err)
		}

		err = store.Ping(context.Background())
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want wrapped context.Canceled from Ping", err)
		}
		if !strings.Contains(err.Error(), "k8s lease store: ping") {
			t.Fatalf("err = %v, want wrapped with ping prefix", err)
		}
	})
}
