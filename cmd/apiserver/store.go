package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/skaphos/berth/internal/k8s"
	"github.com/skaphos/berth/internal/lease"
	"github.com/skaphos/berth/internal/lease/sqlstore"
)

const (
	storeBackendUnset = ""
	storeBackendMem   = "mem"
	storeBackendK8s   = "k8s"
	storeBackendSQL   = "sql"

	sqlDriverPostgres = "postgres"
	sqlDriverMySQL    = "mysql"
	sqlDriverSQLite   = "sqlite"

	sqlMigrateAuto = "auto"
	sqlMigrateOff  = "off"

	sqlStoreStartupTimeout = 30 * time.Second
)

// storeConfig groups all flag values that feed the lease.Store factory.
type storeConfig struct {
	backend                string
	coordinationKubeconfig string
	coordinationNamespace  string
	coordinationQPS        float64
	coordinationBurst      int
	sqlDriver              string
	sqlDSN                 string
	sqlDSNFile             string
	sqlMigrate             string
}

// resolveStoreBackend returns the explicit backend when --store-backend was
// set, validating the value. When the flag is omitted, it falls back to the
// legacy heuristic (empty --coordination-namespace → mem, set → k8s) and logs
// a deprecation warning. The implicit fallback is scheduled for removal one
// release after the SQL backend ships.
func resolveStoreBackend(cfg storeConfig) (string, error) {
	if cfg.backend != storeBackendUnset {
		switch cfg.backend {
		case storeBackendMem, storeBackendK8s, storeBackendSQL:
			return cfg.backend, nil
		default:
			return "", fmt.Errorf("--store-backend must be one of %q, %q, %q; got %q",
				storeBackendMem, storeBackendK8s, storeBackendSQL, cfg.backend)
		}
	}
	inferred := storeBackendMem
	if cfg.coordinationNamespace != "" {
		inferred = storeBackendK8s
	}
	slog.Warn("--store-backend not set; falling back to legacy heuristic based on --coordination-namespace. "+
		"Implicit selection is DEPRECATED and will be removed one release after the SQL backend ships. "+
		"Set --store-backend explicitly.",
		"inferred", inferred)
	return inferred, nil
}

// validateStoreConfig rejects flag combinations that mix backends or omit
// required per-backend inputs.
func validateStoreConfig(backend string, cfg storeConfig) error {
	hasCoordinationFlags := cfg.coordinationNamespace != "" || cfg.coordinationKubeconfig != ""
	hasSQLFlags := cfg.sqlDriver != "" || cfg.sqlDSN != "" || cfg.sqlDSNFile != "" || cfg.sqlMigrate != ""

	switch backend {
	case storeBackendMem:
		if hasCoordinationFlags {
			return errors.New("--coordination-namespace and --coordination-kubeconfig are only valid with --store-backend=k8s")
		}
		if hasSQLFlags {
			return errors.New("--sql-* flags are only valid with --store-backend=sql")
		}
	case storeBackendK8s:
		if cfg.coordinationNamespace == "" {
			return errors.New("--coordination-namespace is required when --store-backend=k8s")
		}
		if hasSQLFlags {
			return errors.New("--sql-* flags are only valid with --store-backend=sql")
		}
	case storeBackendSQL:
		if hasCoordinationFlags {
			return errors.New("--coordination-* flags are only valid with --store-backend=k8s")
		}
		switch cfg.sqlDriver {
		case sqlDriverPostgres, sqlDriverMySQL, sqlDriverSQLite:
		case "":
			return errors.New("--sql-driver is required when --store-backend=sql")
		default:
			return fmt.Errorf("--sql-driver must be one of %q, %q, %q; got %q",
				sqlDriverPostgres, sqlDriverMySQL, sqlDriverSQLite, cfg.sqlDriver)
		}
		switch {
		case cfg.sqlDSN == "" && cfg.sqlDSNFile == "":
			return errors.New("one of --sql-dsn or --sql-dsn-file is required when --store-backend=sql")
		case cfg.sqlDSN != "" && cfg.sqlDSNFile != "":
			return errors.New("--sql-dsn and --sql-dsn-file are mutually exclusive")
		}
		switch cfg.sqlMigrate {
		case "", sqlMigrateAuto, sqlMigrateOff:
		default:
			return fmt.Errorf("--sql-migrate must be one of %q, %q; got %q",
				sqlMigrateAuto, sqlMigrateOff, cfg.sqlMigrate)
		}
	default:
		return fmt.Errorf("internal error: unknown backend %q", backend)
	}
	return nil
}

// buildStore constructs the lease.Store for the chosen backend. The caller is
// expected to have already invoked validateStoreConfig.
func buildStore(ctx context.Context, backend string, cfg storeConfig) (lease.Store, error) {
	switch backend {
	case storeBackendMem:
		slog.Warn("running with in-memory lease store; state will not survive restart, do not use in production")
		return lease.NewMemStore(), nil
	case storeBackendK8s:
		clientset, err := k8s.NewClientset(cfg.coordinationKubeconfig, k8s.ClientConfig{
			QPS:   float32(cfg.coordinationQPS),
			Burst: cfg.coordinationBurst,
		})
		if err != nil {
			return nil, err
		}
		return lease.NewK8sLeaseStore(clientset, cfg.coordinationNamespace)
	case storeBackendSQL:
		dsn, err := resolveSQLDSN(cfg)
		if err != nil {
			return nil, err
		}
		migrate := cfg.sqlMigrate
		if migrate == "" {
			migrate = sqlstore.MigrateAuto
		}
		if cfg.sqlDriver == sqlDriverSQLite {
			slog.Warn("running with sqlite lease store; SQLite is durable and ACID, but single-writer only and not suitable for multi-replica HA API servers")
		}
		startupCtx, cancel := context.WithTimeout(ctx, sqlStoreStartupTimeout)
		defer cancel()
		return sqlstore.New(startupCtx, sqlstore.Config{
			Driver:  cfg.sqlDriver,
			DSN:     dsn,
			Migrate: migrate,
		})
	default:
		return nil, fmt.Errorf("internal error: unknown backend %q", backend)
	}
}

func resolveSQLDSN(cfg storeConfig) (string, error) {
	if cfg.sqlDSN != "" {
		return cfg.sqlDSN, nil
	}
	data, err := os.ReadFile(cfg.sqlDSNFile)
	if err != nil {
		return "", fmt.Errorf("read --sql-dsn-file: %w", err)
	}
	dsn := strings.TrimSpace(string(data))
	if dsn == "" {
		return "", errors.New("--sql-dsn-file is empty")
	}
	return dsn, nil
}
