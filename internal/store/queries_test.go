package store_test

import (
	"context"
	"database/sql"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/danielfrg/dinero/internal/config"
	"github.com/danielfrg/dinero/internal/ledger"
	"github.com/danielfrg/dinero/internal/store"
	"github.com/danielfrg/dinero/internal/testutil"
)

func TestSQLiteQueries(t *testing.T) {
	fx := testutil.NewSQLite(t)
	fx.Insert(t, "2026-03-08 23:59:59.999999", "closing", 5.25, "Checking")
	fx.Insert(t, "2026-03-09 00:00:00", "next", 7.0, "Checking")
	s := fx.Open(t, "US/Central")
	b, err := s.Balance(context.Background(), "Checking", "2026-03-08")
	if err != nil || b.Amount != "5.25" {
		t.Fatalf("balance %+v %v", b, err)
	}
	before := "2026-03-08"
	p, err := s.Search(context.Background(), ledger.Query{Before: &before, Limit: 50, Sort: "date", Desc: true})
	if err != nil || len(p.Transactions) != 1 || p.Transactions[0].Description != "closing" {
		t.Fatalf("search %+v %v", p, err)
	}
}

func TestSearchOffsetUsesIDTieBreakAndHasMore(t *testing.T) {
	fx := testutil.NewSQLite(t)
	for i := 0; i < 3; i++ {
		fx.Insert(t, "2026-03-08 12:00:00", "same", float64(i), "Checking")
	}
	s := fx.Open(t, "UTC")
	page, err := s.Search(context.Background(), ledger.Query{Sort: "date", Limit: 1, Offset: 1, Desc: true})
	if err != nil || len(page.Transactions) != 1 || page.Transactions[0].ID != 2 || !page.HasMore {
		t.Fatalf("page %+v %v", page, err)
	}
}

func TestSummaryOmitsInvalidAmountsAndIgnoresPageLimit(t *testing.T) {
	for _, kind := range []string{"sqlite", "postgres"} {
		t.Run(kind, func(t *testing.T) {
			var fx *testutil.Fixture
			if kind == "sqlite" {
				fx = testutil.NewSQLite(t)
			} else {
				fx = testutil.NewPostgres(t)
			}
			fx.InsertNullable(t, "2026-03-08 12:00:00", "valid outflow", "Food", "Cafe", 2.25, "Checking", "")
			fx.InsertNullable(t, "2026-03-08 12:00:00", "valid inflow", "Food", "Cafe", -1.00, "Checking", "")
			fx.InsertNullable(t, "2026-03-08 12:00:00", "missing", "Food", "Cafe", nil, "Checking", "")
			fx.InsertNullable(t, "2026-03-08 12:00:00", "subcent", "Food", "Cafe", 0.001, "Checking", "")
			fx.InsertNullable(t, "2026-03-08 12:00:00", "tiny subcent", "Food", "Cafe", 0.000000001, "Checking", "")
			wantIncomplete, wantCount := int64(3), int64(5)
			if kind == "postgres" {
				fx.InsertNullable(t, "2026-03-08 12:00:00", "infinite", "Food", "Cafe", math.Inf(1), "Checking", "")
				wantIncomplete++
				wantCount++
			}
			s := fx.Open(t, "US/Central")
			svc := ledger.NewService(s, config.Config{Timezone: "US/Central"})
			summary, err := svc.Summary(context.Background(), ledger.SummaryQuery{Query: ledger.Query{Limit: 1}, GroupBy: "category"})
			if err != nil {
				t.Fatal(err)
			}
			if summary.IncompleteAmountCount != wantIncomplete || len(summary.Groups) != 1 {
				t.Fatalf("summary %+v", summary)
			}
			g := summary.Groups[0]
			if g.Key != "Food" || g.Count != wantCount || g.SignedTotal != "1.25" || g.Outflows != "2.25" || g.Inflows != "-1.00" {
				t.Fatalf("group %+v", g)
			}
		})
	}
}

func TestEightColumnSchemaAndBudgetProjection(t *testing.T) {
	fx := testutil.NewSQLite(t)
	fx.Insert(t, "2026-03-08 12:00:00", "budget item", 2, "Checking")
	rows, err := fx.DB.Query("PRAGMA table_info(transactions)")
	if err != nil {
		t.Fatal(err)
	}
	var count int
	for rows.Next() {
		count++
	}
	rows.Close()
	if count != 8 {
		t.Fatalf("columns %d, want 8", count)
	}
	var date, description, account string
	var amount float64
	err = fx.DB.QueryRow("SELECT date, description, amount, account FROM transactions WHERE description = ?", "budget item").Scan(&date, &description, &amount, &account)
	if err != nil || description != "budget item" || amount != 2 || account != "Checking" {
		t.Fatalf("projection %s %s %v %s: %v", date, description, amount, account, err)
	}
}

func TestDefaultSummaryAndDiscovery(t *testing.T) {
	for _, kind := range []string{"sqlite", "postgres"} {
		t.Run(kind, func(t *testing.T) {
			var fx *testutil.Fixture
			if kind == "sqlite" {
				fx = testutil.NewSQLite(t)
			} else {
				fx = testutil.NewPostgres(t)
			}
			fx.InsertNullable(t, "2026-03-08 12:00:00", "one", "Food", "Cafe", 4.00, "Checking", "")
			s := fx.Open(t, "US/Central")
			svc := ledger.NewService(s, config.Config{Timezone: "US/Central", Plaid: config.Plaid{AccountIDToName: map[string]string{"configured": "Savings"}, ItemIDs: map[string]string{"item": "secret-id"}, Tokens: map[string]string{"token-only": "secret-token"}}})
			sum, err := svc.Summary(context.Background(), ledger.SummaryQuery{})
			if err != nil || len(sum.Groups) != 1 || sum.Groups[0].SignedTotal != "4.00" {
				t.Fatalf("default summary %+v %v", sum, err)
			}
			accounts, err := svc.Accounts(context.Background())
			if err != nil || len(accounts) != 2 || accounts[0].Name != "Checking" || accounts[1].Name != "Savings" || accounts[1].LatestDate != nil {
				t.Fatalf("accounts %+v %v", accounts, err)
			}
			cats, err := svc.Categories(context.Background())
			if err != nil || len(cats.Categories) != 1 || cats.Categories[0] != "Food" || len(cats.Pairs) != 1 || cats.Pairs[0].Subcategory != "Cafe" {
				t.Fatalf("categories %+v %v", cats, err)
			}
			status, err := svc.Status(context.Background())
			if err != nil || len(status.ItemAliases) != 2 || status.ItemAliases[0] != "item" || status.ItemAliases[1] != "token-only" || len(status.Accounts) != 2 {
				t.Fatalf("status %+v %v", status, err)
			}
		})
	}
}

func TestReadOnlyStoreRejectsWrites(t *testing.T) {
	fx := testutil.NewSQLite(t)
	s := fx.Open(t, "UTC")
	err := s.WithWriteTx(context.Background(), func(tx *sql.Tx) error {
		_, e := tx.Exec("INSERT INTO transactions(description) VALUES ('forbidden')")
		return e
	})
	if err == nil {
		t.Fatal("read-only store accepted write")
	}
	var count int
	if err := fx.DB.QueryRow("SELECT COUNT(*) FROM transactions").Scan(&count); err != nil || count != 0 {
		t.Fatalf("count %d error %v", count, err)
	}
}

func TestReadOnlyOpenDoesNotCreateMissingSQLiteFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing with space.sqlite")
	db, err := store.Open(context.Background(), path, true, "UTC")
	if err == nil {
		db.Close()
		t.Fatal("read-only open accepted missing database")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("read-only open created file or stat failed: %v", err)
	}
}

func TestReadOnlyOpenUsesEscapedExistingSQLitePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing with space.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("CREATE TABLE transactions(id INTEGER PRIMARY KEY, date DATETIME)"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()
	s, err := store.Open(context.Background(), path, true, "UTC")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
}
