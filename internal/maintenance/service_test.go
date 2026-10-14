package maintenance_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danielfrg/dinero/internal/config"
	"github.com/danielfrg/dinero/internal/maintenance"
	"github.com/danielfrg/dinero/internal/store"
	"github.com/danielfrg/dinero/internal/testutil"
)

func TestRulesCacheExport(t *testing.T) {
	for _, fx := range []*testutil.Fixture{testutil.NewSQLite(t), testutil.NewPostgres(t)} {
		state := t.TempDir()
		t.Setenv("DINERO_STATE_DIR", state)
		for i := 0; i < 3; i++ {
			fx.InsertNullable(t, "2026-09-01 08:00:00", "Coffee", "Food", "Cafe", 4.25, "Checking", "")
		}
		fx.InsertNullable(t, "2026-09-01 08:00:00", "Coffee", "Other", "Other", 3.0, "Checking", "")
		s, err := store.Open(context.Background(), fx.DSN, false, "UTC")
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		svc := maintenance.New(s, config.Config{Timezone: "UTC"})
		rules, err := svc.GenerateRules(context.Background())
		if err != nil || len(rules) != 1 || rules["Coffee"] != [2]string{"Food", "Cafe"} {
			t.Fatalf("rules %v %v", rules, err)
		}
		stats, err := svc.PreviewRules(context.Background())
		if err != nil || len(stats) != 1 || stats[0].Amount != 3 {
			t.Fatalf("rule counts %+v %v", stats, err)
		}
		if err := svc.BuildCache(context.Background()); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(filepath.Join(state, "cache", "accounts.json"))
		if err != nil {
			t.Fatal(err)
		}
		var accounts []string
		if err := json.Unmarshal(raw, &accounts); err != nil || len(accounts) != 1 || accounts[0] != "Checking" {
			t.Fatalf("accounts %s %v", raw, err)
		}
		dir := t.TempDir()
		if err := svc.Export(context.Background(), dir); err != nil {
			t.Fatal(err)
		}
		exported, err := sql.Open("sqlite", filepath.Join(dir, "transactions.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer exported.Close()
		var n int
		if err := exported.QueryRow("SELECT count(*) FROM transactions").Scan(&n); err != nil || n != 4 {
			t.Fatalf("export rows %d %v", n, err)
		}
	}
}

func TestInitSchemaIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.sqlite")
	s, err := store.Open(context.Background(), path, false, "UTC")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	svc := maintenance.New(s, config.Config{Timezone: "UTC"})
	if err := svc.InitDB(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := svc.InitDB(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestExportPreservesNullsAndRawUTC(t *testing.T) {
	for _, fx := range []*testutil.Fixture{testutil.NewSQLite(t), testutil.NewPostgres(t)} {
		table := "transactions"
		if strings.HasPrefix(fx.DSN, "postgres") {
			table = "public.transactions"
		}
		if _, err := fx.DB.Exec("INSERT INTO "+table+" (date,description,category,subcategory,amount,notes,account) VALUES ("+func() string {
			if table == "transactions" {
				return "?,?,?,?,?,?,?"
			}
			return "$1,$2,$3,$4,$5,$6,$7"
		}()+")", "2026-09-01 05:00:00", nil, nil, nil, nil, nil, nil); err != nil {
			t.Fatal(err)
		}
		s, err := store.Open(context.Background(), fx.DSN, false, "US/Central")
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		svc := maintenance.New(s, config.Config{Timezone: "US/Central"})
		dir := t.TempDir()
		if err := svc.Export(context.Background(), dir); err != nil {
			t.Fatal(err)
		}
		csv, err := os.ReadFile(filepath.Join(dir, "all.csv"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(csv), "2026-09-01 05:00:00") || strings.Contains(string(csv), "2026-09-01 00:00:00") {
			t.Fatalf("timezone changed: %s", csv)
		}
		db, err := sql.Open("sqlite", filepath.Join(dir, "transactions.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		var desc, cat, sub, notes, account sql.NullString
		var amount sql.NullFloat64
		if err := db.QueryRow("SELECT description,category,subcategory,amount,notes,account FROM transactions").Scan(&desc, &cat, &sub, &amount, &notes, &account); err != nil {
			t.Fatal(err)
		}
		if desc.Valid || cat.Valid || sub.Valid || amount.Valid || notes.Valid || account.Valid {
			t.Fatal("null fields coalesced")
		}
		var exportedDate string
		if err := db.QueryRow("SELECT CAST(date AS TEXT) FROM transactions").Scan(&exportedDate); err != nil {
			t.Fatal(err)
		}
		if exportedDate != "2026-09-01 05:00:00.000000" {
			t.Fatalf("exported date %q", exportedDate)
		}
	}
}

func TestCSVFloatKeepsDecimalPoint(t *testing.T) {
	fx := testutil.NewSQLite(t)
	fx.InsertNullable(t, "2026-09-01 08:00:00", "Salary", "Income", "Pay", -100.0, "Checking", "")
	s, err := store.Open(context.Background(), fx.DSN, false, "UTC")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	dir := t.TempDir()
	if err := maintenance.New(s, config.Config{Timezone: "UTC"}).Export(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(filepath.Join(dir, "all.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), "-100.0") {
		t.Fatalf("CSV amount %s", text)
	}
}
