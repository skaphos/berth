package sqlstore

import (
	"database/sql"
	"fmt"
	"time"
)

const columns = "namespace, name, holder, ttl_ms, acquired_at, renewed_at, fencing_token"

type dialect struct {
	driverName   string
	schema       []string
	getSQL       string
	listSQL      string
	insertSQL    string
	updateSQL    string
	deleteSQL    string
	timeValue    func(time.Time) any
	readTx       *sql.TxOptions
	writeTx      *sql.TxOptions
	duplicateKey func(error) bool
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
	PRIMARY KEY (namespace, name)
)`},
			getSQL:    "SELECT " + columns + " FROM berth_leases WHERE namespace = $1 AND name = $2",
			listSQL:   "SELECT " + columns + " FROM berth_leases",
			insertSQL: "INSERT INTO berth_leases (" + columns + ") VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT (namespace, name) DO NOTHING",
			updateSQL: "UPDATE berth_leases SET holder = $1, ttl_ms = $2, acquired_at = $3, renewed_at = $4, fencing_token = $5 WHERE namespace = $6 AND name = $7 AND fencing_token = $8",
			deleteSQL: "DELETE FROM berth_leases WHERE namespace = $1 AND name = $2 AND fencing_token = $3",
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
	PRIMARY KEY (namespace, name)
) ENGINE=InnoDB`},
			getSQL:    "SELECT " + columns + " FROM berth_leases WHERE namespace = ? AND name = ?",
			listSQL:   "SELECT " + columns + " FROM berth_leases",
			insertSQL: "INSERT INTO berth_leases (" + columns + ") VALUES (?, ?, ?, ?, ?, ?, ?)",
			updateSQL: "UPDATE berth_leases SET holder = ?, ttl_ms = ?, acquired_at = ?, renewed_at = ?, fencing_token = ? WHERE namespace = ? AND name = ? AND fencing_token = ?",
			deleteSQL: "DELETE FROM berth_leases WHERE namespace = ? AND name = ? AND fencing_token = ?",
			timeValue: sqlTimeValue,
			readTx: &sql.TxOptions{
				Isolation: sql.LevelReadCommitted,
				ReadOnly:  true,
			},
			writeTx:      &sql.TxOptions{Isolation: sql.LevelReadCommitted},
			duplicateKey: isMySQLDuplicateKey,
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
	PRIMARY KEY (namespace, name)
)`},
			getSQL:    "SELECT " + columns + " FROM berth_leases WHERE namespace = ? AND name = ?",
			listSQL:   "SELECT " + columns + " FROM berth_leases",
			insertSQL: "INSERT OR IGNORE INTO berth_leases (" + columns + ") VALUES (?, ?, ?, ?, ?, ?, ?)",
			updateSQL: "UPDATE berth_leases SET holder = ?, ttl_ms = ?, acquired_at = ?, renewed_at = ?, fencing_token = ? WHERE namespace = ? AND name = ? AND fencing_token = ?",
			deleteSQL: "DELETE FROM berth_leases WHERE namespace = ? AND name = ? AND fencing_token = ?",
			timeValue: sqliteTimeValue,
			readTx:    &sql.TxOptions{ReadOnly: true},
		}, nil
	default:
		return dialect{}, fmt.Errorf("sql store: driver must be one of %q, %q, %q; got %q",
			DriverPostgres, DriverMySQL, DriverSQLite, driver)
	}
}

func sqlTimeValue(t time.Time) any {
	return t.UTC().Truncate(time.Microsecond)
}

func sqliteTimeValue(t time.Time) any {
	return t.UTC().Format(time.RFC3339Nano)
}
