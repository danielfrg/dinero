package maintenance_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/url"
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
		categoriesRaw, err := os.ReadFile(filepath.Join(state, "cache", "categories.json"))
		if err != nil {
			t.Fatal(err)
		}
		var categories struct {
			Categories    []string                                 `json:"categories"`
			Subcategories []string                                 `json:"subcategories"`
			Pairs         []struct{ Category, Subcategory string } `json:"category_subcategory_pairs"`
		}
		if err := json.Unmarshal(categoriesRaw, &categories); err != nil || len(categories.Categories) != 2 || len(categories.Subcategories) != 2 || len(categories.Pairs) != 2 {
			t.Fatalf("category cache %s %v", categoriesRaw, err)
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
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 2 {
			t.Fatalf("export temp files leaked: %v %v", entries, err)
		}
		csvText, err := os.ReadFile(filepath.Join(dir, "all.csv"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(string(csvText), "id,date,description,category,subcategory,amount,notes,account\n") {
			t.Fatalf("CSV columns %s", csvText)
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
		if strings.HasPrefix(fx.DSN, "postgres") && !strings.Contains(string(csv), "2026-09-01 05:00:00+00:00") {
			t.Fatalf("PostgreSQL CSV offset lost: %s", csv)
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

func TestExportFailurePreservesPreviousArtifacts(t *testing.T) {
	for _, tc := range []struct {
		name    string
		fixture func(*testing.T) *testutil.Fixture
	}{{"sqlite", func(t *testing.T) *testutil.Fixture { return testutil.NewSQLite(t) }}, {"postgres", func(t *testing.T) *testutil.Fixture { return testutil.NewPostgres(t) }}} {
		t.Run(tc.name, func(t *testing.T) {
			fx := tc.fixture(t)
			fx.InsertNullable(t, "2026-09-01 08:00:00", "Coffee", "Food", "Cafe", 4.25, "Checking", "")
			s, err := store.Open(context.Background(), fx.DSN, false, "UTC")
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			dir := t.TempDir()
			csvPath := filepath.Join(dir, "all.csv")
			dbPath := filepath.Join(dir, "transactions.db")
			if err := os.WriteFile(csvPath, []byte("previous CSV\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(dbPath, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dbPath, "previous"), []byte("previous DB"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := maintenance.New(s, config.Config{Timezone: "UTC"}).Export(context.Background(), dir); err == nil {
				t.Fatal("expected invalid destination failure")
			}
			got, err := os.ReadFile(csvPath)
			if err != nil || string(got) != "previous CSV\n" {
				t.Fatalf("previous CSV changed %q %v", got, err)
			}
			got, err = os.ReadFile(filepath.Join(dbPath, "previous"))
			if err != nil || string(got) != "previous DB" {
				t.Fatalf("previous database changed %q %v", got, err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 2 {
				t.Fatalf("temporary files leaked: %v %v", entries, err)
			}
		})
	}
}

func TestExportRejectsSQLiteSourceAliasesBeforePublication(t *testing.T) {
	for _, kind := range []string{"direct", "relative", "uri", "relative-uri", "directory-symlink", "hardlink-db", "hardlink-csv", "source-csv"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "transactions.db")
			if kind == "source-csv" {
				source = filepath.Join(dir, "all.csv")
			}
			db, err := sql.Open("sqlite", source)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err = db.Exec("CREATE TABLE transactions (id INTEGER PRIMARY KEY AUTOINCREMENT,date DATETIME,description TEXT,category TEXT,subcategory TEXT,amount FLOAT,notes TEXT,account TEXT); INSERT INTO transactions VALUES (1,'2026-09-01','source','','',1,'','Checking')"); err != nil {
				t.Fatal(err)
			}
			output := dir
			dsn := source
			switch kind {
			case "relative", "relative-uri":
				dsn, err = filepath.Rel(func() string { p, _ := os.Getwd(); return p }(), source)
				if kind == "relative-uri" {
					dsn = "file:" + dsn + "?mode=ro"
				}
			case "uri":
				dsn = (&url.URL{Scheme: "file", Path: source, RawQuery: "mode=ro"}).String()
			case "directory-symlink":
				output = filepath.Join(t.TempDir(), "alias")
				err = os.Symlink(dir, output)
			case "hardlink-db", "hardlink-csv":
				output = t.TempDir()
				target := "transactions.db"
				if kind == "hardlink-csv" {
					target = "all.csv"
				}
				err = os.Link(source, filepath.Join(output, target))
			}
			if err != nil {
				t.Fatal(err)
			}
			previous := filepath.Join(output, "all.csv")
			if kind == "hardlink-csv" || kind == "source-csv" {
				previous = filepath.Join(output, "transactions.db")
			}
			if previous != source {
				if err := os.WriteFile(previous, []byte("previous export"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.ReadFile(source)
			if err != nil {
				t.Fatal(err)
			}
			prior, err := os.ReadFile(previous)
			if err != nil {
				t.Fatal(err)
			}
			st, err := store.Open(context.Background(), dsn, true, "UTC")
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			// Deliberately conflicting config: the actual opened store is authoritative.
			err = maintenance.New(st, config.Config{Timezone: "UTC", Database: config.Database{ConnectionString: "sqlite:///unused.db"}}).Export(context.Background(), output)
			if err == nil || !strings.Contains(err.Error(), "source") {
				t.Fatalf("source export accepted: %v", err)
			}
			after, err := os.ReadFile(source)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("source bytes changed", err)
			}
			after, err = os.ReadFile(previous)
			if err != nil || !bytes.Equal(prior, after) {
				t.Fatal("previous export changed", err)
			}
			var schema string
			if err := db.QueryRow("SELECT sql FROM sqlite_master WHERE name='transactions'").Scan(&schema); err != nil || !strings.Contains(schema, "PRIMARY KEY AUTOINCREMENT") {
				t.Fatal("schema changed", schema, err)
			}
			var count int
			if err := db.QueryRow("SELECT count(*) FROM transactions WHERE id=1 AND description='source'").Scan(&count); err != nil || count != 1 {
				t.Fatal("rows changed", count, err)
			}
			entries, err := os.ReadDir(output)
			if err != nil || len(entries) != 2 {
				t.Fatal("export artifacts created", entries, err)
			}
		})
	}
}
