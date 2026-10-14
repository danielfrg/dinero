package store

import (
	"context"
	"database/sql"
	"sort"
	"strconv"
	"strings"

	"github.com/danielfrg/dinero/internal/ledger"
	"github.com/shopspring/decimal"
)

// validAmountSQL checks storage before decimal conversion. A conversion can
// shorten floats, which could conceal a tiny fractional cent.
func (s *Store) validAmountSQL() string {
	if s.dialect == "postgres" {
		return "amount IS NOT NULL AND amount > '-Infinity'::float8 AND amount < 'Infinity'::float8 AND mod(amount::numeric * 100,1)=0"
	}
	return "amount IS NOT NULL AND amount = amount AND abs(amount) < 1e308 AND round(amount,2)=amount"
}

type summaryAccumulator struct {
	count, incomplete         int64
	signed, outflows, inflows decimal.Decimal
}

func (s *Store) Summary(ctx context.Context, q ledger.SummaryQuery) (ledger.Summary, error) {
	where, args, err := s.conditions(q.Query)
	if err != nil {
		return ledger.Summary{}, err
	}
	table := "public.transactions"
	if s.dialect == "sqlite" {
		table = "transactions"
	}
	key := "''"
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
	stmt := "SELECT " + key + ",amount,CASE WHEN " + s.validAmountSQL() + " THEN 1 ELSE 0 END FROM " + table + where
	tx, readCtx, cancel, err := s.readTx(ctx)
	if err != nil {
		return ledger.Summary{}, err
	}
	defer cancel()
	defer tx.Rollback()
	rows, err := tx.QueryContext(readCtx, stmt, args...)
	if err != nil {
		return ledger.Summary{}, &ledger.ErrUnavailable{}
	}
	defer rows.Close()
	groups := map[string]*summaryAccumulator{}
	for rows.Next() {
		var keyValue sql.NullString
		var amount sql.NullFloat64
		var valid int64
		if err = rows.Scan(&keyValue, &amount, &valid); err != nil {
			return ledger.Summary{}, &ledger.ErrUnavailable{}
		}
		group := groups[keyValue.String]
		if group == nil {
			group = &summaryAccumulator{}
			groups[keyValue.String] = group
		}
		group.count++
		if valid == 0 || !amount.Valid {
			group.incomplete++
			continue
		}
		value, parseErr := decimal.NewFromString(strconv.FormatFloat(amount.Float64, 'f', -1, 64))
		if parseErr != nil {
			return ledger.Summary{}, &ledger.ErrUnavailable{}
		}
		value = value.RoundBank(2)
		group.signed = group.signed.Add(value)
		if value.IsPositive() {
			group.outflows = group.outflows.Add(value)
		}
		if value.IsNegative() {
			group.inflows = group.inflows.Add(value)
		}
	}
	if err = rows.Err(); err != nil {
		return ledger.Summary{}, &ledger.ErrUnavailable{}
	}
	if err = tx.Commit(); err != nil {
		return ledger.Summary{}, &ledger.ErrUnavailable{}
	}
	if len(groups) == 0 && (q.GroupBy == "" || q.GroupBy == "all") {
		groups[""] = &summaryAccumulator{}
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := ledger.Summary{Groups: make([]ledger.SummaryGroup, 0, len(keys))}
	for _, key := range keys {
		g := groups[key]
		out.IncompleteAmountCount += g.incomplete
		out.Groups = append(out.Groups, ledger.SummaryGroup{Key: key, SignedTotal: g.signed.String(), Outflows: g.outflows.String(), Inflows: g.inflows.String(), Count: g.count, IncompleteAmountCount: g.incomplete})
	}
	return out, nil
}
