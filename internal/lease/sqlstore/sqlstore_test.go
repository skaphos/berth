package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/skaphos/berth/internal/lease"
)

func newSQLiteStore(t *testing.T) *Store {
	t.Helper()
	store, err := New(context.Background(), Config{
		Driver: DriverSQLite,
		DSN:    "file:" + t.Name() + "?mode=memory&cache=shared",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close sqlite store: %v", err)
		}
	})
	return store
}

func sampleRecord() *lease.Record {
	now := time.Date(2026, 5, 24, 10, 30, 0, 123456000, time.UTC)
	return &lease.Record{
		Key:          lease.Key{Namespace: "tenant-a", Name: "ingest"},
		Holder:       "cluster-east",
		TTL:          30 * time.Second,
		AcquiredAt:   now,
		RenewedAt:    now,
		FencingToken: 1,
	}
}

func TestNewValidatesConfig(t *testing.T) {
	t.Parallel()

	if _, err := New(context.Background(), Config{Driver: DriverSQLite}); err == nil {
		t.Fatal("expected error for empty DSN")
	}
	if _, err := New(context.Background(), Config{Driver: "redis", DSN: "x"}); err == nil {
		t.Fatal("expected error for unknown driver")
	}
	if _, err := New(context.Background(), Config{Driver: DriverSQLite, DSN: ":memory:", Migrate: "sometimes"}); err == nil {
		t.Fatal("expected error for unknown migrate mode")
	}
}

func TestMySQLDialectUsesReadCommittedTransactions(t *testing.T) {
	t.Parallel()

	d, err := dialectFor(DriverMySQL)
	if err != nil {
		t.Fatal(err)
	}
	if d.readTx == nil || d.readTx.Isolation != sql.LevelReadCommitted || !d.readTx.ReadOnly {
		t.Fatalf("read tx = %#v, want read-only READ COMMITTED", d.readTx)
	}
	if d.writeTx == nil || d.writeTx.Isolation != sql.LevelReadCommitted || d.writeTx.ReadOnly {
		t.Fatalf("write tx = %#v, want read-write READ COMMITTED", d.writeTx)
	}
}

func TestSQLiteStorePutCreateThenGetRoundTrips(t *testing.T) {
	t.Parallel()

	store := newSQLiteStore(t)
	rec := sampleRecord()

	if err := store.Put(context.Background(), 0, rec); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := store.Get(context.Background(), rec.Key)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Key != rec.Key ||
		got.Holder != rec.Holder ||
		got.TTL != rec.TTL ||
		got.FencingToken != rec.FencingToken ||
		!got.AcquiredAt.Equal(rec.AcquiredAt) ||
		!got.RenewedAt.Equal(rec.RenewedAt) {
		t.Fatalf("round-trip mismatch:\n got = %+v\nwant = %+v", got, rec)
	}
}

func TestSQLiteStorePutCreateConflictsWhenPresent(t *testing.T) {
	t.Parallel()

	store := newSQLiteStore(t)
	rec := sampleRecord()
	if err := store.Put(context.Background(), 0, rec); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(context.Background(), 0, rec); !errors.Is(err, lease.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
}

func TestSQLiteStorePutCASRequiresMatchingToken(t *testing.T) {
	t.Parallel()

	store := newSQLiteStore(t)
	first := sampleRecord()
	if err := store.Put(context.Background(), 0, first); err != nil {
		t.Fatal(err)
	}

	stale := *first
	stale.Holder = "cluster-west"
	stale.FencingToken = 2
	if err := store.Put(context.Background(), 99, &stale); !errors.Is(err, lease.ErrConflict) {
		t.Fatalf("stale CAS err = %v, want ErrConflict", err)
	}

	if err := store.Put(context.Background(), first.FencingToken, &stale); err != nil {
		t.Fatalf("matching CAS: %v", err)
	}
	got, err := store.Get(context.Background(), stale.Key)
	if err != nil {
		t.Fatal(err)
	}
	if got.Holder != "cluster-west" || got.FencingToken != 2 {
		t.Fatalf("got holder/token = %s/%d, want cluster-west/2", got.Holder, got.FencingToken)
	}
}

func TestSQLiteStorePutCASOnAbsentReturnsConflict(t *testing.T) {
	t.Parallel()

	store := newSQLiteStore(t)
	if err := store.Put(context.Background(), 1, sampleRecord()); !errors.Is(err, lease.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
}

func TestSQLiteStoreDeleteDistinguishesNotFoundAndConflict(t *testing.T) {
	t.Parallel()

	store := newSQLiteStore(t)
	rec := sampleRecord()
	if err := store.Delete(context.Background(), rec.Key, 1); !errors.Is(err, lease.ErrNotFound) {
		t.Fatalf("missing delete err = %v, want ErrNotFound", err)
	}
	if err := store.Put(context.Background(), 0, rec); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(context.Background(), rec.Key, 99); !errors.Is(err, lease.ErrConflict) {
		t.Fatalf("stale delete err = %v, want ErrConflict", err)
	}
	if err := store.Delete(context.Background(), rec.Key, rec.FencingToken); err != nil {
		t.Fatalf("matching delete: %v", err)
	}
	if _, err := store.Get(context.Background(), rec.Key); !errors.Is(err, lease.ErrNotFound) {
		t.Fatalf("after delete err = %v, want ErrNotFound", err)
	}
}

func TestSQLiteStoreList(t *testing.T) {
	t.Parallel()

	store := newSQLiteStore(t)
	rec := sampleRecord()
	if err := store.Put(context.Background(), 0, rec); err != nil {
		t.Fatal(err)
	}
	rec2 := *rec
	rec2.Key = lease.Key{Namespace: "tenant-b", Name: "egress"}
	if err := store.Put(context.Background(), 0, &rec2); err != nil {
		t.Fatal(err)
	}

	got, err := store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("List len = %d, want 2", len(got))
	}
}

func TestSQLiteStoreContextCancellationIsHonored(t *testing.T) {
	t.Parallel()

	store := newSQLiteStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := store.Get(ctx, lease.Key{Namespace: "ns", Name: "a"}); err == nil {
		t.Fatal("Get must surface context cancellation")
	}
	if _, err := store.List(ctx); err == nil {
		t.Fatal("List must surface context cancellation")
	}
	if err := store.Put(ctx, 0, sampleRecord()); err == nil {
		t.Fatal("Put must surface context cancellation")
	}
	if err := store.Delete(ctx, lease.Key{Namespace: "ns", Name: "a"}, 1); err == nil {
		t.Fatal("Delete must surface context cancellation")
	}
}

func TestSQLiteStoreConcurrentAcquireExactlyOneWinner(t *testing.T) {
	t.Parallel()

	const holders = 16
	mgr := lease.NewManager(newSQLiteStore(t))
	key := lease.Key{Namespace: "ns", Name: "a"}

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		winners   []string
		winnerTok int32
	)
	wg.Add(holders)
	for i := range holders {
		holder := fmt.Sprintf("holder-%02d", i)
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

func TestParseTimeStringSQLFormats(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want time.Time
	}{
		{
			name: "mysql datetime six fractional digits",
			in:   "2026-05-24 10:30:00.123456",
			want: time.Date(2026, 5, 24, 10, 30, 0, 123456000, time.UTC),
		},
		{
			name: "mysql datetime three fractional digits",
			in:   "2026-05-24 10:30:00.123",
			want: time.Date(2026, 5, 24, 10, 30, 0, 123000000, time.UTC),
		},
		{
			name: "sql timestamp six fractional digits with offset",
			in:   "2026-05-24 10:30:00.123456-05:00",
			want: time.Date(2026, 5, 24, 15, 30, 0, 123456000, time.UTC),
		},
		{
			name: "sql timestamp three fractional digits with offset",
			in:   "2026-05-24 10:30:00.123-05:00",
			want: time.Date(2026, 5, 24, 15, 30, 0, 123000000, time.UTC),
		},
		{
			name: "seconds only",
			in:   "2026-05-24 10:30:00",
			want: time.Date(2026, 5, 24, 10, 30, 0, 0, time.UTC),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseTimeString(tc.in)
			if err != nil {
				t.Fatalf("parseTimeString(%q): %v", tc.in, err)
			}
			if !got.Equal(tc.want) {
				t.Fatalf("parseTimeString(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

var _ lease.Store = (*Store)(nil)
