package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danielfrg/dinero/internal/cli"
	"github.com/danielfrg/dinero/internal/plaid"
	"github.com/danielfrg/dinero/internal/testutil"
)

type reportingPlaid struct {
	beforeFetch func(string)
}

func (p reportingPlaid) GetTransactions(_ context.Context, token string, _, _ time.Time, offset int) (plaid.TransactionsPage, error) {
	if p.beforeFetch != nil {
		p.beforeFetch(token)
	}
	switch token {
	case "synthetic-first-secret":
		if offset == 0 {
			return plaid.TransactionsPage{Total: 3, Transactions: []plaid.Transaction{
				{AccountID: "checking", Date: "2026-10-13", Name: "Existing coffee", Amount: 4.25},
				{AccountID: "checking", Date: "2026-10-14", Name: "Lunch", Amount: 9.5},
			}}, nil
		}
		return plaid.TransactionsPage{Total: 3, Transactions: []plaid.Transaction{
			{AccountID: "checking", Date: "2026-10-14", Name: "Pending gas", Amount: 35, Pending: true},
		}}, nil
	case "synthetic-second-secret":
		return plaid.TransactionsPage{Total: 2, Transactions: []plaid.Transaction{
			{AccountID: "card", Date: "2026-10-14", Name: "Card hold", Amount: 12.3, Pending: true},
			{AccountID: "savings", Date: "2026-10-14", Name: "Salary", Amount: -100},
		}}, nil
	default:
		return plaid.TransactionsPage{}, fmt.Errorf("unexpected synthetic token")
	}
}

func reportingConfig(t *testing.T, fx *testutil.Fixture) {
	t.Helper()
	queryConfig(t, fx.DSN)
	t.Setenv("DINERO_STATE_DIR", t.TempDir())
	t.Setenv("DINERO_YES", "0")
	body := "timezone = \"UTC\"\n[database]\nconnection_string = \"sqlite:///" + fx.DSN + "\"\n" +
		"[plaid.tokens]\nbank_a = \"synthetic-first-secret\"\nbank_b = \"synthetic-second-secret\"\n" +
		"[plaid.account_id_to_name]\nchecking = \"Checking\"\ncard = \"Credit Card\"\nsavings = \"Savings\"\n"
	if err := os.WriteFile(os.Getenv("DINERO_CONFIG_FILE"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(os.Getenv("DINERO_STATE_DIR"), "category_rules.json"), []byte(`{"Lunch":["Food","Dining"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	fx.Insert(t, "2026-10-13 00:00:00", "Existing coffee", 4.25, "Checking")
	fx.InsertNullable(t, "", "Historical null", "", "", nil, "", "")
}

func reportingCommand(t *testing.T, p plaid.Client, args ...string) (string, string, error) {
	t.Helper()
	var out, stderr bytes.Buffer
	root := cli.NewRoot(cli.Dependencies{PlaidClient: p})
	root.SetArgs(args)
	root.SetIn(strings.NewReader(""))
	root.SetOut(&out)
	root.SetErr(&stderr)
	err := root.ExecuteContext(context.Background())
	return out.String(), stderr.String(), err
}

// A regression to aggregate-only output would hide both the new and pending
// records the user needs to review before explicitly committing ingestion.
func TestTransactionsDefaultReportsAccountDetailsAndTotalsWithoutWriting(t *testing.T) {
	fx := testutil.NewSQLite(t)
	reportingConfig(t, fx)
	out, stderr, err := reportingCommand(t, reportingPlaid{}, "transactions", "--through", "2026-10-14")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Checking", "Credit Card", "Savings", "Lunch", "9.50", "Food", "Dining",
		"Pending gas", "35.00", "Card hold", "12.30", "Salary", "-100.00", "2026-10-14",
		"All accounts summary from 2026-07-16 to 2026-10-14",
		"Records in table: 2", "Transactions queried: 5", "Pending transactions: 2",
		"Transactions analysed (pending excluded): 3", "New records to be inserted: 2", "Existing transactions: 1",
		"\nDry run: no data was changed.\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
	for _, want := range []string{"bank_a", "bank_b", "3 downloaded, 1 pending, 2 not pending", "2 downloaded, 1 pending, 1 not pending"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("missing progress %q in stderr:\n%s", want, stderr)
		}
	}
	if strings.Contains(out, "Existing coffee") {
		t.Error("existing transactions should be counted without listing every record")
	}
	for _, secret := range []string{"synthetic-first-secret", "synthetic-second-secret"} {
		if strings.Contains(out+stderr, secret) {
			t.Error("progress leaked a credential")
		}
	}
	var count int
	if err := fx.DB.QueryRow("SELECT COUNT(*) FROM transactions").Scan(&count); err != nil || count != 2 {
		t.Fatalf("default preview changed the database: %d, %v", count, err)
	}
}

// Text reporting must never prepend human output or additional keys to JSON.
func TestTransactionsReportingPreservesJSONContractAndCommitDetails(t *testing.T) {
	fx := testutil.NewSQLite(t)
	reportingConfig(t, fx)
	out, stderr, err := reportingCommand(t, reportingPlaid{}, "transactions", "--through", "2026-10-14", "--json")
	if err != nil || stderr != "" {
		t.Fatalf("JSON preview: %v %q", err, stderr)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload) != 9 {
		t.Fatalf("JSON contract gained human reporting fields: %s", out)
	}
	for _, key := range []string{"status", "start", "through", "transactions_queried", "pending_transactions", "new_transactions", "existing_transactions", "missing_account_ids", "transactions"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("missing JSON field %s", key)
		}
	}
	out, _, err = reportingCommand(t, reportingPlaid{}, "transactions", "--through", "2026-10-14", "--yes")
	if err != nil || !strings.Contains(out, "Lunch") || !strings.Contains(out, "Salary") || !strings.Contains(out, "Inserted 2 transaction(s)") {
		t.Fatalf("commit should show the review and result: %v\n%s", err, out)
	}
	var count int
	if err := fx.DB.QueryRow("SELECT COUNT(*) FROM transactions").Scan(&count); err != nil || count != 4 {
		t.Fatalf("committed row count: %d %v", count, err)
	}
}

// A long first fetch should immediately identify its connection in the terminal.
func TestTransactionsShowsProgressBeforeProviderFetch(t *testing.T) {
	fx := testutil.NewSQLite(t)
	reportingConfig(t, fx)
	var out, stderr bytes.Buffer
	provider := reportingPlaid{beforeFetch: func(token string) {
		alias := "bank_a"
		if token == "synthetic-second-secret" {
			alias = "bank_b"
		}
		if !strings.Contains(stderr.String(), "Fetching transactions for "+alias) {
			t.Errorf("no progress for %s before fetch: %s", alias, stderr.String())
		}
	}}
	root := cli.NewRoot(cli.Dependencies{PlaidClient: provider})
	root.SetArgs([]string{"transactions", "--through", "2026-10-14", "--dry-run"})
	root.SetOut(&out)
	root.SetErr(&stderr)
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
}
