package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/skaphos/berth/internal/lease"
	"github.com/skaphos/berth/internal/lease/storetest"
)

// benchDBSeq names each benchmark's in-memory SQLite database uniquely. The
// benchmark runner re-invokes a sub-benchmark to grow N, and a cache=shared
// memory DB outlives the run only while a connection is open; a per-store name
// guarantees every newStore call starts from an empty schema.
var benchDBSeq atomic.Uint64

func newSQLiteStore(tb testing.TB) *Store {
	tb.Helper()
	store, err := New(context.Background(), Config{
		Driver: DriverSQLite,
		DSN:    "file:" + sqliteTestName(tb) + "?mode=memory&cache=shared",
	})
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() {
		if err := store.Close(); err != nil {
			tb.Errorf("close sqlite store: %v", err)
		}
	})
	return store
}

func sqliteTestName(tb testing.TB) string {
	return strings.NewReplacer("/", "_", " ", "_").Replace(tb.Name())
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

func TestSQLiteStoreConformance(t *testing.T) {
	storetest.RunStoreConformance(t, func(tb testing.TB) lease.Store {
		return newSQLiteStore(tb)
	})
}

func BenchmarkSQLiteStore(b *testing.B) {
	storetest.RunStoreBenchmarks(b, func(tb testing.TB) lease.Store {
		store, err := New(context.Background(), Config{
			Driver: DriverSQLite,
			DSN:    fmt.Sprintf("file:bench-%d?mode=memory&cache=shared", benchDBSeq.Add(1)),
		})
		if err != nil {
			tb.Fatal(err)
		}
		tb.Cleanup(func() {
			if err := store.Close(); err != nil {
				tb.Errorf("close sqlite store: %v", err)
			}
		})
		return store
	})
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

func TestMySQLDialectCreateUsesDuplicateKeyErrorForConflict(t *testing.T) {
	t.Parallel()

	d, err := dialectFor(DriverMySQL)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(d.insertSQL, "ON DUPLICATE KEY") {
		t.Fatalf("insertSQL = %q, want plain insert so duplicate creates return a driver error", d.insertSQL)
	}
	if d.duplicateKey == nil {
		t.Fatal("mysql dialect must classify duplicate-key errors")
	}
	if !d.duplicateKey(&mysqldriver.MySQLError{Number: 1062}) {
		t.Fatal("duplicate key error was not classified as conflict")
	}
	if d.duplicateKey(&mysqldriver.MySQLError{Number: 1048}) {
		t.Fatal("non-duplicate mysql error was classified as conflict")
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

func TestSQLiteStorePutCASRequiresMatchingVersion(t *testing.T) {
	t.Parallel()

	store := newSQLiteStore(t)
	first := sampleRecord()
	if err := store.Put(context.Background(), 0, first); err != nil {
		t.Fatal(err)
	}

	next := *first
	next.Holder = "cluster-west"
	next.FencingToken = 2
	if err := store.Put(context.Background(), 99, &next); !errors.Is(err, lease.ErrConflict) {
		t.Fatalf("stale CAS err = %v, want ErrConflict", err)
	}

	if err := store.Put(context.Background(), 1, &next); err != nil {
		t.Fatalf("matching CAS: %v", err)
	}
	got, err := store.Get(context.Background(), next.Key)
	if err != nil {
		t.Fatal(err)
	}
	if got.Holder != "cluster-west" || got.FencingToken != 2 {
		t.Fatalf("got holder/token = %s/%d, want cluster-west/2", got.Holder, got.FencingToken)
	}
	if got.Version != 2 {
		t.Fatalf("Version after update = %d, want 2", got.Version)
	}
}

// TestSQLiteStoreMigratesLegacySchemaInPlace covers upgrade-in-place: a
// database created from the pre-version schema gains the version column via
// migrate=auto, its legacy rows read as Version 1, and the alteration is
// idempotent across repeated opens.
func TestSQLiteStoreMigratesLegacySchemaInPlace(t *testing.T) {
	t.Parallel()

	dsn := "file:" + sqliteTestName(t) + "?mode=memory&cache=shared"

	// Build the legacy schema (no version column) and one legacy row via a
	// raw connection that stays open to keep the shared-memory DB alive.
	raw, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	if _, err := raw.Exec(`CREATE TABLE berth_leases (
		namespace text NOT NULL, name text NOT NULL, holder text NOT NULL,
		ttl_ms integer NOT NULL, acquired_at text NOT NULL, renewed_at text NOT NULL,
		fencing_token integer NOT NULL, PRIMARY KEY (namespace, name))`); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}
	if _, err := raw.Exec(
		`INSERT INTO berth_leases VALUES ('tenant-a', 'ingest', 'cluster-east', 30000, ?, ?, 3)`,
		"2026-05-24T10:30:00Z", "2026-05-24T10:30:00Z"); err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}

	for range 2 { // second open proves the alteration is idempotent
		store, err := New(context.Background(), Config{Driver: DriverSQLite, DSN: dsn})
		if err != nil {
			t.Fatalf("open with migrate=auto over legacy schema: %v", err)
		}
		got, err := store.Get(context.Background(), lease.Key{Namespace: "tenant-a", Name: "ingest"})
		if err != nil {
			t.Fatal(err)
		}
		if got.Version != 1 {
			t.Fatalf("legacy row Version = %d, want 1", got.Version)
		}
		if got.FencingToken != 3 {
			t.Fatalf("legacy row token = %d, want 3", got.FencingToken)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
	}

	// The first post-upgrade CAS at version 1 succeeds exactly once.
	store, err := New(context.Background(), Config{Driver: DriverSQLite, DSN: dsn})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	got, err := store.Get(context.Background(), lease.Key{Namespace: "tenant-a", Name: "ingest"})
	if err != nil {
		t.Fatal(err)
	}
	upd := *got
	upd.RenewedAt = got.RenewedAt.Add(time.Minute)
	if err := store.Put(context.Background(), 1, &upd); err != nil {
		t.Fatalf("first post-upgrade CAS: %v", err)
	}
	if err := store.Put(context.Background(), 1, &upd); !errors.Is(err, lease.ErrConflict) {
		t.Fatalf("second CAS at version 1 = %v, want ErrConflict", err)
	}
}

func TestSQLiteStorePutCASOnAbsentReturnsConflict(t *testing.T) {
	t.Parallel()

	store := newSQLiteStore(t)
	if err := store.Put(context.Background(), 1, sampleRecord()); !errors.Is(err, lease.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
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
