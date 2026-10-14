package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/danielfrg/dinero/internal/config"
	"github.com/danielfrg/dinero/internal/ingest"
	"github.com/danielfrg/dinero/internal/maintenance"
	"github.com/danielfrg/dinero/internal/plaid"
	"github.com/danielfrg/dinero/internal/store"
	"github.com/spf13/cobra"
)

func yesEnvironment() bool { v, ok := os.LookupEnv("DINERO_YES"); return ok && v != "0" }
func confirm(cmd *cobra.Command, prompt string, yes bool, jsonOutput bool) error {
	if yes || yesEnvironment() {
		return nil
	}
	if jsonOutput {
		return &commandExit{code: 2, message: "--json requires --yes or DINERO_YES for writes"}
	}
	if _, err := fmt.Fprint(cmd.ErrOrStderr(), prompt+" [y/N] "); err != nil {
		return err
	}
	var answer string
	fmt.Fscanln(cmd.InOrStdin(), &answer)
	if strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes") {
		return nil
	}
	return &commandExit{code: 1, silent: true}
}
func loadMaintenance(cmd *cobra.Command, deps Dependencies, configPath *string, writable bool) (*maintenance.Service, *store.Store, error) {
	cfg, err := deps.LoadConfig(config.ResolvePath(*configPath))
	if err != nil {
		return nil, nil, err
	}
	dsn, err := cfg.DSN()
	if err != nil {
		return nil, nil, err
	}
	s, err := store.Open(cmd.Context(), dsn, !writable, cfg.Timezone)
	if err != nil {
		return nil, nil, err
	}
	return maintenance.New(s, cfg), s, nil
}
func emit(cmd *cobra.Command, jsonOutput bool, value any, message string) error {
	if jsonOutput {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(value)
	}
	_, err := fmt.Fprintln(cmd.OutOrStdout(), message)
	return err
}
func newInitDBCommand(deps Dependencies, configPath *string) *cobra.Command {
	var yes, jsonOutput, dryRun bool
	cmd := &cobra.Command{Use: "init-db", Short: "Create the legacy transactions table if absent", RunE: func(cmd *cobra.Command, _ []string) error {
		if dryRun {
			return emitDryRun(cmd, jsonOutput, map[string]string{"status": "dry-run"}, "Database initialization preview")
		}
		if err := confirm(cmd, "Create transactions table?", yes, jsonOutput); err != nil {
			return err
		}
		cfg, err := deps.LoadConfig(config.ResolvePath(*configPath))
		if err != nil {
			return err
		}
		dsn, err := cfg.DSN()
		if err != nil {
			return err
		}
		if err := maintenance.EnsureDatabase(cmd.Context(), dsn); err != nil {
			return err
		}
		svc, s, err := loadMaintenance(cmd, deps, configPath, true)
		if err != nil {
			return err
		}
		defer s.Close()
		if err := svc.InitDB(cmd.Context()); err != nil {
			return err
		}
		return emit(cmd, jsonOutput, map[string]string{"status": "created"}, "Transaction table ready")
	}}
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview without creating database or table")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "JSON output")
	return cmd
}
func newRulesCommand(deps Dependencies, configPath *string) *cobra.Command {
	var commit, yes, dryRun, jsonOutput bool
	cmd := &cobra.Command{Use: "mkrules", Short: "Generate frequent transaction category rules", RunE: func(cmd *cobra.Command, _ []string) error {
		svc, s, err := loadMaintenance(cmd, deps, configPath, false)
		if err != nil {
			return err
		}
		stats, err := svc.PreviewRules(cmd.Context())
		s.Close()
		if err != nil {
			return err
		}
		rules := map[string][2]string{}
		for _, r := range stats {
			rules[r.Description] = [2]string{r.Category, r.Subcategory}
		}
		message := "Most common transactions:\ndescription  category  subcategory  amount\n"
		for _, r := range stats {
			message += fmt.Sprintf("%s  %s  %s  %d\n", r.Description, r.Category, r.Subcategory, r.Amount)
		}
		if (!commit && !yesEnvironment()) || dryRun {
			return emitDryRun(cmd, jsonOutput, rules, message)
		}
		if err := confirm(cmd, "Save category rules?", yes, jsonOutput); err != nil {
			return err
		}
		if err := svc.SaveRules(cmd.Context(), rules); err != nil {
			return err
		}
		return emit(cmd, jsonOutput, rules, message+fmt.Sprintf("Saved %d rule(s)", len(rules)))
	}}
	f := cmd.Flags()
	f.BoolVar(&commit, "commit", false, "write rules")
	f.BoolVar(&dryRun, "dry-run", false, "preview only")
	f.BoolVar(&yes, "yes", false, "skip confirmation")
	f.BoolVar(&jsonOutput, "json", false, "JSON output")
	return cmd
}
func newCacheCommand(deps Dependencies, configPath *string) *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{Use: "build-cache", Short: "Write account and category cache", RunE: func(cmd *cobra.Command, _ []string) error {
		svc, s, err := loadMaintenance(cmd, deps, configPath, false)
		if err != nil {
			return err
		}
		defer s.Close()
		if err := svc.BuildCache(cmd.Context()); err != nil {
			return err
		}
		return emit(cmd, jsonOutput, map[string]string{"status": "built", "directory": filepath.Join(config.StateDir(), "cache")}, "Cache built")
	}}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "JSON output")
	return cmd
}
func newExportCommand(deps Dependencies, configPath *string) *cobra.Command {
	var directory string
	var jsonOutput bool
	cmd := &cobra.Command{Use: "mkdataset", Short: "Export CSV and SQLite dataset", RunE: func(cmd *cobra.Command, _ []string) error {
		svc, s, err := loadMaintenance(cmd, deps, configPath, false)
		if err != nil {
			return err
		}
		defer s.Close()
		if err := svc.Export(cmd.Context(), directory); err != nil {
			return err
		}
		return emit(cmd, jsonOutput, map[string]string{"status": "exported", "directory": directory}, "Dataset exported to "+directory)
	}}
	f := cmd.Flags()
	f.StringVar(&directory, "directory", "data", "export directory")
	f.BoolVar(&jsonOutput, "json", false, "JSON output")
	return cmd
}
func newTransactionsCommand(deps Dependencies, configPath *string) *cobra.Command {
	var days int
	var through string
	var commit, yes, dryRun, jsonOutput bool
	cmd := &cobra.Command{Use: "transactions", Short: "Fetch and optionally insert Plaid transactions", RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := deps.LoadConfig(config.ResolvePath(*configPath))
		if err != nil {
			return err
		}
		dsn, err := cfg.DSN()
		if err != nil {
			return err
		}
		provider := deps.PlaidClient
		if provider == nil && len(cfg.Plaid.Tokens) > 0 {
			provider, err = plaid.New(cfg.Plaid.ClientID, cfg.Plaid.Secret, cfg.Plaid.Env)
			if err != nil {
				return err
			}
		}
		reader, err := store.Open(cmd.Context(), dsn, true, cfg.Timezone)
		if err != nil {
			return err
		}
		var progress func(ingest.FetchProgress) error
		if !jsonOutput {
			progress = func(event ingest.FetchProgress) error {
				return printTransactionsProgress(cmd.ErrOrStderr(), event)
			}
		}
		preview, err := ingest.New(reader, cfg, provider).PreviewWithProgress(cmd.Context(), days, through, progress)
		reader.Close()
		if err != nil {
			return err
		}
		for _, id := range preview.MissingAccountIDs {
			fmt.Fprintf(cmd.ErrOrStderr(), "Plaid account ID %s has no mapping; add it under [plaid.account_id_to_name]. Using the ID as the account name.\n", id)
		}
		if !jsonOutput {
			if err := printTransactionsPreview(cmd.OutOrStdout(), preview); err != nil {
				return err
			}
		}
		if (!commit && !yes && !yesEnvironment()) || dryRun {
			return emitDryRun(cmd, jsonOutput, preview, fmt.Sprintf("%d new, %d existing, %d pending", preview.NewTransactions, preview.ExistingTransactions, preview.PendingTransactions))
		}
		if preview.NewTransactions == 0 {
			return emit(cmd, jsonOutput, preview, "No new transactions to insert.")
		}
		if err := confirm(cmd, "Insert new transactions?", yes, jsonOutput); err != nil {
			return err
		}
		writer, err := store.Open(cmd.Context(), dsn, false, cfg.Timezone)
		if err != nil {
			return errors.New("transactions not committed")
		}
		defer writer.Close()
		done, err := ingest.New(writer, cfg, provider).Commit(cmd.Context(), preview)
		if err != nil {
			return err
		}
		return emit(cmd, jsonOutput, done, fmt.Sprintf("Inserted %d transaction(s)", done.NewTransactions))
	}}
	f := cmd.Flags()
	f.IntVar(&days, "days", 90, "days of history")
	f.StringVar(&through, "through", "", "last date YYYY-MM-DD")
	f.BoolVar(&commit, "commit", false, "insert fetched transactions")
	f.BoolVar(&dryRun, "dry-run", false, "preview only")
	f.BoolVar(&yes, "yes", false, "skip confirmation")
	f.BoolVar(&jsonOutput, "json", false, "JSON output")
	return cmd
}
