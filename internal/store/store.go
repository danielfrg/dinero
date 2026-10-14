package store

import (
	"context"
	"database/sql"
	"net/url"
	"path/filepath"
	"strings"
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
	// Keep the legacy default compatible with PostgreSQL installations whose
	// timezone data omits IANA backward aliases.
	if timezone == "US/Central" {
		timezone = "America/Chicago"
	}
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
	} else {
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
		if readOnly {
			query.Set("mode", "ro")
			query.Add("_pragma", "query_only(1)")
		} else {
			query.Set("_txlock", "immediate")
			query.Add("_pragma", "busy_timeout(5000)")
		}
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
func (s *Store) readTx(parent context.Context) (*sql.Tx, context.Context, context.CancelFunc, error) {
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		cancel()
		return nil, nil, nil, &ledger.ErrUnavailable{}
	}
	return tx, ctx, cancel, nil
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
