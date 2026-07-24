package lease

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// managedByLabel marks Lease objects created by Berth so List can
	// filter them out from any unrelated Leases sharing the namespace.
	managedByLabel = "berth.skaphos.io/managed-by"
	managedByValue = "berth"

	// Annotations preserve the original Berth (tenant-namespace, lease-name)
	// pair. The K8s Lease.metadata.name encoding is unambiguous for
	// validated keys (the namespace segment cannot contain a dot, so the
	// first dot is the separator), but the annotations remain the source of
	// truth for reverse translation.
	tenantNamespaceAnnotation = "berth.skaphos.io/tenant-namespace"
	leaseNameAnnotation       = "berth.skaphos.io/lease-name"

	// versionAnnotation persists [Record.Version]. Objects written before
	// the annotation existed read as version 1; the resourceVersion guard on
	// updates keeps even their first post-upgrade racing writes safe.
	versionAnnotation = "berth.skaphos.io/version"
)

// K8sLeaseStore implements [Store] against coordination.k8s.io/v1.Lease
// objects in a single namespace of a coordination cluster.
//
// Each Berth lease maps 1:1 to a Lease object; the FencingToken is
// persisted as Lease.spec.leaseTransitions and the record Version as an
// annotation. Storage-level CAS is double-guarded: Put checks the version
// annotation against the caller's expectation and the subsequent UPDATE
// carries metadata.resourceVersion from the same read, so the kube-apiserver
// returns 409 Conflict on any interleaved write (translated to
// [ErrConflict]).
//
// All Berth leases across all tenants are pooled in a single coordination
// namespace; the K8s Lease.metadata.name encodes the Berth (tenant-ns,
// lease-name) pair as "<tenant-ns>.<lease-name>".
type K8sLeaseStore struct {
	client    kubernetes.Interface
	namespace string
}

// NewK8sLeaseStore returns a Store that persists leases into namespace of
// the cluster reachable via client.
func NewK8sLeaseStore(client kubernetes.Interface, namespace string) (*K8sLeaseStore, error) {
	if client == nil {
		return nil, errors.New("k8s lease store: client is required")
	}
	if namespace == "" {
		return nil, errors.New("k8s lease store: namespace is required")
	}
	return &K8sLeaseStore{client: client, namespace: namespace}, nil
}

// Ping implements [Store]. It issues a single-item List to confirm the
// coordination apiserver is reachable without paying for a full lease
// enumeration (Limit: 1 is constant cost regardless of lease count).
func (s *K8sLeaseStore) Ping(ctx context.Context) error {
	_, err := s.client.CoordinationV1().Leases(s.namespace).List(ctx, metav1.ListOptions{Limit: 1})
	if err != nil {
		return fmt.Errorf("k8s lease store: ping: %w", err)
	}
	return nil
}

// Get implements [Store].
func (s *K8sLeaseStore) Get(ctx context.Context, key Key) (*Record, error) {
	l, err := s.client.CoordinationV1().Leases(s.namespace).Get(ctx, k8sLeaseName(key), metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("k8s lease store: get %s: %w", key, err)
	}
	return recordFromLease(l)
}

// List implements [Store]. Only Lease objects bearing the Berth managed-by
// label are returned.
func (s *K8sLeaseStore) List(ctx context.Context) ([]Record, error) {
	list, err := s.client.CoordinationV1().Leases(s.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: managedByLabel + "=" + managedByValue,
	})
	if err != nil {
		return nil, fmt.Errorf("k8s lease store: list: %w", err)
	}
	out := make([]Record, 0, len(list.Items))
	for i := range list.Items {
		rec, err := recordFromLease(&list.Items[i])
		if err != nil {
			// Skip Leases that aren't ours (annotation missing). Defensive
			// belt-and-suspenders — the label selector above should prevent
			// this from happening in practice.
			continue
		}
		out = append(out, *rec)
	}
	return out, nil
}

// Put implements [Store]. expectedVersion=0 creates; expectedVersion>0
// updates only when the current Lease's version annotation equals
// expectedVersion, with the update additionally guarded by the
// resourceVersion captured in the same read.
func (s *K8sLeaseStore) Put(ctx context.Context, expectedVersion int64, rec *Record) error {
	if rec == nil {
		return ErrConflict
	}
	leases := s.client.CoordinationV1().Leases(s.namespace)

	if expectedVersion == 0 {
		_, err := leases.Create(ctx, leaseFromRecord(rec, s.namespace, 1), metav1.CreateOptions{})
		if err != nil {
			if apierrors.IsAlreadyExists(err) {
				return ErrConflict
			}
			return fmt.Errorf("k8s lease store: create %s: %w", rec.Key, err)
		}
		return nil
	}

	cur, err := leases.Get(ctx, k8sLeaseName(rec.Key), metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ErrConflict
		}
		return fmt.Errorf("k8s lease store: get %s for put: %w", rec.Key, err)
	}
	if versionFromLease(cur) != expectedVersion {
		return ErrConflict
	}
	applyRecordToLease(cur, rec, expectedVersion+1)
	if _, err := leases.Update(ctx, cur, metav1.UpdateOptions{}); err != nil {
		if apierrors.IsConflict(err) {
			return ErrConflict
		}
		return fmt.Errorf("k8s lease store: update %s: %w", rec.Key, err)
	}
	return nil
}

// k8sLeaseName encodes a Berth Key as a coordination.k8s.io Lease name. The
// encoding is injective for keys accepted by [ValidateKey]: the namespace
// segment cannot contain a dot, so the first dot always separates namespace
// from name.
func k8sLeaseName(key Key) string {
	return key.Namespace + "." + key.Name
}

// versionFromLease reads the record version annotation. Objects written
// before the annotation existed (or with a mangled value) read as version 1,
// the same default the SQL migration applies to legacy rows.
func versionFromLease(l *coordinationv1.Lease) int64 {
	v, err := strconv.ParseInt(l.Annotations[versionAnnotation], 10, 64)
	if err != nil || v < 1 {
		return 1
	}
	return v
}

// recordFromLease translates a Lease into a Record. Returns an error when
// the Lease lacks the Berth annotations identifying its origin.
func recordFromLease(l *coordinationv1.Lease) (*Record, error) {
	tenant, ok := l.Annotations[tenantNamespaceAnnotation]
	if !ok {
		return nil, fmt.Errorf("lease %s/%s: missing annotation %s", l.Namespace, l.Name, tenantNamespaceAnnotation)
	}
	name, ok := l.Annotations[leaseNameAnnotation]
	if !ok {
		return nil, fmt.Errorf("lease %s/%s: missing annotation %s", l.Namespace, l.Name, leaseNameAnnotation)
	}
	rec := &Record{Key: Key{Namespace: tenant, Name: name}, Version: versionFromLease(l)}
	if l.Spec.HolderIdentity != nil {
		rec.Holder = *l.Spec.HolderIdentity
	}
	if l.Spec.LeaseDurationSeconds != nil {
		rec.TTL = time.Duration(*l.Spec.LeaseDurationSeconds) * time.Second
	}
	if l.Spec.AcquireTime != nil {
		rec.AcquiredAt = l.Spec.AcquireTime.Time
	}
	if l.Spec.RenewTime != nil {
		rec.RenewedAt = l.Spec.RenewTime.Time
	}
	if l.Spec.LeaseTransitions != nil {
		rec.FencingToken = *l.Spec.LeaseTransitions
	}
	return rec, nil
}

// leaseFromRecord builds a fresh Lease (no resourceVersion) for Create,
// stored at the given version.
func leaseFromRecord(rec *Record, namespace string, version int64) *coordinationv1.Lease {
	holder := rec.Holder
	ttl := int32(rec.TTL / time.Second)
	transitions := rec.FencingToken
	acquired := metav1.NewMicroTime(rec.AcquiredAt)
	renewed := metav1.NewMicroTime(rec.RenewedAt)
	return &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      k8sLeaseName(rec.Key),
			Namespace: namespace,
			Labels:    map[string]string{managedByLabel: managedByValue},
			Annotations: map[string]string{
				tenantNamespaceAnnotation: rec.Key.Namespace,
				leaseNameAnnotation:       rec.Key.Name,
				versionAnnotation:         strconv.FormatInt(version, 10),
			},
		},
		Spec: coordinationv1.LeaseSpec{
			HolderIdentity:       &holder,
			LeaseDurationSeconds: &ttl,
			AcquireTime:          &acquired,
			RenewTime:            &renewed,
			LeaseTransitions:     &transitions,
		},
	}
}

// applyRecordToLease overwrites the spec of an existing Lease while
// preserving its resourceVersion, storing the record at the given version.
// Labels and annotations are repaired in case they were stripped externally.
func applyRecordToLease(l *coordinationv1.Lease, rec *Record, version int64) {
	holder := rec.Holder
	ttl := int32(rec.TTL / time.Second)
	transitions := rec.FencingToken
	acquired := metav1.NewMicroTime(rec.AcquiredAt)
	renewed := metav1.NewMicroTime(rec.RenewedAt)
	l.Spec.HolderIdentity = &holder
	l.Spec.LeaseDurationSeconds = &ttl
	l.Spec.AcquireTime = &acquired
	l.Spec.RenewTime = &renewed
	l.Spec.LeaseTransitions = &transitions
	if l.Annotations == nil {
		l.Annotations = map[string]string{}
	}
	l.Annotations[tenantNamespaceAnnotation] = rec.Key.Namespace
	l.Annotations[leaseNameAnnotation] = rec.Key.Name
	l.Annotations[versionAnnotation] = strconv.FormatInt(version, 10)
	if l.Labels == nil {
		l.Labels = map[string]string{}
	}
	l.Labels[managedByLabel] = managedByValue
}
