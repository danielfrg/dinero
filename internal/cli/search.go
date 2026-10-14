package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/danielfrg/dinero/internal/config"
	"github.com/danielfrg/dinero/internal/ledger"
	"github.com/danielfrg/dinero/internal/store"
	"github.com/spf13/cobra"
)

type legacyTransaction struct {
	ID          int64    `json:"id"`
	Date        string   `json:"date"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Subcategory string   `json:"subcategory"`
	Amount      *float64 `json:"amount"`
	Account     string   `json:"account"`
	Notes       string   `json:"notes"`
}

func openQueryService(ctx context.Context, deps Dependencies, path string) (*ledger.Service, *store.Store, error) {
	cfg, err := deps.LoadConfig(config.ResolvePath(path))
	if err != nil {
		return nil, nil, err
	}
	dsn, err := cfg.DSN()
	if err != nil {
		return nil, nil, err
	}
	db, err := store.Open(ctx, dsn, true, cfg.Timezone)
	if err != nil {
		return nil, nil, err
	}
	return ledger.NewService(db, cfg), db, nil
}
func newSearchCommand(deps Dependencies, configPath *string) *cobra.Command {
	var account, category, subcategory, description, after, before, sort string
	var year, month, limit int
	var desc, asc, jsonOutput bool
	cmd := &cobra.Command{Use: "search", Short: "Search transactions", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Second)
		defer cancel()
		svc, db, err := openQueryService(ctx, deps, *configPath)
		if err != nil {
			return err
		}
		defer db.Close()
		q := ledger.Query{Sort: sort, Desc: desc && !asc, Limit: limit}
		if cmd.Flags().Changed("account") {
			q.Account = &account
		}
		if cmd.Flags().Changed("category") {
			q.Category = &category
		}
		if cmd.Flags().Changed("subcategory") {
			q.Subcategory = &subcategory
		}
		if cmd.Flags().Changed("description") {
			q.Description = &description
		}
		if cmd.Flags().Changed("after") {
			q.After = &after
		}
		if cmd.Flags().Changed("before") {
			q.Before = &before
		}
		if cmd.Flags().Changed("year") {
			q.Year = &year
		}
		if cmd.Flags().Changed("month") {
			q.Month = &month
		}
		page, err := svc.Search(ctx, q)
		if err != nil {
			return err
		}
		rows := make([]legacyTransaction, 0, len(page.Transactions))
		for _, tr := range page.Transactions {
			row := legacyTransaction{ID: tr.ID, Description: tr.Description, Category: tr.Category, Subcategory: tr.Subcategory, Amount: tr.Amount, Account: tr.Account, Notes: tr.Notes}
			if tr.Date != nil {
				row.Date = tr.Date.Format("2006-01-02")
			}
			if row.Amount != nil && (math.IsNaN(*row.Amount) || math.IsInf(*row.Amount, 0)) {
				row.Amount = nil
			}
			rows = append(rows, row)
		}
		if jsonOutput {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(rows)
		}
		if len(rows) == 0 {
			return nil
		}
		if _, err = fmt.Fprintln(cmd.OutOrStdout(), "id\tdate\tdescription\tcategory\tsubcategory\tamount\taccount\tnotes"); err != nil {
			return err
		}
		for _, row := range rows {
			amount := ""
			if row.Amount != nil {
				amount = fmt.Sprintf("%.2f", *row.Amount)
			}
			if _, err = fmt.Fprintf(cmd.OutOrStdout(), "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", row.ID, row.Date, row.Description, row.Category, row.Subcategory, amount, row.Account, row.Notes); err != nil {
				return err
			}
		}
		return nil
	}}
	flags := cmd.Flags()
	flags.StringVar(&account, "account", "", "account name")
	flags.StringVar(&category, "category", "", "category")
	flags.StringVar(&subcategory, "subcategory", "", "subcategory")
	flags.StringVar(&description, "description", "", "description contains pattern")
	flags.StringVar(&after, "after", "", "on or after YYYY-MM-DD")
	flags.StringVar(&before, "before", "", "on or before YYYY-MM-DD")
	flags.IntVar(&year, "year", 0, "year")
	flags.IntVar(&month, "month", 0, "month")
	flags.IntVar(&limit, "limit", 50, "maximum rows")
	flags.StringVar(&sort, "sort", "date", "sort column")
	flags.BoolVar(&desc, "desc", true, "descending order")
	flags.BoolVar(&asc, "asc", false, "ascending order")
	flags.BoolVar(&jsonOutput, "json", false, "JSON output")
	return cmd
}
