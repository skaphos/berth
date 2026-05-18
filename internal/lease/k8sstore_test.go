package lease

import (
	"context"
	"errors"
	"testing"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
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

func TestK8sStorePutCASRequiresMatchingTransitions(t *testing.T) {
	t.Parallel()

	store, _ := newK8sStore(t)
	first := sampleRecord()
	if err := store.Put(context.Background(), 0, first); err != nil {
		t.Fatal(err)
	}

	stale := *first
	stale.FencingToken = 2
	if err := store.Put(context.Background(), 99, &stale); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale CAS err = %v, want ErrConflict", err)
	}

	if err := store.Put(context.Background(), first.FencingToken, &stale); err != nil {
		t.Fatalf("matching CAS: %v", err)
	}

	got, err := store.Get(context.Background(), stale.Key)
	if err != nil {
		t.Fatal(err)
	}
	if got.FencingToken != 2 {
		t.Fatalf("FencingToken = %d, want 2", got.FencingToken)
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

func TestK8sStoreDeleteNotFound(t *testing.T) {
	t.Parallel()

	store, _ := newK8sStore(t)
	if err := store.Delete(context.Background(), Key{Namespace: "x", Name: "y"}, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestK8sStoreDeleteRequiresMatchingTransitions(t *testing.T) {
	t.Parallel()

	store, _ := newK8sStore(t)
	rec := sampleRecord()
	if err := store.Put(context.Background(), 0, rec); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(context.Background(), rec.Key, 99); !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	if err := store.Delete(context.Background(), rec.Key, rec.FencingToken); err != nil {
		t.Fatalf("matching delete: %v", err)
	}
	if _, err := store.Get(context.Background(), rec.Key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete err = %v, want ErrNotFound", err)
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
