package store

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/danielfrg/dinero/internal/ledger"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

type Store struct {
	db           *sql.DB
	dialect, dsn string
	loc          *time.Location
	readOnly     bool
}

func Open(ctx context.Context, dsn string, readOnly bool, timezone string) (*Store, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, &ledger.ErrInvalidInput{Field: "timezone", Reason: "unknown timezone"}
	}
	if dsn == "" {
		return nil, &ledger.ErrInvalidInput{Field: "database", Reason: "empty connection"}
	}
	dialect, driverDSN := "sqlite", dsn
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		if _, err = url.Parse(dsn); err != nil {
			return nil, &ledger.ErrInvalidInput{Field: "database", Reason: "invalid URL"}
		}
		dialect = "postgres"
		driverDSN = dsn
	} else if readOnly {
		var sqliteURI *url.URL
		if strings.HasPrefix(dsn, "file:") {
			sqliteURI, err = url.Parse(dsn)
		} else {
			var absolute string
			absolute, err = filepath.Abs(dsn)
			sqliteURI = &url.URL{Scheme: "file", Path: absolute}
		}
		if err != nil {
			return nil, &ledger.ErrInvalidInput{Field: "database", Reason: "invalid SQLite path"}
		}
		query := sqliteURI.Query()
		query.Set("mode", "ro")
		query.Add("_pragma", "query_only(1)")
		sqliteURI.RawQuery = query.Encode()
		driverDSN = sqliteURI.String()
	}
	driver := "sqlite"
	if dialect == "postgres" {
		driver = "pgx"
	}
	db, err := sql.Open(driver, driverDSN)
	if err != nil {
		return nil, &ledger.ErrUnavailable{}
	}
	if dialect == "sqlite" {
		db.SetMaxOpenConns(1)
	}
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, &ledger.ErrUnavailable{}
	}
	return &Store{db: db, dialect: dialect, dsn: dsn, loc: loc, readOnly: readOnly}, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Ping(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return &ledger.ErrUnavailable{}
	}
	return nil
}
func (s *Store) readTx(ctx context.Context) (*sql.Tx, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, &ledger.ErrUnavailable{}
	}
	return tx, nil
}
func (s *Store) WithWriteTx(ctx context.Context, fn func(*sql.Tx) error) error {
	if s.readOnly {
		return errors.New("store is read-only")
	}
	var lock *os.File
	if s.dialect == "sqlite" {
		var err error
		lock, err = os.OpenFile(s.dsn+".lock", os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			return err
		}
		defer lock.Close()
		for {
			err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
			if err == nil {
				break
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			time.Sleep(10 * time.Millisecond)
		}
		defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if s.dialect == "postgres" {
		if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(20261014)"); err != nil {
			return err
		}
	}
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) placeholder(n int) string {
	if s.dialect == "postgres" {
		return "$" + itoa(n)
	}
	return "?"
}
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
