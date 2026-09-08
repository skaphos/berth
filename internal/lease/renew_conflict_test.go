package lease

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestRenewLosingCASReportsCurrentHolder covers issue #120. Renew's lost-CAS
// branch used to report the pre-write read's holder and fencing token with
// zero ExpiresAt/AcquiredAt, unlike every other not-acquired path — so a
// caller rendering "held by X until Y" got a stale holder and zero times.
// The branch now re-reads and reports whoever actually won the key.
func TestRenewLosingCASReportsCurrentHolder(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	mem := NewMemStore()
	// Stall only A's renewal write: the seed acquisition and B's reclaim go
	// straight to mem.
	stalling := &stallingStore{
		Store:   mem,
		stallOn: func(rec *Record) bool { return rec.Holder == "holder-a" && rec.RenewedAt.After(base) },
		entered: make(chan struct{}),
		gate:    make(chan struct{}),
	}

	seed := NewManager(mem).WithClock(func() time.Time { return base })
	mgrA := NewManager(stalling).WithClock(func() time.Time { return base.Add(10 * time.Second) })
	reclaimAt := base.Add(40 * time.Second)
	mgrB := NewManager(mem).WithClock(func() time.Time { return reclaimAt })
	key := Key{Namespace: "ns", Name: "lost-cas"}

	acq, err := seed.Acquire(context.Background(), key, "holder-a", 30*time.Second)
	if err != nil || !acq.Acquired {
		t.Fatalf("seed acquire: %v acquired=%v", err, acq.Acquired)
	}

	type out struct {
		res AcquireResult
		err error
	}
	done := make(chan out, 1)
	go func() {
		res, err := mgrA.Renew(context.Background(), key, "holder-a", acq.FencingToken, 30*time.Second)
		done <- out{res, err}
	}()

	<-stalling.entered // A passed its read-side checks and is about to write

	// The lease expires while A's renewal is in flight; B reclaims it.
	reclaim, err := mgrB.Acquire(context.Background(), key, "holder-b", 30*time.Second)
	if err != nil || !reclaim.Acquired {
		t.Fatalf("reclaim: %v acquired=%v", err, reclaim.Acquired)
	}

	close(stalling.gate) // A's now-stale renewal write lands and must lose
	a := <-done
	if a.err != nil {
		t.Fatalf("renew: %v", a.err)
	}
	if a.res.Acquired {
		t.Fatal("renew reported Acquired=true after losing the CAS to a reclaim")
	}
	if a.res.Holder != "holder-b" {
		t.Fatalf("holder = %q, want holder-b (the writer that won the key)", a.res.Holder)
	}
	if a.res.FencingToken != reclaim.FencingToken {
		t.Fatalf("fencing token = %d, want %d", a.res.FencingToken, reclaim.FencingToken)
	}
	if a.res.AcquiredAt != reclaimAt {
		t.Fatalf("acquiredAt = %v, want %v", a.res.AcquiredAt, reclaimAt)
	}
	if want := reclaimAt.Add(30 * time.Second); a.res.ExpiresAt != want {
		t.Fatalf("expiresAt = %v, want %v", a.res.ExpiresAt, want)
	}
}

// blindAfterFirstGetStore serves one Get from the inner store, forces the
// write to lose its CAS, then fails every subsequent Get.
type blindAfterFirstGetStore struct {
	Store
	gets int
}

func (s *blindAfterFirstGetStore) Get(ctx context.Context, key Key) (*Record, error) {
	s.gets++
	if s.gets > 1 {
		return nil, errors.New("backend unavailable")
	}
	return s.Store.Get(ctx, key)
}

func (s *blindAfterFirstGetStore) Put(context.Context, int64, *Record) error { return ErrConflict }

// TestRenewLostCASFallsBackWhenRereadFails pins the other half of issue #120:
// when the post-conflict re-read cannot be served, Renew still reports lease
// loss with the fields it does have populated — never a zero expiry.
func TestRenewLostCASFallsBackWhenRereadFails(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	mem := NewMemStore()
	seed := NewManager(mem).WithClock(func() time.Time { return base })
	key := Key{Namespace: "ns", Name: "blind"}
	acq, err := seed.Acquire(context.Background(), key, "holder-a", 30*time.Second)
	if err != nil || !acq.Acquired {
		t.Fatalf("seed acquire: %v acquired=%v", err, acq.Acquired)
	}

	blind := &blindAfterFirstGetStore{Store: mem}
	mgr := NewManager(blind).WithClock(func() time.Time { return base.Add(10 * time.Second) })
	res, err := mgr.Renew(context.Background(), key, "holder-a", acq.FencingToken, 30*time.Second)
	if err != nil {
		t.Fatalf("renew must report loss, not error: %v", err)
	}
	if res.Acquired {
		t.Fatal("renew reported Acquired=true after a conflict")
	}
	if res.Holder != "holder-a" || res.FencingToken != acq.FencingToken {
		t.Fatalf("fallback lost the stale identity: %+v", res)
	}
	if res.AcquiredAt.IsZero() || res.ExpiresAt.IsZero() {
		t.Fatalf("fallback returned zero times: acquiredAt=%v expiresAt=%v", res.AcquiredAt, res.ExpiresAt)
	}
}
