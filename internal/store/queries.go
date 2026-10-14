package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/danielfrg/dinero/internal/ledger"
)

func (s *Store) conditions(q ledger.Query) (string, []any, error) {
	var clauses []string
	var args []any
	add := func(fragment string, v any) {
		args = append(args, v)
		clauses = append(clauses, fmt.Sprintf(fragment, s.placeholder(len(args))))
	}
	for _, f := range []struct {
		column string
		value  *string
	}{{"account", q.Account}, {"category", q.Category}, {"subcategory", q.Subcategory}} {
		if f.value != nil {
			add(f.column+" = %s", *f.value)
		}
	}
	if q.Description != nil {
		if s.dialect == "postgres" {
			add("description ILIKE %s", "%"+*q.Description+"%")
		} else {
			add("LOWER(description) LIKE LOWER(%s)", "%"+*q.Description+"%")
		}
	}
	if q.After != nil {
		day, err := ledger.ParseDay(*q.After, s.loc)
		if err != nil {
			return "", nil, err
		}
		if s.dialect == "sqlite" {
			add("date >= %s", day.Format("2006-01-02 15:04:05"))
		} else {
			add("date >= %s", day)
		}
	}
	if q.Before != nil {
		day, err := ledger.NextDay(*q.Before, s.loc)
		if err != nil {
			return "", nil, err
		}
		if s.dialect == "sqlite" {
			add("date < %s", day.Format("2006-01-02 15:04:05"))
		} else {
			add("date < %s", day)
		}
	}
	if q.Year != nil {
		if s.dialect == "sqlite" {
			add("CAST(strftime('%Y',date) AS INTEGER) = %s", *q.Year)
		} else {
			args = append(args, s.loc.String())
			zone := s.placeholder(len(args))
			add("EXTRACT(YEAR FROM date AT TIME ZONE "+zone+") = %s", *q.Year)
		}
	}
	if q.Month != nil {
		if s.dialect == "sqlite" {
			add("CAST(strftime('%m',date) AS INTEGER) = %s", *q.Month)
		} else {
			args = append(args, s.loc.String())
			zone := s.placeholder(len(args))
			add("EXTRACT(MONTH FROM date AT TIME ZONE "+zone+") = %s", *q.Month)
		}
	}
	if len(clauses) == 0 {
		return "", args, nil
	}
	return " WHERE " + strings.Join(clauses, " AND "), args, nil
}
func (s *Store) selectDate() string {
	if s.dialect == "postgres" {
		return "to_char(date AT TIME ZONE '" + strings.ReplaceAll(s.loc.String(), "'", "''") + "', 'YYYY-MM-DD HH24:MI:SS.US')"
	}
	return "CAST(date AS TEXT)"
}
func (s *Store) Search(ctx context.Context, q ledger.Query) (ledger.Page, error) {
	where, args, err := s.conditions(q)
	if err != nil {
		return ledger.Page{}, err
	}
	sortColumn := q.Sort
	if sortColumn == "" {
		sortColumn = "date"
	}
	switch sortColumn {
	case "date", "amount", "description", "account", "category", "subcategory":
	default:
		return ledger.Page{}, &ledger.ErrInvalidInput{Field: "sort", Reason: "unsupported column"}
	}
	dir := "ASC"
	if q.Desc {
		dir = "DESC"
	}
	lim := q.Limit
	if lim <= 0 {
		lim = 50
	}
	args = append(args, lim+1, q.Offset)
	stmt := "SELECT id," + s.selectDate() + ",description,category,subcategory,amount,account,notes FROM public.transactions" + where + " ORDER BY " + sortColumn + " " + dir + ", id " + dir + " LIMIT " + s.placeholder(len(args)-1) + " OFFSET " + s.placeholder(len(args))
	if s.dialect == "sqlite" {
		stmt = strings.ReplaceAll(stmt, "public.transactions", "transactions")
	}
	tx, err := s.readTx(ctx)
	if err != nil {
		return ledger.Page{}, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, stmt, args...)
	if err != nil {
		return ledger.Page{}, &ledger.ErrUnavailable{}
	}
	defer rows.Close()
	out := ledger.Page{Transactions: []ledger.Transaction{}, Limit: lim, Offset: q.Offset}
	for rows.Next() {
		var t ledger.Transaction
		var date, desc, cat, sub, acc, notes sql.NullString
		var amount sql.NullFloat64
		if err = rows.Scan(&t.ID, &date, &desc, &cat, &sub, &amount, &acc, &notes); err != nil {
			return ledger.Page{}, &ledger.ErrUnavailable{}
		}
		t.Description = desc.String
		t.Category = cat.String
		t.Subcategory = sub.String
		t.Account = acc.String
		t.Notes = notes.String
		if amount.Valid {
			v := amount.Float64
			t.Amount = &v
		}
		if date.Valid {
			d, e := parseStoredDate(date.String, s.loc)
			if e != nil {
				return ledger.Page{}, &ledger.ErrUnavailable{}
			}
			t.Date = &d
		}
		out.Transactions = append(out.Transactions, t)
	}
	if err = rows.Err(); err != nil {
		return ledger.Page{}, &ledger.ErrUnavailable{}
	}
	if len(out.Transactions) > lim {
		out.HasMore = true
		out.Transactions = out.Transactions[:lim]
	}
	if err = tx.Commit(); err != nil {
		return ledger.Page{}, &ledger.ErrUnavailable{}
	}
	return out, nil
}
func parseStoredDate(value string, loc *time.Location) (time.Time, error) {
	for _, layout := range []string{"2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05", "2006-01-02T15:04:05.999999999Z07:00", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, value, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid stored date")
}
func (s *Store) Accounts(ctx context.Context) ([]ledger.Account, error) {
	dateExpr := "MAX(" + s.selectDate() + ")"
	stmt := "SELECT account," + dateExpr + " FROM public.transactions WHERE account IS NOT NULL AND account <> '' GROUP BY account ORDER BY account"
	if s.dialect == "sqlite" {
		stmt = strings.ReplaceAll(stmt, "public.transactions", "transactions")
	}
	tx, err := s.readTx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, stmt)
	if err != nil {
		return nil, &ledger.ErrUnavailable{}
	}
	defer rows.Close()
	out := []ledger.Account{}
	for rows.Next() {
		var a ledger.Account
		var latest sql.NullString
		if err = rows.Scan(&a.Name, &latest); err != nil {
			return nil, &ledger.ErrUnavailable{}
		}
		if latest.Valid {
			v := latest.String[:10]
			a.LatestDate = &v
		}
		out = append(out, a)
	}
	if err = rows.Err(); err != nil {
		return nil, &ledger.ErrUnavailable{}
	}
	if err = tx.Commit(); err != nil {
		return nil, &ledger.ErrUnavailable{}
	}
	return out, nil
}
func (s *Store) Categories(ctx context.Context) (ledger.Categories, error) {
	out := ledger.Categories{Categories: []string{}, Subcategories: []string{}, Pairs: []ledger.CategoryPair{}}
	tx, err := s.readTx(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	table := "public.transactions"
	if s.dialect == "sqlite" {
		table = "transactions"
	}
	for _, spec := range []struct {
		col string
		dst *[]string
	}{{"category", &out.Categories}, {"subcategory", &out.Subcategories}} {
		rows, err := tx.QueryContext(ctx, "SELECT DISTINCT "+spec.col+" FROM "+table+" WHERE "+spec.col+" IS NOT NULL AND "+spec.col+" <> '' ORDER BY "+spec.col)
		if err != nil {
			return out, &ledger.ErrUnavailable{}
		}
		for rows.Next() {
			var v string
			if err = rows.Scan(&v); err != nil {
				rows.Close()
				return out, &ledger.ErrUnavailable{}
			}
			*spec.dst = append(*spec.dst, v)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return out, &ledger.ErrUnavailable{}
		}
	}
	rows, err := tx.QueryContext(ctx, "SELECT DISTINCT category,subcategory FROM "+table+" WHERE category IS NOT NULL AND category <> '' AND subcategory IS NOT NULL AND subcategory <> '' ORDER BY category,subcategory")
	if err != nil {
		return out, &ledger.ErrUnavailable{}
	}
	defer rows.Close()
	for rows.Next() {
		var p ledger.CategoryPair
		if err = rows.Scan(&p.Category, &p.Subcategory); err != nil {
			return out, &ledger.ErrUnavailable{}
		}
		out.Pairs = append(out.Pairs, p)
	}
	if err = rows.Err(); err != nil {
		return out, &ledger.ErrUnavailable{}
	}
	if err = tx.Commit(); err != nil {
		return out, &ledger.ErrUnavailable{}
	}
	return out, nil
}
func (s *Store) Balance(ctx context.Context, account, through string) (ledger.Balance, error) {
	day, err := ledger.NextDay(through, s.loc)
	if err != nil {
		return ledger.Balance{}, err
	}
	var boundary any = day
	table := "public.transactions"
	sum := "COALESCE(SUM(amount),0)::text"
	if s.dialect == "sqlite" {
		boundary = day.Format("2006-01-02 15:04:05")
		table = "transactions"
		sum = "CAST(COALESCE(SUM(amount),0) AS TEXT)"
	}
	stmt := "SELECT " + sum + " FROM " + table + " WHERE account = " + s.placeholder(1) + " AND date < " + s.placeholder(2)
	var amount string
	tx, err := s.readTx(ctx)
	if err != nil {
		return ledger.Balance{}, err
	}
	defer tx.Rollback()
	if err = tx.QueryRowContext(ctx, stmt, account, boundary).Scan(&amount); err != nil {
		return ledger.Balance{}, &ledger.ErrUnavailable{}
	}
	if err = tx.Commit(); err != nil {
		return ledger.Balance{}, &ledger.ErrUnavailable{}
	}
	return ledger.Balance{Account: account, Through: through, Amount: amount}, nil
}
