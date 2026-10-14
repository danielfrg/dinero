package ingest_test

import (
	"context"
	"errors"
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
	if err == nil || !strings.Contains(err.Error(), "Credentials expired for account: bank") {
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
