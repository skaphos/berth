package lease

import (
	"context"
	"errors"
	"testing"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

var leaseGVR = coordinationv1.SchemeGroupVersion.WithResource("leases")

// seedForConflict creates the sample record at version 1 and returns the
// store, the fake clientset for reactor installation, and the record.
func seedForConflict(t *testing.T) (*K8sLeaseStore, *fake.Clientset, *Record) {
	t.Helper()
	store, client := newK8sStore(t)
	rec := sampleRecord()
	if err := store.Put(context.Background(), 0, rec); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return store, client.(*fake.Clientset), rec
}

// renewalOf returns rec advanced by one renewal, the write a holder makes to
// extend its own lease without changing ownership.
func renewalOf(rec *Record) *Record {
	next := *rec
	next.RenewedAt = rec.RenewedAt.Add(10 * time.Second)
	return &next
}

// TestK8sStorePutRetriesMetadataOnlyConflict covers issue #121. A writer that
// touches only metadata Berth does not own moves the Kubernetes
// resourceVersion without moving the Berth record version. That used to fail
// the holder's own renewal with ErrConflict, which Manager.Renew reads as
// lease loss — an unnecessary step-down. Put now re-reads, confirms the record
// version is untouched, and completes the write.
func TestK8sStorePutRetriesMetadataOnlyConflict(t *testing.T) {
	t.Parallel()

	store, client, rec := seedForConflict(t)
	name := k8sLeaseName(rec.Key)

	attempts := 0
	client.PrependReactor("update", "leases", func(k8stesting.Action) (bool, runtime.Object, error) {
		attempts++
		if attempts > 1 {
			return false, nil, nil // let the retry reach the tracker
		}
		// A policy controller labels the object between our Get and Update.
		obj, err := client.Tracker().Get(leaseGVR, testNamespace, name)
		if err != nil {
			return true, nil, err
		}
		l := obj.(*coordinationv1.Lease)
		l.Labels["example.com/policy"] = "applied"
		if err := client.Tracker().Update(leaseGVR, l, testNamespace); err != nil {
			return true, nil, err
		}
		return true, nil, apierrors.NewConflict(leaseGVR.GroupResource(), name, errors.New("object was modified"))
	})

	if err := store.Put(context.Background(), 1, renewalOf(rec)); err != nil {
		t.Fatalf("metadata-only conflict must not fail the write: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("update attempts = %d, want 2 (one conflict, one retry)", attempts)
	}

	got, err := store.Get(context.Background(), rec.Key)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 2 {
		t.Fatalf("version = %d, want 2 (exactly one write committed)", got.Version)
	}
	if got.Holder != rec.Holder || got.FencingToken != rec.FencingToken {
		t.Fatalf("retry changed ownership: %+v", got)
	}

	// The interleaved writer's metadata must survive: the retry re-applies
	// onto the freshly read object rather than the one it read first.
	stored, err := client.CoordinationV1().Leases(testNamespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if stored.Labels["example.com/policy"] != "applied" {
		t.Fatalf("retry clobbered external metadata: %v", stored.Labels)
	}
}

// TestK8sStorePutDoesNotRetryRealVersionChange is the safety half of #121: a
// conflict caused by an actual Berth write must still lose. The retry loop
// revalidates the expected version on every pass and never relaxes the
// predicate, so a reclaim or competing renewal is reported as ErrConflict.
func TestK8sStorePutDoesNotRetryRealVersionChange(t *testing.T) {
	t.Parallel()

	store, client, rec := seedForConflict(t)
	name := k8sLeaseName(rec.Key)

	attempts := 0
	client.PrependReactor("update", "leases", func(k8stesting.Action) (bool, runtime.Object, error) {
		attempts++
		obj, err := client.Tracker().Get(leaseGVR, testNamespace, name)
		if err != nil {
			return true, nil, err
		}
		l := obj.(*coordinationv1.Lease)
		// Another holder's write commits: the record version moves too.
		l.Annotations[versionAnnotation] = "2"
		holder := "cluster-west"
		l.Spec.HolderIdentity = &holder
		if err := client.Tracker().Update(leaseGVR, l, testNamespace); err != nil {
			return true, nil, err
		}
		return true, nil, apierrors.NewConflict(leaseGVR.GroupResource(), name, errors.New("object was modified"))
	})

	err := store.Put(context.Background(), 1, renewalOf(rec))
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict for a genuine record-version change", err)
	}
	if attempts != 1 {
		t.Fatalf("update attempts = %d, want 1; the re-read must abort on a version change", attempts)
	}
	got, err := store.Get(context.Background(), rec.Key)
	if err != nil {
		t.Fatal(err)
	}
	if got.Holder != "cluster-west" {
		t.Fatalf("stale write overwrote the winner: %+v", got)
	}
}

// TestK8sStorePutConflictRetryBudgetIsBounded verifies the loop terminates:
// an object under permanent contention yields ErrConflict after a fixed
// number of attempts rather than spinning inside one Put.
func TestK8sStorePutConflictRetryBudgetIsBounded(t *testing.T) {
	t.Parallel()

	store, client, rec := seedForConflict(t)
	name := k8sLeaseName(rec.Key)

	attempts := 0
	client.PrependReactor("update", "leases", func(k8stesting.Action) (bool, runtime.Object, error) {
		attempts++
		return true, nil, apierrors.NewConflict(leaseGVR.GroupResource(), name, errors.New("object was modified"))
	})

	err := store.Put(context.Background(), 1, renewalOf(rec))
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict once the budget is spent", err)
	}
	if attempts != maxPutUpdateAttempts {
		t.Fatalf("update attempts = %d, want %d", attempts, maxPutUpdateAttempts)
	}
}

// TestK8sStorePutConflictRetryHonoursCancellation checks that a caller who
// has given up does not have its write retried behind it. The fake clientset
// ignores context cancellation, so the loop tests ctx itself.
func TestK8sStorePutConflictRetryHonoursCancellation(t *testing.T) {
	t.Parallel()

	store, client, rec := seedForConflict(t)
	name := k8sLeaseName(rec.Key)
	ctx, cancel := context.WithCancel(context.Background())

	attempts := 0
	client.PrependReactor("update", "leases", func(k8stesting.Action) (bool, runtime.Object, error) {
		attempts++
		cancel() // the caller times out while the first attempt is in flight
		return true, nil, apierrors.NewConflict(leaseGVR.GroupResource(), name, errors.New("object was modified"))
	})

	err := store.Put(ctx, 1, renewalOf(rec))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if attempts != 1 {
		t.Fatalf("update attempts = %d, want 1; the loop must stop at cancellation", attempts)
	}
}
