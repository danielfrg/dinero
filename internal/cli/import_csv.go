package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/danielfrg/dinero/internal/config"
	"github.com/danielfrg/dinero/internal/importcsv"
	"github.com/danielfrg/dinero/internal/store"
	"github.com/spf13/cobra"
)

type commandExit struct {
	code    int
	message string
	silent  bool
}

func (e *commandExit) Error() string { return e.message }

func newImportCSVCommand(deps Dependencies, configPath *string) *cobra.Command {
	var commit, dryRun, yes, jsonOutput bool
	cmd := &cobra.Command{Use: "import-csv FILE ACCOUNT", Short: "Preview or import transactions from CSV", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		file, account := args[0], args[1]
		input, err := os.Open(file)
		if os.IsNotExist(err) {
			return fmt.Errorf("File not found: %s", file)
		}
		if err != nil {
			return fmt.Errorf("cannot read CSV file")
		}
		defer input.Close()
		if dryRun {
			commit = false
		}
		if yes && !commit {
			return &commandExit{code: 2, message: "--yes requires --commit"}
		}
		cfg, err := deps.LoadConfig(config.ResolvePath(*configPath))
		if err != nil {
			return err
		}
		dsn, err := cfg.DSN()
		if err != nil {
			return err
		}
		reader, err := store.Open(cmd.Context(), dsn, true, cfg.Timezone)
		if err != nil {
			return err
		}
		preview, err := importcsv.New(reader, cfg).Preview(cmd.Context(), input, file, account)
		reader.Close()
		if err != nil {
			return err
		}
		if preview.Status == "ambiguous" {
			if err = writeImportResult(cmd.OutOrStdout(), preview, jsonOutput); err != nil {
				return err
			}
			return &commandExit{code: 1, silent: true}
		}
		if preview.Status == "no-transactions" || preview.Status == "no-new-transactions" || !commit {
			return writeImportResult(cmd.OutOrStdout(), preview, jsonOutput)
		}
		if jsonOutput && !yes {
			return &commandExit{code: 2, message: "--json --commit requires --yes"}
		}
		if !yes {
			if err = writeImportResult(cmd.OutOrStdout(), preview, false); err != nil {
				return err
			}
			if _, err = fmt.Fprint(cmd.ErrOrStderr(), "Import these transactions? [y/N] "); err != nil {
				return err
			}
			var answer string
			fmt.Fscanln(cmd.InOrStdin(), &answer)
			if !strings.EqualFold(answer, "y") && !strings.EqualFold(answer, "yes") {
				return &commandExit{code: 1, silent: true}
			}
		}
		writer, err := store.Open(cmd.Context(), dsn, false, cfg.Timezone)
		if err != nil {
			return &commandExit{code: 1, message: "Import failed; no transactions were committed"}
		}
		defer writer.Close()
		done, err := importcsv.New(writer, cfg).Commit(cmd.Context(), preview)
		if err != nil {
			return err
		}
		if done.Status == "ambiguous" {
			if err = writeImportResult(cmd.OutOrStdout(), done, jsonOutput); err != nil {
				return err
			}
			return &commandExit{code: 1, silent: true}
		}
		if !yes {
			if done.Status == "imported" {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "Imported %d transaction(s).\n", done.NewTransactions)
				return err
			}
			return writeImportResult(cmd.OutOrStdout(), done, false)
		}
		if err = writeImportResult(cmd.OutOrStdout(), done, jsonOutput); err != nil {
			return err
		}
		return nil
	}}
	flags := cmd.Flags()
	flags.BoolVar(&commit, "commit", false, "write imported transactions")
	flags.BoolVar(&dryRun, "dry-run", false, "preview without writing")
	flags.BoolVar(&yes, "yes", false, "skip confirmation with --commit")
	flags.BoolVar(&jsonOutput, "json", false, "JSON output")
	return cmd
}
func writeImportResult(out io.Writer, result importcsv.Result, jsonOutput bool) error {
	if jsonOutput {
		return json.NewEncoder(out).Encode(result)
	}
	if _, err := fmt.Fprintf(out, "%s\nImport from: %s\nAccount: %s\n%s\nStatus: %s\nTransactions in CSV: %d\nNew transactions: %d\nAlready existing (will skip): %d\nAmbiguous transactions: %d\n\n", strings.Repeat("=", 80), result.File, result.Account, strings.Repeat("=", 80), result.Status, result.TransactionsInCSV, result.NewTransactions, result.ExistingTransactions, result.AmbiguousTransactions); err != nil {
		return err
	}
	if len(result.Transactions) > 0 {
		if _, err := fmt.Fprintf(out, "%s\nNew transactions:\n%s\n", strings.Repeat("-", 80), strings.Repeat("-", 80)); err != nil {
			return err
		}
		for _, tr := range result.Transactions {
			cat := ""
			if tr.Category != "" {
				cat = " [" + tr.Category + "]"
			}
			if _, err := fmt.Fprintf(out, "  %s  %10s  %s%s\n", tr.Date, tr.Amount, tr.Description, cat); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(out); err != nil {
			return err
		}
	}
	for _, a := range result.Ambiguous {
		ids := []string{}
		for _, id := range a.ExistingIDs {
			ids = append(ids, fmt.Sprint(id))
		}
		if _, err := fmt.Fprintf(out, "Ambiguous: CSV row %d matches database IDs %s\n", a.Transaction.Row, strings.Join(ids, ", ")); err != nil {
			return err
		}
	}
	return nil
}
