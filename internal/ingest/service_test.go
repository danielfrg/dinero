package ingest_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/danielfrg/dinero/internal/config"
	"github.com/danielfrg/dinero/internal/importcsv"
	"github.com/danielfrg/dinero/internal/ingest"
	"github.com/danielfrg/dinero/internal/plaid"
	"github.com/danielfrg/dinero/internal/store"
	"github.com/danielfrg/dinero/internal/testutil"
)

type fakeProvider struct {
	pages      map[int]plaid.TransactionsPage
	failOffset int
	failErr    error
	calls      []int
}

func (f *fakeProvider) GetTransactions(_ context.Context, _ string, start, end time.Time, offset int) (plaid.TransactionsPage, error) {
	f.calls = append(f.calls, offset)
	if start.Format("2006-01-02") != "2026-07-16" || end.Format("2006-01-02") != "2026-10-14" {
		return plaid.TransactionsPage{}, errors.New("wrong default range")
	}
	if f.failOffset == offset {
		if f.failErr != nil {
			return plaid.TransactionsPage{}, f.failErr
		}
		return plaid.TransactionsPage{}, errors.New("provider failed")
	}
	return f.pages[offset], nil
}

func TestPagesPendingAndRepeat(t *testing.T) {
	t.Setenv("DINERO_STATE_DIR", t.TempDir())
	for _, fx := range []*testutil.Fixture{testutil.NewSQLite(t), testutil.NewPostgres(t)} {
		s, err := store.Open(context.Background(), fx.DSN, false, "UTC")
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		provider := &fakeProvider{failOffset: -1, pages: map[int]plaid.TransactionsPage{
			0: {Total: 3, Transactions: []plaid.Transaction{{AccountID: "a", Date: "2026-10-14", Name: "Coffee", Amount: 4.25}, {AccountID: "a", Date: "2026-10-14", Name: "Pending", Amount: 1, Pending: true}}},
			2: {Total: 3, Transactions: []plaid.Transaction{{AccountID: "a", Date: "2026-10-13", Name: "Lunch", Amount: 9}}},
		}}
		cfg := config.Config{Timezone: "UTC", Plaid: config.Plaid{Tokens: map[string]string{"bank": "synthetic"}, AccountIDToName: map[string]string{"a": "Checking"}}}
		svc := ingest.New(s, cfg, provider)
		preview, err := svc.Preview(context.Background(), 90, "2026-10-14")
		if err != nil {
			t.Fatal(err)
		}
		if preview.NewTransactions != 2 || preview.PendingTransactions != 1 || len(provider.calls) != 2 {
			t.Fatalf("preview %+v calls %v", preview, provider.calls)
		}
		var n int
		if err := fx.DB.QueryRow("SELECT count(*) FROM transactions").Scan(&n); err != nil || n != 0 {
			t.Fatalf("preview wrote: %d %v", n, err)
		}
		done, err := svc.Commit(context.Background(), preview)
		if err != nil || done.NewTransactions != 2 {
			t.Fatalf("commit %+v %v", done, err)
		}
		_, err = svc.Commit(context.Background(), preview)
		if err != nil {
			t.Fatal(err)
		}
		if err := fx.DB.QueryRow("SELECT count(*) FROM transactions").Scan(&n); err != nil || n != 2 {
			t.Fatalf("repeat: %d %v", n, err)
		}
	}
}

func TestProviderFailureAndStoppedPagination(t *testing.T) {
	t.Setenv("DINERO_STATE_DIR", t.TempDir())
	fx := testutil.NewSQLite(t)
	s, err := store.Open(context.Background(), fx.DSN, false, "UTC")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cfg := config.Config{Timezone: "UTC", Plaid: config.Plaid{Tokens: map[string]string{"bank": "synthetic"}}}
	p := &fakeProvider{failOffset: 1, pages: map[int]plaid.TransactionsPage{0: {Total: 2, Transactions: []plaid.Transaction{{AccountID: "unmapped", Date: "2026-10-14", Name: "A", Amount: 1}}}}}
	svc := ingest.New(s, cfg, p)
	if _, err := svc.Preview(context.Background(), 90, "2026-10-14"); err == nil {
		t.Fatal("expected page failure")
	}
	p.failOffset = -1
	if _, err := svc.Preview(context.Background(), 90, "2026-10-14"); err == nil {
		t.Fatal("expected pagination stall")
	}
	var n int
	fx.DB.QueryRow("SELECT count(*) FROM transactions").Scan(&n)
	if n != 0 {
		t.Fatal(n)
	}
}

func TestChangedLoginNamesAliasWithoutWriting(t *testing.T) {
	t.Setenv("DINERO_STATE_DIR", t.TempDir())
	fx := testutil.NewSQLite(t)
	s, err := store.Open(context.Background(), fx.DSN, false, "UTC")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p := &fakeProvider{failOffset: 0, failErr: errors.New("the login details of this item have changed")}
	cfg := config.Config{Timezone: "UTC", Plaid: config.Plaid{Tokens: map[string]string{"bank": "synthetic"}}}
	_, err = ingest.New(s, cfg, p).Preview(context.Background(), 90, "2026-10-14")
	if err == nil || !strings.Contains(err.Error(), "Credentials expired for account: bank") || !strings.Contains(err.Error(), "dinero link --update bank") {
		t.Fatalf("changed-login error %v", err)
	}
}

func TestCommitRollsBackWhenSecondInsertFails(t *testing.T) {
	t.Setenv("DINERO_STATE_DIR", t.TempDir())
	fx := testutil.NewSQLite(t)
	if _, err := fx.DB.Exec("CREATE TRIGGER stop_lunch BEFORE INSERT ON transactions WHEN NEW.description = 'Lunch' BEGIN SELECT RAISE(ABORT, 'synthetic failure'); END"); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(context.Background(), fx.DSN, false, "UTC")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p := &fakeProvider{failOffset: -1, pages: map[int]plaid.TransactionsPage{0: {Total: 2, Transactions: []plaid.Transaction{{AccountID: "a", Date: "2026-10-14", Name: "Coffee", Amount: 4.25}, {AccountID: "a", Date: "2026-10-14", Name: "Lunch", Amount: 9}}}}}
	cfg := config.Config{Timezone: "UTC", Plaid: config.Plaid{Tokens: map[string]string{"bank": "synthetic"}, AccountIDToName: map[string]string{"a": "Checking"}}}
	svc := ingest.New(s, cfg, p)
	preview, err := svc.Preview(context.Background(), 90, "2026-10-14")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Commit(context.Background(), preview); err == nil {
		t.Fatal("expected insert failure")
	}
	var n int
	if err := fx.DB.QueryRow("SELECT COUNT(*) FROM transactions").Scan(&n); err != nil || n != 0 {
		t.Fatalf("rollback rows %d %v", n, err)
	}
}

func TestConcurrentCSVAndPlaidWritersDeduplicate(t *testing.T) {
	t.Setenv("DINERO_STATE_DIR", t.TempDir())
	fx := testutil.NewSQLite(t)
	s, err := store.Open(context.Background(), fx.DSN, false, "UTC")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cfg := config.Config{Timezone: "UTC", Plaid: config.Plaid{Tokens: map[string]string{"bank": "synthetic"}, AccountIDToName: map[string]string{"a": "Checking"}}}
	p := &fakeProvider{failOffset: -1, pages: map[int]plaid.TransactionsPage{0: {Total: 1, Transactions: []plaid.Transaction{{AccountID: "a", Date: "2026-10-14", Name: "Coffee", Amount: 4.25}}}}}
	ingestSvc := ingest.New(s, cfg, p)
	preview, err := ingestSvc.Preview(context.Background(), 90, "2026-10-14")
	if err != nil {
		t.Fatal(err)
	}
	csvSvc := importcsv.New(s, cfg)
	csvPreview, err := csvSvc.Preview(context.Background(), strings.NewReader("date,description,amount\n2026-10-14,Coffee,4.25\n"), "synthetic.csv", "Checking")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() { defer wg.Done(); _, e := ingestSvc.Commit(context.Background(), preview); errs <- e }()
	go func() { defer wg.Done(); _, e := csvSvc.Commit(context.Background(), csvPreview); errs <- e }()
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var n int
	if e := fx.DB.QueryRow("SELECT COUNT(*) FROM transactions").Scan(&n); e != nil || n != 1 {
		t.Fatalf("concurrent rows %d %v", n, e)
	}
}

func TestPostgresMatchingUsesRawUTCCalendarDay(t *testing.T) {
	t.Setenv("DINERO_STATE_DIR", t.TempDir())
	fx := testutil.NewPostgres(t)
	fx.InsertNullable(t, "2026-09-01 04:59:59+00:00", "ExistingUTC", "", "", 4.25, "Checking", "kept")
	fx.InsertNullable(t, "2026-09-01 04:59:59+00:00", "NewLocal", "", "", 9.00, "Checking", "kept")
	s, err := store.Open(context.Background(), fx.DSN, false, "US/Central")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	localized, err := s.Transactions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := localized[0].Date.Format("2006-01-02"); got != "2026-08-31" {
		t.Fatalf("query date localization changed: %s", got)
	}
	cfg := config.Config{Timezone: "US/Central", Plaid: config.Plaid{Tokens: map[string]string{"bank": "synthetic"}, AccountIDToName: map[string]string{"a": "Checking"}}}
	p := &fakeProvider{failOffset: -1, pages: map[int]plaid.TransactionsPage{0: {Total: 2, Transactions: []plaid.Transaction{
		{AccountID: "a", Date: "2026-09-01", Name: "ExistingUTC", Amount: 4.25},
		{AccountID: "a", Date: "2026-08-31", Name: "NewLocal", Amount: 9.00},
	}}}}
	svc := ingest.New(s, cfg, p)
	preview, err := svc.Preview(context.Background(), 90, "2026-10-14")
	if err != nil {
		t.Fatal(err)
	}
	if preview.ExistingTransactions != 1 || preview.NewTransactions != 1 || len(preview.Transactions) != 1 || preview.Transactions[0].Description != "NewLocal" {
		t.Fatalf("wrong raw-day preview %+v", preview)
	}
	done, err := svc.Commit(context.Background(), preview)
	if err != nil {
		t.Fatal(err)
	}
	if done.ExistingTransactions != 1 || done.NewTransactions != 1 || done.Transactions[0].Description != "NewLocal" {
		t.Fatalf("wrong raw-day commit %+v", done)
	}
	var existingUTC, newLocal int
	if err := fx.DB.QueryRow("SELECT count(*) FROM transactions WHERE description='ExistingUTC'").Scan(&existingUTC); err != nil {
		t.Fatal(err)
	}
	if err := fx.DB.QueryRow("SELECT count(*) FROM transactions WHERE description='NewLocal'").Scan(&newLocal); err != nil {
		t.Fatal(err)
	}
	if existingUTC != 1 || newLocal != 2 {
		t.Fatalf("unexpected persisted multiplicity UTC=%d local=%d", existingUTC, newLocal)
	}
}

func TestBrokenRulesAbortPreviewAndCommit(t *testing.T) {
	cases := []struct {
		name      string
		breakFile func(*testing.T, string)
	}{
		{"malformed", func(t *testing.T, path string) {
			t.Helper()
			if err := os.WriteFile(path, []byte(`{"Coffee":`), 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{"wrong-pair", func(t *testing.T, path string) {
			t.Helper()
			if err := os.WriteFile(path, []byte(`{"Coffee":["Food"]}`), 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{"read-error", func(t *testing.T, path string) {
			t.Helper()
			if err := os.Mkdir(path, 0700); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, dialect := range []string{"sqlite", "postgres"} {
				t.Run(dialect, func(t *testing.T) {
					state := t.TempDir()
					t.Setenv("DINERO_STATE_DIR", state)
					var fx *testutil.Fixture
					if dialect == "postgres" {
						fx = testutil.NewPostgres(t)
					} else {
						fx = testutil.NewSQLite(t)
					}
					s, err := store.Open(context.Background(), fx.DSN, false, "UTC")
					if err != nil {
						t.Fatal(err)
					}
					defer s.Close()
					cfg := config.Config{Timezone: "UTC", Plaid: config.Plaid{Tokens: map[string]string{"bank": "synthetic"}, AccountIDToName: map[string]string{"a": "Checking"}}}
					p := &fakeProvider{failOffset: -1, pages: map[int]plaid.TransactionsPage{0: {Total: 1, Transactions: []plaid.Transaction{{AccountID: "a", Date: "2026-10-14", Name: "Coffee", Amount: 4.25}}}}}
					svc := ingest.New(s, cfg, p)
					path := filepath.Join(state, "category_rules.json")
					tc.breakFile(t, path)
					if _, err := svc.Preview(context.Background(), 90, "2026-10-14"); err == nil || !strings.Contains(err.Error(), "category rules") {
						t.Fatalf("expected rules preview failure, got %v", err)
					}
					if err := os.RemoveAll(path); err != nil {
						t.Fatal(err)
					}
					preview, err := svc.Preview(context.Background(), 90, "2026-10-14")
					if err != nil {
						t.Fatal(err)
					}
					tc.breakFile(t, path)
					if _, err := svc.Commit(context.Background(), preview); err == nil || !strings.Contains(err.Error(), "category rules") {
						t.Fatalf("expected rules commit failure, got %v", err)
					}
					var n int
					if err := fx.DB.QueryRow("SELECT count(*) FROM transactions").Scan(&n); err != nil || n != 0 {
						t.Fatalf("rules failure wrote %d rows: %v", n, err)
					}
				})
			}
		})
	}
}
