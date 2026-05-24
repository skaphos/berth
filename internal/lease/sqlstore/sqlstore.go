// Package sqlstore implements lease.Store with database/sql.
package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/skaphos/berth/internal/lease"
	_ "modernc.org/sqlite"
)

const (
	DriverPostgres = "postgres"
	DriverMySQL    = "mysql"
	DriverSQLite   = "sqlite"

	MigrateAuto = "auto"
	MigrateOff  = "off"
)

// Config configures a SQL Store.
type Config struct {
	Driver  string
	DSN     string
	Migrate string
}

// Store persists lease records in a SQL database.
type Store struct {
	db      *sql.DB
	dialect dialect
}

// New opens a SQL-backed lease store and applies the schema when configured.
func New(ctx context.Context, cfg Config) (*Store, error) {
	d, err := dialectFor(cfg.Driver)
	if err != nil {
		return nil, err
	}
	if cfg.DSN == "" {
		return nil, errors.New("sql store: dsn is required")
	}
	migrate := cfg.Migrate
	if migrate == "" {
		migrate = MigrateAuto
	}
	if migrate != MigrateAuto && migrate != MigrateOff {
		return nil, fmt.Errorf("sql store: migrate must be %q or %q", MigrateAuto, MigrateOff)
	}

	db, err := sql.Open(d.driverName, cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("sql store: open %s: %w", cfg.Driver, err)
	}
	if cfg.Driver == DriverSQLite {
		// Keeps :memory: databases coherent and avoids concurrent-writer
		// surprises. SQLite remains ACID, but it is still a single-writer
		// backend and not suitable for multi-replica HA API servers.
		db.SetMaxOpenConns(1)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sql store: ping %s: %w", cfg.Driver, err)
	}

	s := &Store{db: db, dialect: d}
	if migrate == MigrateAuto {
		if err := s.migrate(ctx); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	return s, nil
}

// Close closes the underlying database handle.
func (s *Store) Close() error {
	return s.db.Close()
}

// Get implements lease.Store.
func (s *Store) Get(ctx context.Context, key lease.Key) (*lease.Record, error) {
	tx, err := s.db.BeginTx(ctx, s.dialect.readTx)
	if err != nil {
		return nil, fmt.Errorf("sql store: begin get: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	rec, err := s.getTx(ctx, tx, key)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("sql store: commit get %s: %w", key, err)
	}
	return rec, nil
}

// List implements lease.Store.
func (s *Store) List(ctx context.Context) ([]lease.Record, error) {
	tx, err := s.db.BeginTx(ctx, s.dialect.readTx)
	if err != nil {
		return nil, fmt.Errorf("sql store: begin list: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, s.dialect.listSQL)
	if err != nil {
		return nil, fmt.Errorf("sql store: list: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []lease.Record
	for rows.Next() {
		rec, err := scanRecord(rows)
		if err != nil {
			return nil, fmt.Errorf("sql store: scan list: %w", err)
		}
		out = append(out, *rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sql store: list rows: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("sql store: commit list: %w", err)
	}
	return out, nil
}

// Put implements lease.Store.
func (s *Store) Put(ctx context.Context, expected int32, rec *lease.Record) error {
	if rec == nil {
		return lease.ErrConflict
	}
	tx, err := s.db.BeginTx(ctx, s.dialect.writeTx)
	if err != nil {
		return fmt.Errorf("sql store: begin put: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var res sql.Result
	if expected == 0 {
		res, err = tx.ExecContext(ctx, s.dialect.insertSQL, s.recordArgs(rec)...)
		if err != nil {
			if s.dialect.duplicateKey != nil && s.dialect.duplicateKey(err) {
				return lease.ErrConflict
			}
			return fmt.Errorf("sql store: insert %s: %w", rec.Key, err)
		}
	} else {
		args := append(s.recordUpdateArgs(rec), rec.Key.Namespace, rec.Key.Name, expected)
		res, err = tx.ExecContext(ctx, s.dialect.updateSQL, args...)
		if err != nil {
			return fmt.Errorf("sql store: update %s: %w", rec.Key, err)
		}
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sql store: rows affected %s: %w", rec.Key, err)
	}
	if expected == 0 {
		if affected != 1 {
			return lease.ErrConflict
		}
	} else if affected == 0 {
		return lease.ErrConflict
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sql store: commit put %s: %w", rec.Key, err)
	}
	return nil
}

func isMySQLDuplicateKey(err error) bool {
	var mysqlErr *mysqldriver.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}

// Delete implements lease.Store.
func (s *Store) Delete(ctx context.Context, key lease.Key, expected int32) error {
	tx, err := s.db.BeginTx(ctx, s.dialect.writeTx)
	if err != nil {
		return fmt.Errorf("sql store: begin delete: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, s.dialect.deleteSQL, key.Namespace, key.Name, expected)
	if err != nil {
		return fmt.Errorf("sql store: delete %s: %w", key, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sql store: rows affected delete %s: %w", key, err)
	}
	if affected == 0 {
		if _, err := s.getTx(ctx, tx, key); err != nil {
			if errors.Is(err, lease.ErrNotFound) {
				return lease.ErrNotFound
			}
			return err
		}
		return lease.ErrConflict
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sql store: commit delete %s: %w", key, err)
	}
	return nil
}

func (s *Store) migrate(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sql store: begin migrate: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, stmt := range s.dialect.schema {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("sql store: migrate: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sql store: commit migrate: %w", err)
	}
	return nil
}

func (s *Store) getTx(ctx context.Context, tx *sql.Tx, key lease.Key) (*lease.Record, error) {
	row := tx.QueryRowContext(ctx, s.dialect.getSQL, key.Namespace, key.Name)
	rec, err := scanRecord(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, lease.ErrNotFound
		}
		return nil, fmt.Errorf("sql store: get %s: %w", key, err)
	}
	return rec, nil
}

func (s *Store) recordArgs(rec *lease.Record) []any {
	return []any{
		rec.Key.Namespace,
		rec.Key.Name,
		rec.Holder,
		rec.TTL.Milliseconds(),
		s.dialect.timeValue(rec.AcquiredAt),
		s.dialect.timeValue(rec.RenewedAt),
		rec.FencingToken,
	}
}

func (s *Store) recordUpdateArgs(rec *lease.Record) []any {
	return []any{
		rec.Holder,
		rec.TTL.Milliseconds(),
		s.dialect.timeValue(rec.AcquiredAt),
		s.dialect.timeValue(rec.RenewedAt),
		rec.FencingToken,
	}
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRecord(row rowScanner) (*lease.Record, error) {
	var (
		namespace string
		name      string
		holder    string
		ttlMS     int64
		acquired  any
		renewed   any
		token     int32
	)
	if err := row.Scan(&namespace, &name, &holder, &ttlMS, &acquired, &renewed, &token); err != nil {
		return nil, err
	}
	acquiredAt, err := parseTime(acquired)
	if err != nil {
		return nil, fmt.Errorf("parse acquired_at: %w", err)
	}
	renewedAt, err := parseTime(renewed)
	if err != nil {
		return nil, fmt.Errorf("parse renewed_at: %w", err)
	}
	return &lease.Record{
		Key:          lease.Key{Namespace: namespace, Name: name},
		Holder:       holder,
		TTL:          time.Duration(ttlMS) * time.Millisecond,
		AcquiredAt:   acquiredAt,
		RenewedAt:    renewedAt,
		FencingToken: token,
	}, nil
}

func parseTime(v any) (time.Time, error) {
	switch t := v.(type) {
	case time.Time:
		return t.UTC(), nil
	case string:
		return parseTimeString(t)
	case []byte:
		return parseTimeString(string(t))
	default:
		return time.Time{}, fmt.Errorf("unsupported value %T", v)
	}
}

func parseTimeString(s string) (time.Time, error) {
	layouts := []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05.999999Z07:00",
		"2006-01-02 15:04:05.999Z07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05.999999",
		"2006-01-02 15:04:05.999",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported format %q", s)
}

var _ lease.Store = (*Store)(nil)
