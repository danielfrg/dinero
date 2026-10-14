package maintenance

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// EnsureDatabase creates a missing PostgreSQL database for the explicit
// init-db command. SQLite files are created when the store opens.
func EnsureDatabase(ctx context.Context, dsn string) error {
	if !strings.HasPrefix(dsn, "postgres://") && !strings.HasPrefix(dsn, "postgresql://") {
		return nil
	}
	target, err := sql.Open("pgx", dsn)
	if err != nil {
		return errors.New("cannot open PostgreSQL database")
	}
	pingErr := target.PingContext(ctx)
	target.Close()
	if pingErr == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if !errors.As(pingErr, &pgErr) || pgErr.Code != "3D000" {
		return errors.New("PostgreSQL database unavailable")
	}
	u, err := url.Parse(dsn)
	if err != nil {
		return errors.New("invalid PostgreSQL database URL")
	}
	name := strings.TrimPrefix(u.Path, "/")
	if name == "" || strings.Contains(name, "/") {
		return errors.New("invalid PostgreSQL database name")
	}
	u.Path = "/postgres"
	u.RawPath = ""
	admin, err := sql.Open("pgx", u.String())
	if err != nil {
		return errors.New("cannot open PostgreSQL maintenance database")
	}
	defer admin.Close()
	quoted := `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
	_, err = admin.ExecContext(ctx, "CREATE DATABASE "+quoted)
	if err != nil {
		if errors.As(err, &pgErr) && pgErr.Code == "42P04" {
			return nil
		}
		return fmt.Errorf("cannot create PostgreSQL database: %w", err)
	}
	return nil
}
