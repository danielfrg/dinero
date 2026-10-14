package importcsv_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/danielfrg/dinero/internal/config"
	"github.com/danielfrg/dinero/internal/importcsv"
	"github.com/danielfrg/dinero/internal/store"
	"github.com/danielfrg/dinero/internal/testutil"
)

const oneCSV = "date,description,amount\n2026-09-02,Coffee,4.25\n"

func databases(t *testing.T, run func(*testing.T, *testutil.Fixture)) {
	t.Run("sqlite", func(t *testing.T) { run(t, testutil.NewSQLite(t)) })
	t.Run("postgres", func(t *testing.T) { run(t, testutil.NewPostgres(t)) })
}
func service(t *testing.T, fx *testutil.Fixture, writable bool) (*importcsv.Service, *store.Store) {
	t.Helper()
	t.Setenv("DINERO_STATE_DIR", t.TempDir())
	t.Setenv("DINERO_DATABASE_URL", "")
	t.Setenv("DINERO_DATABASE_URL_FILE", "")
	os.Unsetenv("DINERO_DATABASE_URL")
	os.Unsetenv("DINERO_DATABASE_URL_FILE")
	s, err := store.Open(context.Background(), fx.DSN, !writable, "America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return importcsv.New(s, config.Config{Timezone: "America/Chicago", Plaid: config.Plaid{AccountIDToName: map[string]string{"fixture": "Test Account"}}}), s
}
func count(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM transactions").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestPreviewAndCommitProvenance(t *testing.T) {
	databases(t, func(t *testing.T, fx *testutil.Fixture) {
		svc, _ := service(t, fx, true)
		p, err := svc.Preview(context.Background(), strings.NewReader(oneCSV), "transactions.csv", "Test Account")
		if err != nil {
			t.Fatal(err)
		}
		if p.Status != "dry-run" || p.NewTransactions != 1 || count(t, fx.DB) != 0 {
			t.Fatalf("preview %+v rows %d", p, count(t, fx.DB))
		}
		done, err := svc.Commit(context.Background(), p)
		if err != nil {
			t.Fatal(err)
		}
		if done.Status != "imported" || count(t, fx.DB) != 1 {
			t.Fatalf("commit %+v rows %d", done, count(t, fx.DB))
		}
		var notes string
		var amount float64
		if err := fx.DB.QueryRow("SELECT notes,amount FROM transactions").Scan(&notes, &amount); err != nil {
			t.Fatal(err)
		}
		if notes != "csv-import:transactions.csv" || amount != 4.25 {
			t.Fatalf("notes %q amount %g", notes, amount)
		}
	})
}
func TestDuplicateAndAmountValidation(t *testing.T) {
	databases(t, func(t *testing.T, fx *testutil.Fixture) {
		svc, _ := service(t, fx, true)
		cases := []struct{ csv, want string }{
			{"date,description,amount\n2026-09-02,Coffee,4.25\n2026-09-02,Coffee,4.25\n", "Duplicate CSV transactions: rows 2, 3"},
			{"date,description,amount\n2026-09-02,Coffee,4.251\n", "Expected at most two decimals"},
			{"date,description,amount\n2026-09-02,Coffee,\"1,2\"\n", "Invalid amount: '1,2'"},
			{"date,description,amount\n2026-09-02,Coffee,999999999999999.99\n", "Cannot preserve cents"},
		}
		for _, tc := range cases {
			_, err := svc.Preview(context.Background(), strings.NewReader(tc.csv), "transactions.csv", "Test Account")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%q: %v", tc.want, err)
			}
		}
		if count(t, fx.DB) != 0 {
			t.Fatal("validation wrote rows")
		}
	})
}
func TestAmbiguousAndUnknownAccount(t *testing.T) {
	databases(t, func(t *testing.T, fx *testutil.Fixture) {
		svc, _ := service(t, fx, true)
		if _, err := svc.Preview(context.Background(), strings.NewReader(oneCSV), "transactions.csv", "Unknown"); err == nil || !strings.Contains(err.Error(), "Unknown account: Unknown") {
			t.Fatalf("unknown: %v", err)
		}
		fx.Insert(t, "2026-09-02 12:00:00", "Coffee", 4.25, "Test Account")
		fx.Insert(t, "2026-09-02 12:00:00", "Coffee", 4.25, "Test Account")
		p, err := svc.Preview(context.Background(), strings.NewReader(oneCSV), "transactions.csv", "Test Account")
		if err != nil {
			t.Fatal(err)
		}
		if p.Status != "ambiguous" || p.AmbiguousTransactions != 1 || len(p.Ambiguous) != 1 || len(p.Ambiguous[0].ExistingIDs) != 2 {
			t.Fatalf("ambiguous %+v", p)
		}
	})
}
func TestConcurrentCommitReclassifies(t *testing.T) {
	databases(t, func(t *testing.T, fx *testutil.Fixture) {
		svc, _ := service(t, fx, true)
		otherStore, err := store.Open(context.Background(), fx.DSN, false, "America/Chicago")
		if err != nil {
			t.Fatal(err)
		}
		defer otherStore.Close()
		other := importcsv.New(otherStore, config.Config{Timezone: "America/Chicago", Plaid: config.Plaid{AccountIDToName: map[string]string{"fixture": "Test Account"}}})
		p, err := svc.Preview(context.Background(), strings.NewReader(oneCSV), "transactions.csv", "Test Account")
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		results := make([]importcsv.Result, 2)
		errs := make([]error, 2)
		services := []*importcsv.Service{svc, other}
		for i := range results {
			wg.Add(1)
			go func(i int) { defer wg.Done(); results[i], errs[i] = services[i].Commit(context.Background(), p) }(i)
		}
		wg.Wait()
		for i, err := range errs {
			if err != nil {
				t.Fatalf("commit %d: %v", i, err)
			}
		}
		if count(t, fx.DB) != 1 {
			t.Fatalf("got %d rows; results %+v", count(t, fx.DB), results)
		}
		if !(results[0].Status == "imported" && results[1].Status == "no-new-transactions" || results[1].Status == "imported" && results[0].Status == "no-new-transactions") {
			t.Fatalf("statuses %+v", results)
		}
	})
}

func TestCompatFixtureAndRulesFallback(t *testing.T) {
	fx := testutil.NewSQLite(t)
	svc, _ := service(t, fx, true)
	input, err := os.ReadFile("../../testdata/compat/import.csv")
	if err != nil {
		t.Fatal(err)
	}
	p, err := svc.Preview(context.Background(), strings.NewReader(string(input)), "import.csv", "Test Account")
	if err != nil {
		t.Fatal(err)
	}
	actual, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.ReadFile("../../testdata/compat/import.json")
	if err != nil {
		t.Fatal(err)
	}
	var got, want any
	if err := json.Unmarshal(actual, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(expected, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("contract %s, want %s", actual, expected)
	}
	if err := os.WriteFile(filepath.Join(config.StateDir(), "category_rules.json"), []byte(`{"Coffee":["Food","Cafe"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	p, err = svc.Preview(context.Background(), strings.NewReader(oneCSV), "import.csv", "Test Account")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Transactions) != 1 || p.Transactions[0].Category != "Food" || p.Transactions[0].Subcategory != "Cafe" {
		t.Fatalf("rules %+v", p.Transactions)
	}
}

func TestCommitReclassifiesNewAmbiguity(t *testing.T) {
	databases(t, func(t *testing.T, fx *testutil.Fixture) {
		svc, _ := service(t, fx, true)
		p, err := svc.Preview(context.Background(), strings.NewReader(oneCSV), "transactions.csv", "Test Account")
		if err != nil {
			t.Fatal(err)
		}
		fx.Insert(t, "2026-09-02 12:00:00", "Coffee", 4.25, "Test Account")
		fx.Insert(t, "2026-09-02 12:00:00", "Coffee", 4.25, "Test Account")
		done, err := svc.Commit(context.Background(), p)
		if err != nil {
			t.Fatal(err)
		}
		if done.Status != "ambiguous" || done.AmbiguousTransactions != 1 || count(t, fx.DB) != 2 {
			t.Fatalf("reclassified %+v rows %d", done, count(t, fx.DB))
		}
	})
}
