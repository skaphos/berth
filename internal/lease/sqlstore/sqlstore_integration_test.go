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
