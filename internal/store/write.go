package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/danielfrg/dinero/internal/ledger"
)

// WriterAdvisoryLock is shared by every PostgreSQL writer in this package.
const WriterAdvisoryLock int64 = 20261014

// WithWriteTx serializes classification and insertion as one database transaction.
// SQLite writer connections use _txlock=immediate, acquiring the write reservation
// before the callback scans rows. PostgreSQL writers acquire this advisory lock.
func (s *Store) WithWriteTx(ctx context.Context, fn func(*sql.Tx) error) error {
	if s.readOnly {
		return errors.New("store is read-only")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if s.dialect == "postgres" {
		if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", WriterAdvisoryLock); err != nil {
			return err
		}
	}
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// TransactionsTx scans legacy transaction columns using the caller's transaction.
// Callers inside WithWriteTx must use this method to avoid a second connection.
func (s *Store) TransactionsTx(ctx context.Context, tx *sql.Tx) ([]ledger.Transaction, error) {
	table := "transactions"
	if s.dialect == "postgres" {
		table = "public.transactions"
	}
	rows, err := tx.QueryContext(ctx, "SELECT id,"+s.selectDate()+",description,category,subcategory,amount,account,notes FROM "+table+" ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ledger.Transaction{}
	for rows.Next() {
		var tr ledger.Transaction
		var date, desc, cat, sub, acc, notes sql.NullString
		var amount sql.NullFloat64
		if err := rows.Scan(&tr.ID, &date, &desc, &cat, &sub, &amount, &acc, &notes); err != nil {
			return nil, err
		}
		tr.Description, tr.Category, tr.Subcategory, tr.Account, tr.Notes = desc.String, cat.String, sub.String, acc.String, notes.String
		if amount.Valid {
			v := amount.Float64
			tr.Amount = &v
		}
		if date.Valid {
			d, e := parseStoredDate(date.String, s.loc)
			if e != nil {
				return nil, e
			}
			tr.Date = &d
		}
		out = append(out, tr)
	}
	return out, rows.Err()
}

func (s *Store) Transactions(ctx context.Context) ([]ledger.Transaction, error) {
	tx, readCtx, cancel, err := s.readTx(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer tx.Rollback()
	rows, err := s.TransactionsTx(readCtx, tx)
	if err != nil {
		return nil, &ledger.ErrUnavailable{}
	}
	if err = tx.Commit(); err != nil {
		return nil, &ledger.ErrUnavailable{}
	}
	return rows, nil
}

// InsertTransactionsTx inserts only the eight legacy columns, without creating a schema.
func (s *Store) InsertTransactionsTx(ctx context.Context, tx *sql.Tx, records []ledger.Transaction) error {
	table := "transactions"
	if s.dialect == "postgres" {
		table = "public.transactions"
	}
	params := "?,?,?,?,?,?,?"
	if s.dialect == "postgres" {
		params = "$1,$2,$3,$4,$5,$6,$7"
	}
	stmt := "INSERT INTO " + table + " (date,description,category,subcategory,amount,notes,account) VALUES (" + params + ")"
	for _, tr := range records {
		var date, amount any
		if tr.Date != nil {
			if s.dialect == "postgres" {
				date = *tr.Date
			} else {
				date = tr.Date.In(s.loc).Format("2006-01-02 15:04:05")
			}
		}
		if tr.Amount != nil {
			amount = *tr.Amount
		}
		if _, err := tx.ExecContext(ctx, stmt, date, tr.Description, tr.Category, tr.Subcategory, amount, tr.Notes, tr.Account); err != nil {
			return fmt.Errorf("insert transaction: %w", err)
		}
	}
	return nil
}
