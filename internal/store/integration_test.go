package store_test

import (
	"context"
	"math"
	"testing"

	"github.com/danielfrg/dinero/internal/ledger"
	"github.com/danielfrg/dinero/internal/testutil"
)

func TestPostgresQueries(t *testing.T) {
	fx := testutil.NewPostgres(t)
	fx.Insert(t, "2026-03-09T04:59:59Z", "local closing", 5.25, "Checking")
	fx.Insert(t, "2026-03-09T05:00:00Z", "next local day", 7.0, "Checking")
	s := fx.Open(t, "US/Central")
	b, err := s.Balance(context.Background(), "Checking", "2026-03-08")
	if err != nil || b.Amount != "5.25" {
		t.Fatalf("balance %+v %v", b, err)
	}
	before := "2026-03-08"
	p, err := s.Search(context.Background(), ledger.Query{Before: &before, Limit: 50, Sort: "date"})
	if err != nil || len(p.Transactions) != 1 || p.Transactions[0].Description != "local closing" {
		t.Fatalf("search %+v %v", p, err)
	}
}

func TestPostgresBalanceCountsNonfiniteWithoutChangingSQLSum(t *testing.T) {
	fx := testutil.NewPostgres(t)
	fx.Insert(t, "2026-03-08 12:00:00", "infinite", math.Inf(1), "Checking")
	fx.InsertNullable(t, "2026-03-08 12:00:00", "nan", "", "", math.NaN(), "Checking", "")
	s := fx.Open(t, "UTC")
	b, err := s.Balance(context.Background(), "Checking", "2026-03-08")
	if err != nil || b.IncompleteAmountCount != 2 || b.Amount != "NaN" {
		t.Fatalf("balance %+v %v", b, err)
	}
}

func TestPostgresYearAndMonthUseConfiguredTimezone(t *testing.T) {
	fx := testutil.NewPostgres(t)
	fx.Insert(t, "2027-01-01T05:59:59Z", "local 2026", 1, "Checking")
	fx.Insert(t, "2027-01-01T06:00:00Z", "local 2027", 1, "Checking")
	s := fx.Open(t, "US/Central")
	year, month := 2026, 12
	page, err := s.Search(context.Background(), ledger.Query{Year: &year, Month: &month, Limit: 50, Sort: "date"})
	if err != nil || len(page.Transactions) != 1 || page.Transactions[0].Description != "local 2026" {
		t.Fatalf("year/month %+v %v", page, err)
	}
	year, month = 2027, 1
	page, err = s.Search(context.Background(), ledger.Query{Year: &year, Month: &month, Limit: 50, Sort: "date"})
	if err != nil || len(page.Transactions) != 1 || page.Transactions[0].Description != "local 2027" {
		t.Fatalf("year/month %+v %v", page, err)
	}
}
