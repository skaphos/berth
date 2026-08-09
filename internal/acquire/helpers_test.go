package acquire

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/skaphos/berth/pkg/client"
)

// acquireResult aliases the client result type so tests can stay terse.
type acquireResult = client.AcquireResult

// fakeClient is a programmable LeaseClient for tests. Each hook, when
// set, handles the corresponding call; calls are counted.
type fakeClient struct {
	mu sync.Mutex

	acquireFn func(holder string) (client.AcquireResult, error)
	renewFn   func(holder string, token int32) (client.AcquireResult, error)
	releaseFn func(holder string, token int32) error

	acquireCalls int
	renewCalls   int
	releaseCalls int
	lastRelease  struct {
		holder string
		token  int32
	}
}

func (f *fakeClient) Acquire(_ context.Context, _, _, holder string, _ time.Duration) (client.AcquireResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acquireCalls++
	if f.acquireFn != nil {
		return f.acquireFn(holder)
	}
	return client.AcquireResult{}, nil
}

func (f *fakeClient) Renew(_ context.Context, _, _, holder string, token int32, _ time.Duration) (client.AcquireResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.renewCalls++
	if f.renewFn != nil {
		return f.renewFn(holder, token)
	}
	return client.AcquireResult{}, nil
}

func (f *fakeClient) Release(_ context.Context, _, _, holder string, token int32) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.releaseCalls++
	f.lastRelease.holder = holder
	f.lastRelease.token = token
	if f.releaseFn != nil {
		return f.releaseFn(holder, token)
	}
	return nil
}

// hangingClient models an API server that accepts the request and then
// stalls: every call blocks until its context is done and reports the
// context error, exactly as an http.Client bounded by a deadline would.
// It is the regression fixture for the unbounded-renew defect — against
// a caller that passes the process-lifetime context, these calls never
// return.
type hangingClient struct {
	mu     sync.Mutex
	calls  int
	inCall chan struct{} // buffered; receives once per entered call
}

func newHangingClient() *hangingClient {
	return &hangingClient{inCall: make(chan struct{}, 16)}
}

func (h *hangingClient) enter() {
	h.mu.Lock()
	h.calls++
	h.mu.Unlock()
	select {
	case h.inCall <- struct{}{}:
	default:
	}
}

func (h *hangingClient) callCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.calls
}

func (h *hangingClient) Acquire(ctx context.Context, _, _, _ string, _ time.Duration) (client.AcquireResult, error) {
	h.enter()
	<-ctx.Done()
	return client.AcquireResult{}, ctx.Err()
}

func (h *hangingClient) Renew(ctx context.Context, _, _, _ string, _ int32, _ time.Duration) (client.AcquireResult, error) {
	h.enter()
	<-ctx.Done()
	return client.AcquireResult{}, ctx.Err()
}

func (h *hangingClient) Release(ctx context.Context, _, _, _ string, _ int32) error {
	h.enter()
	<-ctx.Done()
	return ctx.Err()
}

// acquired is a convenience AcquireResult for a won lease.
func acquired(token int32, ttl time.Duration) client.AcquireResult {
	return client.AcquireResult{Acquired: true, FencingToken: token, ExpiresAt: time.Now().Add(ttl)}
}

// heldByOther is a convenience AcquireResult for a lost lease.
func heldByOther(holder string) client.AcquireResult {
	return client.AcquireResult{Acquired: false, Holder: holder}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
