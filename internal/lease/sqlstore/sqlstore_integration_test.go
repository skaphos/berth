//go:build integration

package sqlstore

import (
	"context"
	"os"
	"testing"

	"github.com/skaphos/berth/internal/lease"
	"github.com/skaphos/berth/internal/lease/storetest"
)

const (
	postgresDSNEnv = "BERTH_TEST_POSTGRES_DSN"
	mysqlDSNEnv    = "BERTH_TEST_MYSQL_DSN"
)

func TestPostgresStoreConformance(t *testing.T) {
	dsn := integrationDSN(t, postgresDSNEnv)
	storetest.RunStoreConformance(t, integrationStoreFactory(DriverPostgres, dsn))
}

func TestMariaDBStoreConformance(t *testing.T) {
	dsn := integrationDSN(t, mysqlDSNEnv)
	storetest.RunStoreConformance(t, integrationStoreFactory(DriverMySQL, dsn))
}

func BenchmarkPostgresStore(b *testing.B) {
	dsn := integrationDSN(b, postgresDSNEnv)
	storetest.RunStoreBenchmarks(b, integrationStoreFactory(DriverPostgres, dsn))
}

func BenchmarkMariaDBStore(b *testing.B) {
	dsn := integrationDSN(b, mysqlDSNEnv)
	storetest.RunStoreBenchmarks(b, integrationStoreFactory(DriverMySQL, dsn))
}

func TestPostgresStoreMigratesLegacySchemaInPlace(t *testing.T) {
	dsn := integrationDSN(t, postgresDSNEnv)
	testMigratesLegacySchemaInPlace(t, DriverPostgres, dsn, `CREATE TABLE berth_leases (
		namespace text NOT NULL, name text NOT NULL, holder text NOT NULL,
		ttl_ms bigint NOT NULL, acquired_at timestamptz NOT NULL, renewed_at timestamptz NOT NULL,
		fencing_token integer NOT NULL, PRIMARY KEY (namespace, name))`,
		`INSERT INTO berth_leases VALUES ('tenant-a', 'ingest', 'cluster-east', 30000, now(), now(), 3)`)
}

func TestMariaDBStoreMigratesLegacySchemaInPlace(t *testing.T) {
	dsn := integrationDSN(t, mysqlDSNEnv)
	testMigratesLegacySchemaInPlace(t, DriverMySQL, dsn, `CREATE TABLE berth_leases (
		namespace varchar(253) NOT NULL, name varchar(253) NOT NULL, holder varchar(253) NOT NULL,
		ttl_ms bigint NOT NULL, acquired_at datetime(6) NOT NULL, renewed_at datetime(6) NOT NULL,
		fencing_token int NOT NULL, PRIMARY KEY (namespace, name)) ENGINE=InnoDB`,
		`INSERT INTO berth_leases VALUES ('tenant-a', 'ingest', 'cluster-east', 30000, NOW(6), NOW(6), 3)`)
}

// testMigratesLegacySchemaInPlace rebuilds the pre-version schema, seeds a
// legacy row, and verifies migrate=auto adds the version column
// idempotently, reads legacy rows as Version 1, and CASes them exactly once.
func testMigratesLegacySchemaInPlace(t *testing.T, driver, dsn, legacySchema, legacyInsert string) {
	t.Helper()
	ctx := context.Background()

	setup, err := New(ctx, Config{Driver: driver, DSN: dsn, Migrate: MigrateOff})
	if err != nil {
		t.Fatalf("open setup store: %v", err)
	}
	if _, err := setup.db.ExecContext(ctx, "DROP TABLE IF EXISTS berth_leases"); err != nil {
		_ = setup.Close()
		t.Fatalf("drop table: %v", err)
	}
	if _, err := setup.db.ExecContext(ctx, legacySchema); err != nil {
		_ = setup.Close()
		t.Fatalf("create legacy schema: %v", err)
	}
	if _, err := setup.db.ExecContext(ctx, legacyInsert); err != nil {
		_ = setup.Close()
		t.Fatalf("insert legacy row: %v", err)
	}
	if err := setup.Close(); err != nil {
		t.Fatal(err)
	}

	for range 2 { // second open proves the alteration is idempotent
		store, err := New(ctx, Config{Driver: driver, DSN: dsn})
		if err != nil {
			t.Fatalf("open with migrate=auto over legacy schema: %v", err)
		}
		got, err := store.Get(ctx, lease.Key{Namespace: "tenant-a", Name: "ingest"})
		if err != nil {
			t.Fatal(err)
		}
		if got.Version != 1 || got.FencingToken != 3 {
			t.Fatalf("legacy row version/token = %d/%d, want 1/3", got.Version, got.FencingToken)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
	}

	store, err := New(ctx, Config{Driver: driver, DSN: dsn})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	got, err := store.Get(ctx, lease.Key{Namespace: "tenant-a", Name: "ingest"})
	if err != nil {
		t.Fatal(err)
	}
	upd := *got
	if err := store.Put(ctx, 1, &upd); err != nil {
		t.Fatalf("first post-upgrade CAS: %v", err)
	}
	if err := store.Put(ctx, 1, &upd); err == nil {
		t.Fatal("second CAS at version 1 must conflict")
	}
}

func integrationDSN(tb testing.TB, env string) string {
	tb.Helper()
	dsn := os.Getenv(env)
	if dsn == "" {
		tb.Skipf("set %s to run this integration test", env)
	}
	return dsn
}

// integrationStoreFactory returns a newStore func that opens a fresh store
// against the given DSN and truncates the table so each store starts empty.
func integrationStoreFactory(driver, dsn string) func(testing.TB) lease.Store {
	return func(tb testing.TB) lease.Store {
		tb.Helper()
		store, err := New(context.Background(), Config{
			Driver: driver,
			DSN:    dsn,
		})
		if err != nil {
			tb.Fatalf("new %s store: %v", driver, err)
		}
		if _, err := store.db.ExecContext(context.Background(), "DELETE FROM berth_leases"); err != nil {
			_ = store.Close()
			tb.Fatalf("clear %s store: %v", driver, err)
		}
		tb.Cleanup(func() {
			if err := store.Close(); err != nil {
				tb.Errorf("close %s store: %v", driver, err)
			}
		})
		return store
	}
}
