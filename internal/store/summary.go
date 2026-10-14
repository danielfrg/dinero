package store

import (
	"context"
	"database/sql"
	"strings"

	"github.com/danielfrg/dinero/internal/ledger"
)

func (s *Store) Summary(ctx context.Context, q ledger.SummaryQuery) (ledger.Summary, error) {
	where, args, err := s.conditions(q.Query)
	if err != nil {
		return ledger.Summary{}, err
	}
	table := "public.transactions"
	valid := "amount IS NOT NULL AND amount > '-Infinity'::float8 AND amount < 'Infinity'::float8 AND mod(amount::numeric * 100,1)=0"
	sum := func(expr string) string {
		return "COALESCE(SUM(CASE WHEN " + valid + " THEN " + expr + " ELSE 0 END),0)::text"
	}
	key := "''"
	if s.dialect == "sqlite" {
		table = "transactions"
		valid = "amount IS NOT NULL AND amount = amount AND abs(amount) < 1e308 AND round(amount,2)=amount"
		sum = func(expr string) string {
			return "CAST(COALESCE(SUM(CASE WHEN " + valid + " THEN " + expr + " ELSE 0 END),0) AS TEXT)"
		}
	}
	switch q.GroupBy {
	case "", "all":
	case "account", "category", "subcategory":
		key = "COALESCE(" + q.GroupBy + ",'')"
	case "month":
		if s.dialect == "sqlite" {
			key = "strftime('%Y-%m',date)"
		} else {
			key = "to_char(date AT TIME ZONE '" + strings.ReplaceAll(s.loc.String(), "'", "''") + "','YYYY-MM')"
		}
	default:
		return ledger.Summary{}, &ledger.ErrInvalidInput{Field: "group_by", Reason: "unsupported group"}
	}
	stmt := "SELECT " + key + ",COUNT(*),COALESCE(SUM(CASE WHEN " + valid + " THEN 0 ELSE 1 END),0)," + sum("amount") + "," + sum("CASE WHEN amount > 0 THEN amount ELSE 0 END") + "," + sum("CASE WHEN amount < 0 THEN amount ELSE 0 END") + " FROM " + table + where
	if q.GroupBy != "" && q.GroupBy != "all" {
		stmt += " GROUP BY " + key + " ORDER BY " + key
	}
	tx, err := s.readTx(ctx)
	if err != nil {
		return ledger.Summary{}, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, stmt, args...)
	if err != nil {
		return ledger.Summary{}, &ledger.ErrUnavailable{}
	}
	defer rows.Close()
	out := ledger.Summary{Groups: []ledger.SummaryGroup{}}
	for rows.Next() {
		var g ledger.SummaryGroup
		var keyValue sql.NullString
		if err = rows.Scan(&keyValue, &g.Count, &g.IncompleteAmountCount, &g.SignedTotal, &g.Outflows, &g.Inflows); err != nil {
			return ledger.Summary{}, &ledger.ErrUnavailable{}
		}
		g.Key = keyValue.String
		out.IncompleteAmountCount += g.IncompleteAmountCount
		out.Groups = append(out.Groups, g)
	}
	if err = rows.Err(); err != nil {
		return ledger.Summary{}, &ledger.ErrUnavailable{}
	}
	if err = tx.Commit(); err != nil {
		return ledger.Summary{}, &ledger.ErrUnavailable{}
	}
	return out, nil
}
