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
	runSQLStoreConformance(t, DriverPostgres, dsn)
}

func TestMariaDBStoreConformance(t *testing.T) {
	dsn := integrationDSN(t, mysqlDSNEnv)
	runSQLStoreConformance(t, DriverMySQL, dsn)
}

func integrationDSN(t *testing.T, env string) string {
	t.Helper()
	dsn := os.Getenv(env)
	if dsn == "" {
		t.Skipf("set %s to run this integration test", env)
	}
	return dsn
}

func runSQLStoreConformance(t *testing.T, driver, dsn string) {
	t.Helper()
	storetest.RunStoreConformance(t, func(tb testing.TB) lease.Store {
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
	})
}
