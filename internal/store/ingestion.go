package store

import (
	"context"
	"database/sql"
)

// IngestionTransaction retains the database's UTC calendar day and nullable
// tuple fields. Ledger queries intentionally use the configured local day.
type IngestionTransaction struct {
	Date, Description, Account sql.NullString
	Amount                     sql.NullFloat64
}

func (s *Store) IngestionTransactions(ctx context.Context) ([]IngestionTransaction, error) {
	tx, readCtx, cancel, err := s.readTx(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer tx.Rollback()
	records, err := s.IngestionTransactionsTx(readCtx, tx)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return records, nil
}

// IngestionTransactionsTx uses the supplied write transaction to preserve
// the shared writer lock and avoid a second SQLite connection.
func (s *Store) IngestionTransactionsTx(ctx context.Context, tx *sql.Tx) ([]IngestionTransaction, error) {
	table := "transactions"
	dateExpr := "substr(CAST(date AS TEXT),1,10)"
	if s.dialect == "postgres" {
		table = "public.transactions"
		dateExpr = "to_char(date AT TIME ZONE 'UTC','YYYY-MM-DD')"
	}
	rows, err := tx.QueryContext(ctx, "SELECT "+dateExpr+",description,amount,account FROM "+table+" ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IngestionTransaction{}
	for rows.Next() {
		var r IngestionTransaction
		if err := rows.Scan(&r.Date, &r.Description, &r.Amount, &r.Account); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
