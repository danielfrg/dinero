package ingest

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/danielfrg/dinero/internal/config"
	"github.com/danielfrg/dinero/internal/ledger"
	"github.com/danielfrg/dinero/internal/plaid"
	"github.com/danielfrg/dinero/internal/store"
)

type Record struct {
	Date        string  `json:"date"`
	Description string  `json:"description"`
	Account     string  `json:"account"`
	Amount      float64 `json:"amount"`
	Category    string  `json:"category"`
	Subcategory string  `json:"subcategory"`
}
type Result struct {
	Status               string   `json:"status"`
	Start                string   `json:"start"`
	Through              string   `json:"through"`
	TransactionsQueried  int      `json:"transactions_queried"`
	PendingTransactions  int      `json:"pending_transactions"`
	NewTransactions      int      `json:"new_transactions"`
	ExistingTransactions int      `json:"existing_transactions"`
	MissingAccountIDs    []string `json:"missing_account_ids"`
	Transactions         []Record `json:"transactions"`
	records              []plaid.Transaction
}
type Service struct {
	store    *store.Store
	cfg      config.Config
	provider plaid.Client
	loc      *time.Location
}

func New(s *store.Store, cfg config.Config, p plaid.Client) *Service {
	loc, e := time.LoadLocation(cfg.Timezone)
	if e != nil {
		loc = time.UTC
	}
	return &Service{store: s, cfg: cfg, provider: p, loc: loc}
}

func (s *Service) Preview(ctx context.Context, days int, through string) (Result, error) {
	if days <= 0 {
		return Result{}, errors.New("days must be positive")
	}
	var end time.Time
	var err error
	if through == "" {
		end = time.Now().In(s.loc)
	} else {
		end, err = time.ParseInLocation("2006-01-02", through, s.loc)
		if err != nil {
			return Result{}, errors.New("through must be YYYY-MM-DD")
		}
	}
	start := end.AddDate(0, 0, -days)
	all := []plaid.Transaction{}
	aliases := make([]string, 0, len(s.cfg.Plaid.Tokens))
	for alias := range s.cfg.Plaid.Tokens {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	if len(aliases) > 0 && s.provider == nil {
		return Result{}, errors.New("Plaid client is required")
	}
	for _, alias := range aliases {
		token := s.cfg.Plaid.Tokens[alias]
		for offset := 0; ; {
			page, e := s.provider.GetTransactions(ctx, token, start, end, offset)
			if e != nil {
				lower := strings.ToLower(e.Error())
				if strings.Contains(lower, "the login details of this item have changed") || strings.Contains(lower, "item_login_required") {
					return Result{}, fmt.Errorf("Credentials expired for account: %s", alias)
				}
				return Result{}, fmt.Errorf("fetch transactions for %s: %w", alias, e)
			}
			if page.Total < 0 || page.Total < offset+len(page.Transactions) {
				return Result{}, errors.New("Plaid returned inconsistent pagination")
			}
			all = append(all, page.Transactions...)
			offset += len(page.Transactions)
			if offset >= page.Total {
				break
			}
			if len(page.Transactions) == 0 {
				return Result{}, errors.New("Plaid pagination stopped before total transactions")
			}
		}
	}
	existing, err := s.store.Transactions(ctx)
	if err != nil {
		return Result{}, err
	}
	return s.classify(start, end, all, existing), nil
}

func (s *Service) Commit(ctx context.Context, preview Result) (Result, error) {
	if preview.Start == "" || preview.Through == "" {
		return Result{}, errors.New("invalid transactions preview")
	}
	start, e := time.ParseInLocation("2006-01-02", preview.Start, s.loc)
	if e != nil {
		return Result{}, e
	}
	end, e := time.ParseInLocation("2006-01-02", preview.Through, s.loc)
	if e != nil {
		return Result{}, e
	}
	var result Result
	err := s.store.WithWriteTx(ctx, func(tx *sql.Tx) error {
		existing, e := s.store.TransactionsTx(ctx, tx)
		if e != nil {
			return e
		}
		result = s.classify(start, end, preview.records, existing)
		if len(result.Transactions) == 0 {
			return nil
		}
		rows := make([]ledger.Transaction, 0, len(result.Transactions))
		for _, r := range result.Transactions {
			date, e := time.ParseInLocation("2006-01-02", r.Date, s.loc)
			if e != nil {
				return e
			}
			amount := r.Amount
			rows = append(rows, ledger.Transaction{Date: &date, Description: r.Description, Category: r.Category, Subcategory: r.Subcategory, Amount: &amount, Account: r.Account, Notes: "new"})
		}
		if e := s.store.InsertTransactionsTx(ctx, tx, rows); e != nil {
			return e
		}
		result.Status = "imported"
		return nil
	})
	if err != nil {
		return Result{}, fmt.Errorf("transactions not committed: %w", err)
	}
	return result, nil
}

type key struct {
	date, account, description string
	amount                     float64
}

func (s *Service) classify(start, end time.Time, records []plaid.Transaction, existing []ledger.Transaction) Result {
	byKey := map[key]bool{}
	for _, row := range existing {
		if row.Date != nil && row.Amount != nil {
			byKey[key{row.Date.In(s.loc).Format("2006-01-02"), row.Account, row.Description, *row.Amount}] = true
		}
	}
	rules := readRules()
	result := Result{Status: "dry-run", Start: start.Format("2006-01-02"), Through: end.Format("2006-01-02"), TransactionsQueried: len(records), MissingAccountIDs: []string{}, Transactions: []Record{}, records: records}
	missing := map[string]bool{}
	for _, tr := range records {
		if tr.Pending {
			result.PendingTransactions++
			continue
		}
		account := s.cfg.Plaid.AccountIDToName[tr.AccountID]
		if account == "" {
			account = tr.AccountID
			if !missing[tr.AccountID] {
				result.MissingAccountIDs = append(result.MissingAccountIDs, tr.AccountID)
				missing[tr.AccountID] = true
			}
		}
		k := key{tr.Date, account, tr.Name, tr.Amount}
		if byKey[k] {
			result.ExistingTransactions++
			continue
		}
		category := rules[tr.Name]
		result.Transactions = append(result.Transactions, Record{Date: tr.Date, Description: tr.Name, Account: account, Amount: tr.Amount, Category: category[0], Subcategory: category[1]})
		result.NewTransactions++
	}
	sort.Strings(result.MissingAccountIDs)
	if result.NewTransactions == 0 {
		result.Status = "no-new-transactions"
	}
	return result
}
func readRules() map[string][2]string {
	out := map[string][2]string{}
	b, e := os.ReadFile(filepath.Join(config.StateDir(), "category_rules.json"))
	if e == nil {
		_ = json.Unmarshal(b, &out)
	}
	return out
}
