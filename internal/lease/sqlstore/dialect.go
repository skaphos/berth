package sqlstore

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const columns = "namespace, name, holder, ttl_ms, acquired_at, renewed_at, fencing_token, version"

type dialect struct {
	driverName string
	schema     []string
	// alterations are additive, idempotent-by-tolerance statements applied
	// after schema, upgrading tables created before a column existed. A
	// failure recognized by duplicateColumn means the column is already
	// present and is not an error.
	alterations     []string
	getSQL          string
	listSQL         string
	insertSQL       string
	updateSQL       string
	timeValue       func(time.Time) any
	readTx          *sql.TxOptions
	writeTx         *sql.TxOptions
	duplicateKey    func(error) bool
	duplicateColumn func(error) bool
}

func dialectFor(driver string) (dialect, error) {
	switch driver {
	case DriverPostgres:
		return dialect{
			driverName: "pgx",
			schema: []string{`
CREATE TABLE IF NOT EXISTS berth_leases (
	namespace text NOT NULL,
	name text NOT NULL,
	holder text NOT NULL,
	ttl_ms bigint NOT NULL,
	acquired_at timestamptz NOT NULL,
	renewed_at timestamptz NOT NULL,
	fencing_token integer NOT NULL,
	version bigint NOT NULL DEFAULT 1,
	PRIMARY KEY (namespace, name)
)`},
			alterations: []string{
				"ALTER TABLE berth_leases ADD COLUMN IF NOT EXISTS version bigint NOT NULL DEFAULT 1",
			},
			getSQL:    "SELECT " + columns + " FROM berth_leases WHERE namespace = $1 AND name = $2",
			listSQL:   "SELECT " + columns + " FROM berth_leases",
			insertSQL: "INSERT INTO berth_leases (" + columns + ") VALUES ($1, $2, $3, $4, $5, $6, $7, 1) ON CONFLICT (namespace, name) DO NOTHING",
			updateSQL: "UPDATE berth_leases SET holder = $1, ttl_ms = $2, acquired_at = $3, renewed_at = $4, fencing_token = $5, version = version + 1 WHERE namespace = $6 AND name = $7 AND version = $8",
			timeValue: sqlTimeValue,
			readTx:    &sql.TxOptions{ReadOnly: true},
		}, nil
	case DriverMySQL:
		return dialect{
			driverName: "mysql",
			schema: []string{`
CREATE TABLE IF NOT EXISTS berth_leases (
	namespace varchar(253) NOT NULL,
	name varchar(253) NOT NULL,
	holder varchar(253) NOT NULL,
	ttl_ms bigint NOT NULL,
	acquired_at datetime(6) NOT NULL,
	renewed_at datetime(6) NOT NULL,
	fencing_token int NOT NULL,
	version bigint NOT NULL DEFAULT 1,
	PRIMARY KEY (namespace, name)
) ENGINE=InnoDB`},
			alterations: []string{
				// MySQL has no ADD COLUMN IF NOT EXISTS; a duplicate-column
				// error (1060) from a pre-upgraded table is tolerated.
				"ALTER TABLE berth_leases ADD COLUMN version bigint NOT NULL DEFAULT 1",
			},
			getSQL:    "SELECT " + columns + " FROM berth_leases WHERE namespace = ? AND name = ?",
			listSQL:   "SELECT " + columns + " FROM berth_leases",
			insertSQL: "INSERT INTO berth_leases (" + columns + ") VALUES (?, ?, ?, ?, ?, ?, ?, 1)",
			updateSQL: "UPDATE berth_leases SET holder = ?, ttl_ms = ?, acquired_at = ?, renewed_at = ?, fencing_token = ?, version = version + 1 WHERE namespace = ? AND name = ? AND version = ?",
			timeValue: sqlTimeValue,
			readTx: &sql.TxOptions{
				Isolation: sql.LevelReadCommitted,
				ReadOnly:  true,
			},
			writeTx:         &sql.TxOptions{Isolation: sql.LevelReadCommitted},
			duplicateKey:    isMySQLDuplicateKey,
			duplicateColumn: isMySQLDuplicateColumn,
		}, nil
	case DriverSQLite:
		return dialect{
			driverName: "sqlite",
			schema: []string{`
CREATE TABLE IF NOT EXISTS berth_leases (
	namespace text NOT NULL,
	name text NOT NULL,
	holder text NOT NULL,
	ttl_ms integer NOT NULL,
	acquired_at text NOT NULL,
	renewed_at text NOT NULL,
	fencing_token integer NOT NULL,
	version integer NOT NULL DEFAULT 1,
	PRIMARY KEY (namespace, name)
)`},
			alterations: []string{
				// SQLite has no ADD COLUMN IF NOT EXISTS; the duplicate-column
				// error from a pre-upgraded table is tolerated.
				"ALTER TABLE berth_leases ADD COLUMN version integer NOT NULL DEFAULT 1",
			},
			getSQL:          "SELECT " + columns + " FROM berth_leases WHERE namespace = ? AND name = ?",
			listSQL:         "SELECT " + columns + " FROM berth_leases",
			insertSQL:       "INSERT OR IGNORE INTO berth_leases (" + columns + ") VALUES (?, ?, ?, ?, ?, ?, ?, 1)",
			updateSQL:       "UPDATE berth_leases SET holder = ?, ttl_ms = ?, acquired_at = ?, renewed_at = ?, fencing_token = ?, version = version + 1 WHERE namespace = ? AND name = ? AND version = ?",
			timeValue:       sqliteTimeValue,
			readTx:          &sql.TxOptions{ReadOnly: true},
			duplicateColumn: isSQLiteDuplicateColumn,
		}, nil
	default:
		return dialect{}, fmt.Errorf("sql store: driver must be one of %q, %q, %q; got %q",
			DriverPostgres, DriverMySQL, DriverSQLite, driver)
	}
}

func isSQLiteDuplicateColumn(err error) bool {
	return err != nil && strings.Contains(err.Error(), "duplicate column name")
}

func sqlTimeValue(t time.Time) any {
	return t.UTC().Truncate(time.Microsecond)
}

func sqliteTimeValue(t time.Time) any {
	return t.UTC().Format(time.RFC3339Nano)
}
