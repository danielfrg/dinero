package testutil

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danielfrg/dinero/internal/store"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

type Fixture struct {
	DSN      string
	DB       *sql.DB
	postgres bool
}

func NewSQLite(t testing.TB) *Fixture {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "ledger.sqlite")
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	fx := &Fixture{DSN: dsn, DB: db}
	fx.createSchema(t)
	return fx
}
func NewPostgres(t testing.TB) *Fixture {
	t.Helper()
	adminDSN := os.Getenv("DINERO_TEST_POSTGRES_DSN")
	if adminDSN == "" {
		t.Skip("DINERO_TEST_POSTGRES_DSN unset")
	}
	admin, err := sql.Open("pgx", adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	if err = admin.Ping(); err != nil {
		admin.Close()
		t.Fatalf("test PostgreSQL unavailable: %v", err)
	}
	u, err := url.Parse(adminDSN)
	if err != nil {
		admin.Close()
		t.Fatal(err)
	}
	name := "dinero_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec("CREATE DATABASE " + name); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	u.Path = "/" + name
	dsn := u.String()
	var db *sql.DB
	t.Cleanup(func() {
		if db != nil {
			db.Close()
		}
		if _, err := admin.Exec("DROP DATABASE " + name + " WITH (FORCE)"); err != nil {
			t.Errorf("drop test database: %v", err)
		}
		admin.Close()
	})
	db, err = sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	fx := &Fixture{DSN: dsn, DB: db, postgres: true}
	fx.createSchema(t)
	return fx
}
func (fx *Fixture) createSchema(t testing.TB) {
	t.Helper()
	dateType := "DATETIME"
	idType := "INTEGER PRIMARY KEY AUTOINCREMENT"
	amountType := "DOUBLE"
	table := "transactions"
	if fx.postgres {
		dateType = "TIMESTAMP WITH TIME ZONE"
		idType = "SERIAL PRIMARY KEY"
		amountType = "DOUBLE PRECISION"
		table = "public.transactions"
	}
	stmt := fmt.Sprintf("CREATE TABLE %s (id %s, date %s NULL, description VARCHAR(1000) NULL, category VARCHAR(50) NULL, subcategory VARCHAR(50) NULL, amount %s NULL, notes VARCHAR(1000) NULL, account VARCHAR(50) NULL)", table, idType, dateType, amountType)
	if _, err := fx.DB.Exec(stmt); err != nil {
		t.Fatal(err)
	}
}
func (fx *Fixture) Insert(t testing.TB, date, description string, amount any, account string) {
	t.Helper()
	fx.InsertNullable(t, date, description, "", "", amount, account, "test")
}
func (fx *Fixture) InsertNullable(t testing.TB, date, description, category, subcategory string, amount any, account, notes string) {
	t.Helper()
	table := "transactions"
	args := []any{date, description, category, subcategory, amount, account, notes}
	placeholders := "?,?,?,?,?,?,?"
	if fx.postgres {
		table = "public.transactions"
		placeholders = "$1,$2,$3,$4,$5,$6,$7"
	}
	stmt := "INSERT INTO " + table + " (date,description,category,subcategory,amount,account,notes) VALUES (" + placeholders + ")"
	if date == "" {
		args[0] = nil
	}
	if _, err := fx.DB.Exec(stmt, args...); err != nil {
		t.Fatal(err)
	}
}
func (fx *Fixture) Open(t testing.TB, timezone string) *store.Store {
	t.Helper()
	s, err := store.Open(context.Background(), fx.DSN, true, timezone)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
