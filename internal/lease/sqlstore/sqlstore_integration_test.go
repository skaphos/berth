//go:build integration

package sqlstore

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/skaphos/berth/internal/lease"
	"github.com/skaphos/berth/internal/lease/storetest"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestPostgresStoreConformance(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	ctr, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("berth"),
		postgres.WithUsername("berth"),
		postgres.WithPassword("berth"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	testcontainers.CleanupContainer(t, ctr)

	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("postgres dsn: %v", err)
	}
	runSQLStoreConformance(t, DriverPostgres, dsn)
}

func TestMariaDBStoreConformance(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	const (
		user     = "berth"
		password = "berth"
		database = "berth"
	)
	ctr, err := testcontainers.Run(ctx, "mariadb:11.4",
		testcontainers.WithEnv(map[string]string{
			"MARIADB_DATABASE":      database,
			"MARIADB_USER":          user,
			"MARIADB_PASSWORD":      password,
			"MARIADB_ROOT_PASSWORD": "root",
		}),
		testcontainers.WithExposedPorts("3306/tcp"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("ready for connections").WithStartupTimeout(2*time.Minute),
		),
	)
	if err != nil {
		t.Fatalf("start mariadb container: %v", err)
	}
	testcontainers.CleanupContainer(t, ctr)

	endpoint, err := ctr.PortEndpoint(ctx, "3306/tcp", "")
	if err != nil {
		t.Fatalf("mariadb endpoint: %v", err)
	}
	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s?timeout=5s", user, password, endpoint, database)
	runSQLStoreConformance(t, DriverMySQL, dsn)
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
