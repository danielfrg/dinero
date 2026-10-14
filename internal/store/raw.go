package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// RawTransaction retains SQL NULLs and database timestamp values for exports,
// rules, and caches. Ledger.Transaction deliberately has different semantics.
type RawTransaction struct {
	ID                                 int64
	Date                               sql.NullString
	Description, Category, Subcategory sql.NullString
	Amount                             sql.NullFloat64
	Notes, Account                     sql.NullString
}

func (s *Store) RawTransactions(ctx context.Context) ([]RawTransaction, error) {
	tx, readCtx, cancel, err := s.readTx(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer tx.Rollback()
	table := "transactions"
	if s.dialect == "postgres" {
		table = "public.transactions"
	}
	rows, err := tx.QueryContext(readCtx, "SELECT id,date,description,category,subcategory,amount,notes,account FROM "+table+" ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RawTransaction{}
	for rows.Next() {
		var r RawTransaction
		var rawDate any
		if err := rows.Scan(&r.ID, &rawDate, &r.Description, &r.Category, &r.Subcategory, &r.Amount, &r.Notes, &r.Account); err != nil {
			return nil, err
		}
		switch v := rawDate.(type) {
		case nil:
		case time.Time:
			if s.dialect == "postgres" {
				v = v.UTC()
			}
			r.Date = sql.NullString{String: v.Format("2006-01-02 15:04:05.999999"), Valid: true}
		case string:
			r.Date = sql.NullString{String: v, Valid: true}
		case []byte:
			r.Date = sql.NullString{String: string(v), Valid: true}
		default:
			return nil, fmt.Errorf("unsupported raw date type %T", v)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) Dialect() string { return s.dialect }

// InitSchema creates the legacy table only when absent.
func (s *Store) InitSchema(ctx context.Context) error {
	return s.WithWriteTx(ctx, func(tx *sql.Tx) error {
		stmt := "CREATE TABLE IF NOT EXISTS transactions (id INTEGER PRIMARY KEY AUTOINCREMENT, date DATETIME NULL, description VARCHAR(1000) NULL, category VARCHAR(50) NULL, subcategory VARCHAR(50) NULL, amount DOUBLE NULL, notes VARCHAR(1000) NULL, account VARCHAR(50) NULL)"
		if s.dialect == "postgres" {
			stmt = "CREATE TABLE IF NOT EXISTS public.transactions (id SERIAL PRIMARY KEY, date TIMESTAMP WITH TIME ZONE NULL, description VARCHAR(1000) NULL, category VARCHAR(50) NULL, subcategory VARCHAR(50) NULL, amount DOUBLE PRECISION NULL, notes VARCHAR(1000) NULL, account VARCHAR(50) NULL)"
		}
		_, err := tx.ExecContext(ctx, stmt)
		return err
	})
}
