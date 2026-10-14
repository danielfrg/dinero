package ledger_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/danielfrg/dinero/internal/testutil"
	"math"
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

func TestRichBalanceNonfiniteRetainsCompleteness(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
			t.Run(dialect+"/"+fmt.Sprint(value), func(t *testing.T) {
				var fx *testutil.Fixture
				if dialect == "sqlite" {
					fx = testutil.NewSQLite(t)
				} else {
					fx = testutil.NewPostgres(t)
				}
				fx.Insert(t, "2026-03-08 12:00:00", "nonfinite", value, "Checking")
				svc := ledger.NewService(fx.Open(t, "UTC"), config.Config{Timezone: "UTC"})
				b, err := svc.Balance(context.Background(), "Checking", "2026-03-08")
				want := ""
				if dialect == "sqlite" && math.IsNaN(value) {
					want = "0.00"
				} // SQLite stores NaN as NULL.
				if err != nil || b.Amount != want || b.Account != "Checking" || b.Through != "2026-03-08" || b.IncompleteAmountCount != 1 {
					t.Fatalf("nonfinite balance %+v %v", b, err)
				}
			})
		}
	}
}

func TestSQLiteOpposingInfinitiesRichBalanceIsNull(t *testing.T) {
	fx := testutil.NewSQLite(t)
	fx.Insert(t, "2026-03-08 12:00:00", "positive", math.Inf(1), "Checking")
	fx.Insert(t, "2026-03-08 12:00:00", "negative", math.Inf(-1), "Checking")
	svc := ledger.NewService(fx.Open(t, "UTC"), config.Config{Timezone: "UTC"})
	got, err := svc.Balance(context.Background(), "Checking", "2026-03-08")
	if err != nil || got.Amount != "" || got.IncompleteAmountCount != 2 {
		t.Fatalf("opposing infinities fabricated total %+v %v", got, err)
	}
	legacy, err := svc.BalanceLegacy(context.Background(), "Checking", "2026-03-08")
	if err != nil || legacy.Amount != "0.00" {
		t.Fatalf("legacy SQL COALESCE changed %+v %v", legacy, err)
	}
}
