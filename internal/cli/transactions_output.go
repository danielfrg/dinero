package cli

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/danielfrg/dinero/internal/ingest"
)

func printTransactionsProgress(out io.Writer, progress ingest.FetchProgress) error {
	alias := transactionText(progress.Alias)
	if !progress.Completed {
		_, err := fmt.Fprintf(out, "Fetching transactions for %s (%s to %s)...\n", alias, progress.Start, progress.Through)
		return err
	}
	_, err := fmt.Fprintf(out, "%s: %d downloaded, %d pending, %d not pending\n", alias, progress.TransactionsQueried, progress.PendingTransactions, progress.TransactionsQueried-progress.PendingTransactions)
	return err
}

func printTransactionsPreview(out io.Writer, result ingest.Result) error {
	var body strings.Builder
	for _, account := range result.Accounts {
		fmt.Fprintf(&body, "%s\n%s\n%s\n", strings.Repeat("-", 80), transactionText(account.Account), strings.Repeat("-", 80))
		fmt.Fprintf(&body, "Queried transactions: %d\nPending transactions: %d\n", account.TransactionsQueried, account.PendingTransactions)
		writeTransactionDetails(&body, account.Pending)
		fmt.Fprintf(&body, "New transactions to be added: %d\n", account.NewTransactions)
		writeTransactionDetails(&body, account.Transactions)
		fmt.Fprintf(&body, "Existing transactions: %d\n\n", account.ExistingTransactions)
	}
	fmt.Fprintf(&body, "%s\nAll accounts summary from %s to %s:\n%s\n", strings.Repeat("=", 80), result.Start, result.Through, strings.Repeat("=", 80))
	fmt.Fprintf(&body, "Records in table: %d\nTransactions queried: %d\nPending transactions: %d\nTransactions analysed (pending excluded): %d\nNew records to be inserted: %d\nExisting transactions: %d\n\n", result.RecordsInTable, result.TransactionsQueried, result.PendingTransactions, result.TransactionsQueried-result.PendingTransactions, result.NewTransactions, result.ExistingTransactions)
	_, err := io.WriteString(out, body.String())
	return err
}

func writeTransactionDetails(body *strings.Builder, records []ingest.Record) {
	if len(records) == 0 {
		return
	}
	table := tabwriter.NewWriter(body, 0, 4, 2, ' ', 0)
	fmt.Fprintln(table, "date\tdescription\tamount\tcategory\tsubcategory")
	for _, record := range records {
		fmt.Fprintf(table, "%s\t%s\t%.2f\t%s\t%s\n", transactionText(record.Date), transactionText(record.Description), record.Amount, transactionText(record.Category), transactionText(record.Subcategory))
	}
	// The table writes only to a strings.Builder, which cannot fail.
	table.Flush()
}

func transactionText(value string) string {
	return strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(value)
}
