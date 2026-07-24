package lease_test

import (
	"testing"

	"k8s.io/client-go/kubernetes/fake"

	"github.com/skaphos/berth/internal/lease"
	"github.com/skaphos/berth/internal/lease/storetest"
)

// TestMemStoreConformance runs the full shared store suite against MemStore.
// Lives in lease_test (external) to avoid an import cycle with storetest,
// which imports lease.
func TestMemStoreConformance(t *testing.T) {
	storetest.RunStoreConformance(t, func(testing.TB) lease.Store {
		return lease.NewMemStore()
	})
}

// TestK8sStoreSafetyRegressions runs the issue #90/#92/#93 store-boundary
// regression suite against the Kubernetes backend over the fake clientset.
// The fake ignores context cancellation, so the full conformance suite
// cannot run here; the k8s-specific behavior it would cover has bespoke
// tests in k8sstore_test.go.
func TestK8sStoreSafetyRegressions(t *testing.T) {
	storetest.RunStoreSafetyRegressions(t, func(tb testing.TB) lease.Store {
		store, err := lease.NewK8sLeaseStore(fake.NewSimpleClientset(), "berth-system")
		if err != nil {
			tb.Fatal(err)
		}
		return store
	})
}
