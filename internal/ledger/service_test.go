package ledger_test

import (
	"context"
	"errors"
	"testing"

	"github.com/danielfrg/dinero/internal/config"
	"github.com/danielfrg/dinero/internal/ledger"
)

type stubRepo struct {
	rows     []ledger.Transaction
	accounts []ledger.Account
}

func (r stubRepo) Search(context.Context, ledger.Query) (ledger.Page, error) {
	return ledger.Page{Transactions: r.rows}, nil
}
func (r stubRepo) Accounts(context.Context) ([]ledger.Account, error) { return r.accounts, nil }
func (r stubRepo) Categories(context.Context) (ledger.Categories, error) {
	return ledger.Categories{}, nil
}
func (r stubRepo) Balance(context.Context, string, string) (ledger.Balance, error) {
	return ledger.Balance{Amount: "2.225"}, nil
}
func (r stubRepo) Summary(context.Context, ledger.SummaryQuery) (ledger.Summary, error) {
	return ledger.Summary{}, nil
}

func TestFormatMoneyHalfEven(t *testing.T) {
	for _, tc := range []struct{ in, want string }{{"2.225", "2.22"}, {"2.235", "2.24"}, {"-2.225", "-2.22"}, {"0", "0.00"}} {
		got, err := ledger.FormatMoney(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("%s: %q %v, want %q", tc.in, got, err, tc.want)
		}
	}
}

func TestServiceValidatesQueriesAndKnowsConfiguredAccounts(t *testing.T) {
	s := ledger.NewService(stubRepo{}, config.Config{Timezone: "UTC", Plaid: config.Plaid{AccountIDToName: map[string]string{"one": "Known"}}})
	for _, q := range []ledger.Query{{Sort: "bad", Limit: 50}, {Month: ptr(13), Limit: 50}, {Limit: -1}, {Limit: 50, Offset: -1}} {
		if _, err := s.Search(context.Background(), q); err == nil {
			t.Errorf("query %+v accepted", q)
		}
	}
	b, err := s.Balance(context.Background(), "Known", "2026-03-08")
	if err != nil || b.Amount != "2.22" {
		t.Fatalf("balance %+v %v", b, err)
	}
	_, err = s.Balance(context.Background(), "Missing", "2026-03-08")
	var unknown *ledger.ErrUnknownAccount
	if !errors.As(err, &unknown) {
		t.Fatalf("unknown account: %v", err)
	}
}
func ptr(v int) *int { return &v }
