package operator

import (
	"context"
	"time"

	"github.com/skaphos/berth/pkg/client"
)

// LeaseClient is the subset of [client.Client] consumed by the reconciler.
// Defined as an interface so tests can inject a fake.
type LeaseClient interface {
	Acquire(ctx context.Context, namespace, name, holder string, ttl time.Duration) (client.AcquireResult, error)
	Renew(ctx context.Context, namespace, name, holder string, fencingToken int32, ttl time.Duration) (client.AcquireResult, error)
	Release(ctx context.Context, namespace, name, holder string, fencingToken int32) error
}
