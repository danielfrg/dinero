package store_test

import (
	"context"
	"math"
	"testing"
	"time"

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

func TestPostgresCentralTimezoneCalendarRules(t *testing.T) {
	fx := testutil.NewPostgres(t)
	cases := []struct {
		utc, local, monthKey string
		year, month          int
	}{
		{"2026-01-01T05:59:59Z", "2025-12-31T23:59:59-06:00", "2025-12", 2025, 12},
		{"2026-01-01T06:00:00Z", "2026-01-01T00:00:00-06:00", "2026-01", 2026, 1},
		{"2026-07-01T04:59:59Z", "2026-06-30T23:59:59-05:00", "2026-06", 2026, 6},
		{"2026-07-01T05:00:00Z", "2026-07-01T00:00:00-05:00", "2026-07", 2026, 7},
	}
	for _, tc := range cases {
		fx.Insert(t, tc.utc, tc.local, 1, "Checking")
	}
	for _, timezone := range []string{"US/Central", "America/Chicago"} {
		t.Run(timezone, func(t *testing.T) {
			s := fx.Open(t, timezone)
			for _, tc := range cases {
				t.Run(tc.monthKey, func(t *testing.T) {
					page, err := s.Search(context.Background(), ledger.Query{Year: &tc.year, Month: &tc.month, Limit: 50})
					if err != nil || len(page.Transactions) != 1 {
						t.Fatalf("year/month %+v %v", page, err)
					}
					date := page.Transactions[0].Date
					if date == nil || date.Format(time.RFC3339) != tc.local {
						t.Fatalf("date %v, want %s", date, tc.local)
					}
				})
			}
			summary, err := s.Summary(context.Background(), ledger.SummaryQuery{GroupBy: "month"})
			if err != nil || len(summary.Groups) != len(cases) {
				t.Fatalf("monthly summary %+v %v", summary, err)
			}
			for i, group := range summary.Groups {
				if group.Key != cases[i].monthKey || group.Count != 1 || group.SignedTotal != "1" {
					t.Fatalf("monthly group %+v, want month %s with one transaction totaling 1", group, cases[i].monthKey)
				}
			}
		})
	}
}
