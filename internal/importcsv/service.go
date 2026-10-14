package importcsv

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/danielfrg/dinero/internal/config"
	"github.com/danielfrg/dinero/internal/ledger"
	"github.com/danielfrg/dinero/internal/store"
)

type Transaction struct {
	Row         int    `json:"row"`
	Date        string `json:"date"`
	Description string `json:"description"`
	Amount      string `json:"amount"`
	Category    string `json:"category"`
	Subcategory string `json:"subcategory"`
}
type Ambiguous struct {
	Transaction Transaction `json:"transaction"`
	ExistingIDs []int64     `json:"existing_ids"`
}
type Result struct {
	Status                string        `json:"status"`
	File                  string        `json:"file"`
	Account               string        `json:"account"`
	TransactionsInCSV     int           `json:"transactions_in_csv"`
	NewTransactions       int           `json:"new_transactions"`
	ExistingTransactions  int           `json:"existing_transactions"`
	AmbiguousTransactions int           `json:"ambiguous_transactions"`
	Transactions          []Transaction `json:"transactions"`
	Ambiguous             []Ambiguous   `json:"ambiguous"`
	records               []record
}
type Service struct {
	store *store.Store
	cfg   config.Config
	loc   *time.Location
}

func New(s *store.Store, cfg config.Config) *Service {
	loc, e := time.LoadLocation(cfg.Timezone)
	if e != nil {
		loc = time.UTC
	}
	return &Service{store: s, cfg: cfg, loc: loc}
}
func (s *Service) Preview(ctx context.Context, input io.Reader, basename, account string) (Result, error) {
	existing, err := s.store.Transactions(ctx)
	if err != nil {
		return Result{}, err
	}
	known := false
	for _, name := range s.cfg.Plaid.AccountIDToName {
		if name == account {
			known = true
			break
		}
	}
	for _, tr := range existing {
		if tr.Account == account {
			known = true
			break
		}
	}
	if !known {
		return Result{}, fmt.Errorf("Unknown account: %s", account)
	}
	records, err := parse(input, basename, account, s.loc)
	if err != nil {
		return Result{}, err
	}
	if groups := duplicateRows(records, s.loc); len(groups) > 0 {
		parts := []string{}
		for _, rows := range groups {
			numbers := []string{}
			for _, row := range rows {
				numbers = append(numbers, fmt.Sprint(row))
			}
			parts = append(parts, "rows "+strings.Join(numbers, ", "))
		}
		return Result{}, fmt.Errorf("Duplicate CSV transactions: %s", strings.Join(parts, "; "))
	}
	return buildResult(basename, account, records, existing, s.loc), nil
}
func (s *Service) Commit(ctx context.Context, preview Result) (Result, error) {
	if len(preview.records) == 0 || preview.Status == "ambiguous" {
		return preview, nil
	}
	var result Result
	err := s.store.WithWriteTx(ctx, func(tx *sql.Tx) error {
		existing, e := s.store.TransactionsTx(ctx, tx)
		if e != nil {
			return e
		}
		result = buildResult(preview.File, preview.Account, preview.records, existing, s.loc)
		if result.Status == "ambiguous" || result.Status == "no-new-transactions" {
			return nil
		}
		newRows, _, _ := classify(preview.records, existing, s.loc)
		transactions := make([]ledger.Transaction, 0, len(newRows))
		for _, r := range newRows {
			transactions = append(transactions, r.transaction)
		}
		if e = s.store.InsertTransactionsTx(ctx, tx, transactions); e != nil {
			return e
		}
		result.Status = "imported"
		return nil
	})
	if err != nil {
		return Result{}, fmt.Errorf("Import failed; no transactions were committed")
	}
	return result, nil
}
func buildResult(file, account string, records []record, existing []ledger.Transaction, loc *time.Location) Result {
	newRows, oldRows, ambiguous := classify(records, existing, loc)
	status := "dry-run"
	if len(records) == 0 {
		status = "no-transactions"
	} else if len(ambiguous) > 0 {
		status = "ambiguous"
	} else if len(newRows) == 0 {
		status = "no-new-transactions"
	}
	result := Result{Status: status, File: file, Account: account, TransactionsInCSV: len(records), NewTransactions: len(newRows), ExistingTransactions: len(oldRows), AmbiguousTransactions: len(ambiguous), Transactions: []Transaction{}, Ambiguous: ambiguous, records: records}
	for _, r := range newRows {
		result.Transactions = append(result.Transactions, serialize(r))
	}
	if result.Ambiguous == nil {
		result.Ambiguous = []Ambiguous{}
	}
	return result
}
func serialize(r record) Transaction {
	return Transaction{Row: r.row, Date: r.transaction.Date.Format("2006-01-02"), Description: r.transaction.Description, Amount: r.amount.StringFixed(2), Category: r.transaction.Category, Subcategory: r.transaction.Subcategory}
}
