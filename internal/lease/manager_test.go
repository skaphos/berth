package lease

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestManager(t *testing.T, now time.Time) (*Manager, *MemStore, *fakeClock) {
	t.Helper()
	clock := &fakeClock{now: now}
	store := NewMemStore()
	mgr := NewManager(store).WithClock(clock.Now)
	return mgr, store, clock
}

type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) advance(d time.Duration) {
	c.now = c.now.Add(d)
}

func TestAcquireRejectsAtFencingTokenCeiling(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	mgr, store, clock := newTestManager(t, base)
	key := Key{Namespace: "ns", Name: "maxed"}

	// Seed an expired record already at the int32 ceiling. Reclaiming it would
	// have to bump the token, which must fail rather than wrap.
	if err := store.Put(context.Background(), 0, &Record{
		Key:          key,
		Holder:       "old",
		TTL:          time.Minute,
		AcquiredAt:   base,
		RenewedAt:    base,
		FencingToken: math.MaxInt32,
	}); err != nil {
		t.Fatalf("seed Put: %v", err)
	}
	clock.advance(2 * time.Minute) // lease is now expired

	if _, err := mgr.Acquire(context.Background(), key, "new", 30*time.Second); err == nil {
		t.Fatal("expected error reclaiming a lease at the int32 fencing-token ceiling")
	} else if !strings.Contains(err.Error(), "ceiling") {
		t.Fatalf("error = %v, want it to mention the int32 ceiling", err)
	}

	// The original record must be untouched — no wrap to a negative/reused token.
	cur, err := store.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if cur.FencingToken != math.MaxInt32 || cur.Holder != "old" {
		t.Fatalf("record mutated: token=%d holder=%q", cur.FencingToken, cur.Holder)
	}
}

func TestNewManagerPreservesStore(t *testing.T) {
	t.Parallel()

	store := NewMemStore()
	mgr := NewManager(store)
	if mgr.store != store {
		t.Fatal("store was not preserved")
	}
	if mgr.now == nil {
		t.Fatal("clock was not initialized")
	}
}

func TestAcquireFreshLease(t *testing.T) {
	t.Parallel()

	mgr, _, clock := newTestManager(t, time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC))
	res, err := mgr.Acquire(context.Background(), Key{Namespace: "ns", Name: "a"}, "holder-1", 30*time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if !res.Acquired {
		t.Fatal("expected Acquired=true on fresh lease")
	}
	if res.Holder != "holder-1" {
		t.Fatalf("Holder = %q, want %q", res.Holder, "holder-1")
	}
	if res.FencingToken != 1 {
		t.Fatalf("FencingToken = %d, want 1", res.FencingToken)
	}
	if want := clock.now.Add(30 * time.Second); !res.ExpiresAt.Equal(want) {
		t.Fatalf("ExpiresAt = %v, want %v", res.ExpiresAt, want)
	}
}

func TestAcquireSecondHolderIsRejectedWhileLive(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mgr, _, clock := newTestManager(t, time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC))
	key := Key{Namespace: "ns", Name: "a"}

	first, err := mgr.Acquire(ctx, key, "holder-1", 30*time.Second)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	if !first.Acquired {
		t.Fatal("first Acquire should succeed")
	}

	clock.advance(5 * time.Second)
	second, err := mgr.Acquire(ctx, key, "holder-2", 30*time.Second)
	if err != nil {
		t.Fatalf("second Acquire: %v", err)
	}
	if second.Acquired {
		t.Fatal("second Acquire should be rejected while first is live")
	}
	if second.Holder != "holder-1" {
		t.Fatalf("Holder = %q, want %q", second.Holder, "holder-1")
	}
	if second.FencingToken != 1 {
		t.Fatalf("FencingToken = %d, want 1", second.FencingToken)
	}
}

func TestAcquireSameHolderIsRenewalNoTokenBump(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mgr, _, clock := newTestManager(t, time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC))
	key := Key{Namespace: "ns", Name: "a"}

	first, err := mgr.Acquire(ctx, key, "holder-1", 30*time.Second)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}

	clock.advance(10 * time.Second)
	second, err := mgr.Acquire(ctx, key, "holder-1", 30*time.Second)
	if err != nil {
		t.Fatalf("second Acquire: %v", err)
	}
	if !second.Acquired {
		t.Fatal("same holder should renew successfully")
	}
	if second.FencingToken != first.FencingToken {
		t.Fatalf("FencingToken bumped on renewal: %d → %d", first.FencingToken, second.FencingToken)
	}
	if !second.AcquiredAt.Equal(first.AcquiredAt) {
		t.Fatalf("AcquiredAt drifted: %v → %v", first.AcquiredAt, second.AcquiredAt)
	}
	if want := clock.now.Add(30 * time.Second); !second.ExpiresAt.Equal(want) {
		t.Fatalf("ExpiresAt = %v, want %v (renewed)", second.ExpiresAt, want)
	}
}

func TestAcquireReclaimsAfterExpiryAndBumpsToken(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mgr, _, clock := newTestManager(t, time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC))
	key := Key{Namespace: "ns", Name: "a"}

	first, err := mgr.Acquire(ctx, key, "holder-1", 30*time.Second)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}

	clock.advance(31 * time.Second) // past TTL
	second, err := mgr.Acquire(ctx, key, "holder-2", 30*time.Second)
	if err != nil {
		t.Fatalf("second Acquire: %v", err)
	}
	if !second.Acquired {
		t.Fatal("expected reclaim after expiry")
	}
	if second.Holder != "holder-2" {
		t.Fatalf("Holder = %q, want holder-2", second.Holder)
	}
	if second.FencingToken != first.FencingToken+1 {
		t.Fatalf("FencingToken = %d, want %d", second.FencingToken, first.FencingToken+1)
	}
}

func TestRenewExtendsTTL(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mgr, _, clock := newTestManager(t, time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC))
	key := Key{Namespace: "ns", Name: "a"}

	acq, err := mgr.Acquire(ctx, key, "h", 30*time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	clock.advance(20 * time.Second)
	res, err := mgr.Renew(ctx, key, "h", acq.FencingToken, 30*time.Second)
	if err != nil {
		t.Fatalf("Renew: %v", err)
	}
	if !res.Acquired {
		t.Fatal("Renew should succeed for live holder")
	}
	if want := clock.now.Add(30 * time.Second); !res.ExpiresAt.Equal(want) {
		t.Fatalf("ExpiresAt = %v, want %v", res.ExpiresAt, want)
	}
}

func TestRenewRejectsStaleToken(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mgr, _, clock := newTestManager(t, time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC))
	key := Key{Namespace: "ns", Name: "a"}

	first, _ := mgr.Acquire(ctx, key, "holder-1", 5*time.Second)
	clock.advance(10 * time.Second) // expire
	mgr.Acquire(ctx, key, "holder-2", 30*time.Second)

	res, err := mgr.Renew(ctx, key, "holder-1", first.FencingToken, 30*time.Second)
	if err != nil {
		t.Fatalf("Renew: %v", err)
	}
	if res.Acquired {
		t.Fatal("Renew with stale token must not succeed")
	}
}

func TestRenewOnAbsentLeaseReturnsNotAcquired(t *testing.T) {
	t.Parallel()

	mgr, _, _ := newTestManager(t, time.Now())
	res, err := mgr.Renew(context.Background(), Key{Namespace: "ns", Name: "a"}, "h", 1, 5*time.Second)
	if err != nil {
		t.Fatalf("Renew: %v", err)
	}
	if res.Acquired {
		t.Fatal("Renew on absent lease must not succeed")
	}
}

func TestReleaseIsIdempotent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mgr, _, _ := newTestManager(t, time.Now())
	key := Key{Namespace: "ns", Name: "a"}

	acq, _ := mgr.Acquire(ctx, key, "h", 30*time.Second)
	if err := mgr.Release(ctx, key, "h", acq.FencingToken); err != nil {
		t.Fatalf("first Release: %v", err)
	}
	if err := mgr.Release(ctx, key, "h", acq.FencingToken); err != nil {
		t.Fatalf("second Release should be no-op: %v", err)
	}
}

func TestReleaseWithStaleTokenReturnsConflict(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mgr, _, clock := newTestManager(t, time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC))
	key := Key{Namespace: "ns", Name: "a"}

	first, _ := mgr.Acquire(ctx, key, "holder-1", 5*time.Second)
	clock.advance(10 * time.Second)
	mgr.Acquire(ctx, key, "holder-2", 30*time.Second)

	err := mgr.Release(ctx, key, "holder-1", first.FencingToken)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
}

func TestAcquireRejectsEmptyHolderAndZeroTTL(t *testing.T) {
	t.Parallel()

	mgr, _, _ := newTestManager(t, time.Now())
	if _, err := mgr.Acquire(context.Background(), Key{Namespace: "ns", Name: "a"}, "", 30*time.Second); err == nil {
		t.Fatal("Acquire with empty holder must return error")
	}
	if _, err := mgr.Acquire(context.Background(), Key{Namespace: "ns", Name: "a"}, "h", 0); err == nil {
		t.Fatal("Acquire with zero TTL must return error")
	}
}

func TestRenewValidatesArgs(t *testing.T) {
	t.Parallel()

	mgr, _, _ := newTestManager(t, time.Now())
	if _, err := mgr.Renew(context.Background(), Key{Namespace: "ns", Name: "a"}, "", 1, 30*time.Second); err == nil {
		t.Fatal("Renew with empty holder must return error")
	}
	if _, err := mgr.Renew(context.Background(), Key{Namespace: "ns", Name: "a"}, "h", 0, 30*time.Second); err == nil {
		t.Fatal("Renew with zero token must return error")
	}
	if _, err := mgr.Renew(context.Background(), Key{Namespace: "ns", Name: "a"}, "h", 1, 0); err == nil {
		t.Fatal("Renew with zero TTL must return error")
	}
}

func TestReleaseValidatesArgs(t *testing.T) {
	t.Parallel()

	mgr, _, _ := newTestManager(t, time.Now())
	if err := mgr.Release(context.Background(), Key{Namespace: "ns", Name: "a"}, "", 1); err == nil {
		t.Fatal("Release with empty holder must return error")
	}
}

func TestManagerRejectsInvalidKeys(t *testing.T) {
	t.Parallel()

	mgr, _, _ := newTestManager(t, time.Now())
	bad := Key{Namespace: "a.b", Name: "c"}
	if _, err := mgr.Acquire(context.Background(), bad, "h", time.Minute); err == nil || !strings.Contains(err.Error(), "invalid namespace") {
		t.Fatalf("Acquire err = %v, want invalid-namespace error", err)
	}
	if _, err := mgr.Renew(context.Background(), bad, "h", 1, time.Minute); err == nil || !strings.Contains(err.Error(), "invalid namespace") {
		t.Fatalf("Renew err = %v, want invalid-namespace error", err)
	}
	if err := mgr.Release(context.Background(), bad, "h", 1); err == nil || !strings.Contains(err.Error(), "invalid namespace") {
		t.Fatalf("Release err = %v, want invalid-namespace error", err)
	}
}

// stallingStore blocks the first Put whose record matches stallOn: it
// signals entered, then waits for gate before forwarding to the inner
// store. This opens a deterministic window between a manager's read-side
// checks and its write — the window in which issue #90's double grant lived.
type stallingStore struct {
	Store
	stallOn func(rec *Record) bool
	entered chan struct{}
	gate    chan struct{}
	once    sync.Once
}

func (s *stallingStore) Put(ctx context.Context, expectedVersion int64, rec *Record) error {
	if s.stallOn(rec) {
		s.once.Do(func() {
			close(s.entered)
			<-s.gate
		})
	}
	return s.Store.Put(ctx, expectedVersion, rec)
}

// TestReclaimRacingRenewGrantsExactlyOne replays the issue #90 interleaving
// at the manager boundary: standby B sees the lease expired and prepares its
// reclaim write, then stalls; holder A's in-flight renew lands first; B's
// prepared write must now lose and B must be told it did not acquire.
// Pre-fix, the store CAS keyed on the fencing token — which A's renew left
// unchanged — so B's write also succeeded and both were told Acquired=true:
// two concurrent holders on the failover hot path.
func TestReclaimRacingRenewGrantsExactlyOne(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	mem := NewMemStore()
	stalling := &stallingStore{
		Store:   mem,
		stallOn: func(rec *Record) bool { return rec.Holder == "holder-b" },
		entered: make(chan struct{}),
		gate:    make(chan struct{}),
	}

	// A and B are API-server-side managers sharing one store; their clocks
	// pin the trace: A renews just before expiry (29.9s), B reclaims just
	// after (30.5s).
	seed := NewManager(mem).WithClock(func() time.Time { return base })
	mgrA := NewManager(mem).WithClock(func() time.Time { return base.Add(29*time.Second + 900*time.Millisecond) })
	mgrB := NewManager(stalling).WithClock(func() time.Time { return base.Add(30*time.Second + 500*time.Millisecond) })
	key := Key{Namespace: "ns", Name: "failover"}

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
		res, err := mgrB.Acquire(context.Background(), key, "holder-b", 30*time.Second)
		done <- out{res, err}
	}()

	<-stalling.entered // B saw the lease expired and is about to write its reclaim

	renewed, err := mgrA.Renew(context.Background(), key, "holder-a", acq.FencingToken, 30*time.Second)
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	if !renewed.Acquired {
		t.Fatal("A's renew landed before any reclaim and must succeed")
	}

	close(stalling.gate) // B's prepared reclaim write lands now
	b := <-done
	if b.err != nil {
		t.Fatalf("reclaim errored: %v", b.err)
	}
	if b.res.Acquired {
		t.Fatal("standby reported Acquired=true after the holder's renew landed — two concurrent holders")
	}
	if b.res.Holder != "holder-a" {
		t.Fatalf("standby was told holder = %q, want holder-a", b.res.Holder)
	}

	got, err := mem.Get(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if got.Holder != "holder-a" || got.FencingToken != acq.FencingToken {
		t.Fatalf("store state = holder %q token %d, want holder-a token %d", got.Holder, got.FencingToken, acq.FencingToken)
	}
}

func TestReleaseTombstonesAndAcquireBumpsPastHighWater(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mgr, store, _ := newTestManager(t, time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC))
	key := Key{Namespace: "ns", Name: "a"}

	acq, err := mgr.Acquire(ctx, key, "holder-1", 30*time.Second)
	if err != nil || !acq.Acquired {
		t.Fatalf("acquire: %v", err)
	}
	if err := mgr.Release(ctx, key, "holder-1", acq.FencingToken); err != nil {
		t.Fatalf("release: %v", err)
	}

	tomb, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("released record must remain as tombstone: %v", err)
	}
	if !tomb.Tombstone() || tomb.FencingToken != acq.FencingToken {
		t.Fatalf("tombstone = %+v, want empty holder and token %d", tomb, acq.FencingToken)
	}

	// A renew by the old holder against the tombstone is a lost lease, not
	// an error.
	res, err := mgr.Renew(ctx, key, "holder-1", acq.FencingToken, 30*time.Second)
	if err != nil {
		t.Fatalf("renew on tombstone: %v", err)
	}
	if res.Acquired {
		t.Fatal("renew on a released lease must not succeed")
	}

	// Releasing again — even with a mismatched holder/token — is idempotent.
	if err := mgr.Release(ctx, key, "someone-else", 99); err != nil {
		t.Fatalf("release on tombstone: %v", err)
	}

	// The next acquisition goes strictly past the high-water mark.
	re, err := mgr.Acquire(ctx, key, "holder-2", 30*time.Second)
	if err != nil || !re.Acquired {
		t.Fatalf("reacquire: %v", err)
	}
	if re.FencingToken != acq.FencingToken+1 {
		t.Fatalf("reacquired token = %d, want %d", re.FencingToken, acq.FencingToken+1)
	}
}

func TestAcquireRejectsCeilingFromTombstone(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mgr, store, _ := newTestManager(t, time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC))
	key := Key{Namespace: "ns", Name: "maxed"}

	if err := store.Put(ctx, 0, &Record{Key: key, Holder: "", FencingToken: math.MaxInt32}); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Acquire(ctx, key, "new", time.Minute); err == nil || !strings.Contains(err.Error(), "ceiling") {
		t.Fatalf("err = %v, want int32-ceiling error", err)
	}
}

// TestConcurrentLifecycleNeverReissuesAToken drives many goroutines through
// acquire/release cycles on one key and asserts every granted fencing token
// is unique — the concurrent statement of "at most one holder at any
// moment" plus "tokens never repeat" (issues #90 and #92 combined).
func TestConcurrentLifecycleNeverReissuesAToken(t *testing.T) {
	t.Parallel()

	const workers = 8
	const rounds = 25
	mgr := NewManager(NewMemStore())
	key := Key{Namespace: "ns", Name: "churn"}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		granted = make(map[int32]string)
	)
	wg.Add(workers)
	for w := range workers {
		holder := "holder-" + string(rune('a'+w))
		go func(holder string) {
			defer wg.Done()
			for range rounds {
				res, err := mgr.Acquire(context.Background(), key, holder, time.Minute)
				if err != nil {
					// Contention can exhaust the bounded CAS retry loop;
					// that is a liveness outcome, not a safety violation.
					continue
				}
				if !res.Acquired {
					continue
				}
				mu.Lock()
				if prev, dup := granted[res.FencingToken]; dup {
					t.Errorf("token %d granted to both %s and %s", res.FencingToken, prev, holder)
				}
				granted[res.FencingToken] = holder
				mu.Unlock()
				if err := mgr.Release(context.Background(), key, holder, res.FencingToken); err != nil {
					t.Errorf("release: %v", err)
				}
			}
		}(holder)
	}
	wg.Wait()

	if len(granted) == 0 {
		t.Fatal("no tokens were granted; the harness is broken")
	}
}
