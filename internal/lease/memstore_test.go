package lease

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestMemStorePutCreateRequiresExpectedZero(t *testing.T) {
	t.Parallel()

	s := NewMemStore()
	rec := &Record{Key: Key{Namespace: "ns", Name: "a"}, Holder: "h", FencingToken: 1, RenewedAt: time.Now(), TTL: time.Minute}

	if err := s.Put(context.Background(), 1, rec); !errors.Is(err, ErrConflict) {
		t.Fatalf("create with non-zero expected = %v, want ErrConflict", err)
	}
	if err := s.Put(context.Background(), 0, rec); err != nil {
		t.Fatalf("create with expected=0: %v", err)
	}
}

func TestMemStorePutSecondCreateConflicts(t *testing.T) {
	t.Parallel()

	s := NewMemStore()
	rec := &Record{Key: Key{Namespace: "ns", Name: "a"}, Holder: "h", FencingToken: 1}

	if err := s.Put(context.Background(), 0, rec); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(context.Background(), 0, rec); !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
}

func TestMemStorePutCASRequiresMatchingToken(t *testing.T) {
	t.Parallel()

	s := NewMemStore()
	key := Key{Namespace: "ns", Name: "a"}
	first := &Record{Key: key, Holder: "h", FencingToken: 1}
	if err := s.Put(context.Background(), 0, first); err != nil {
		t.Fatal(err)
	}

	stale := &Record{Key: key, Holder: "h", FencingToken: 2}
	if err := s.Put(context.Background(), 99, stale); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale CAS err = %v, want ErrConflict", err)
	}

	if err := s.Put(context.Background(), 1, stale); err != nil {
		t.Fatalf("matching CAS: %v", err)
	}
}

func TestMemStoreGetReturnsCopy(t *testing.T) {
	t.Parallel()

	s := NewMemStore()
	key := Key{Namespace: "ns", Name: "a"}
	if err := s.Put(context.Background(), 0, &Record{Key: key, Holder: "h", FencingToken: 1}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	got.Holder = "tampered"
	got2, _ := s.Get(context.Background(), key)
	if got2.Holder != "h" {
		t.Fatal("Get must return a copy independent of the store's record")
	}
}

func TestMemStoreDeleteRequiresMatchingToken(t *testing.T) {
	t.Parallel()

	s := NewMemStore()
	key := Key{Namespace: "ns", Name: "a"}
	if err := s.Put(context.Background(), 0, &Record{Key: key, Holder: "h", FencingToken: 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(context.Background(), key, 99); !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	if err := s.Delete(context.Background(), key, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(context.Background(), key, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestMemStoreContextCancellationIsHonored(t *testing.T) {
	t.Parallel()

	s := NewMemStore()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := s.Get(ctx, Key{Namespace: "ns", Name: "a"}); err == nil {
		t.Fatal("Get must surface context cancellation")
	}
	if _, err := s.List(ctx); err == nil {
		t.Fatal("List must surface context cancellation")
	}
	if err := s.Put(ctx, 0, &Record{Key: Key{Namespace: "ns", Name: "a"}}); err == nil {
		t.Fatal("Put must surface context cancellation")
	}
	if err := s.Delete(ctx, Key{Namespace: "ns", Name: "a"}, 1); err == nil {
		t.Fatal("Delete must surface context cancellation")
	}
}

// TestConcurrentAcquireExactlyOneWinner exercises the race that motivates
// SKA-272: many holders, one key — exactly one must end up holding the lease.
func TestConcurrentAcquireExactlyOneWinner(t *testing.T) {
	t.Parallel()

	const holders = 16
	mgr := NewManager(NewMemStore())
	key := Key{Namespace: "ns", Name: "a"}

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		winners   []string
		winnerTok int32
	)
	wg.Add(holders)
	for i := range holders {
		holder := time.Now().Format("h-150405.000000") // unique per goroutine
		holder += "-" + string(rune('a'+i))
		go func(holder string) {
			defer wg.Done()
			res, err := mgr.Acquire(context.Background(), key, holder, time.Minute)
			if err != nil {
				t.Errorf("Acquire(%s): %v", holder, err)
				return
			}
			if res.Acquired {
				mu.Lock()
				winners = append(winners, holder)
				winnerTok = res.FencingToken
				mu.Unlock()
			}
		}(holder)
	}
	wg.Wait()

	if len(winners) != 1 {
		t.Fatalf("winners = %d, want 1", len(winners))
	}
	if winnerTok != 1 {
		t.Fatalf("winner token = %d, want 1", winnerTok)
	}
}
