package lease_test

import (
	"testing"

	"github.com/skaphos/berth/internal/lease"
	"github.com/skaphos/berth/internal/lease/storetest"
)

// BenchmarkMemStore runs the shared store-benchmark suite against MemStore.
// It is the zero-infrastructure baseline: a pure in-process map+mutex with no
// durability, so its numbers are the floor every durable backend is measured
// against. Lives in lease_test (external) to avoid an import cycle with
// storetest, which imports lease.
func BenchmarkMemStore(b *testing.B) {
	storetest.RunStoreBenchmarks(b, func(testing.TB) lease.Store {
		return lease.NewMemStore()
	})
}
