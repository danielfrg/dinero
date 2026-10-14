package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/spf13/cobra"
	"time"
)

func newBalanceCommand(deps Dependencies, configPath *string) *cobra.Command {
	var account, through string
	var jsonOutput bool
	cmd := &cobra.Command{Use: "balance", Short: "Compute account balance through a date", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Second)
		defer cancel()
		svc, db, err := openQueryService(ctx, deps, *configPath)
		if err != nil {
			return err
		}
		defer db.Close()
		b, err := svc.BalanceLegacy(ctx, account, through)
		if err != nil {
			return err
		}
		if jsonOutput {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
				Account string `json:"account"`
				Through string `json:"through"`
				Amount  string `json:"amount"`
			}{b.Account, b.Through, b.Amount})
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", b.Account, b.Through, b.Amount)
		return err
	}}
	cmd.Flags().StringVar(&account, "account", "", "account name")
	cmd.Flags().StringVar(&through, "through", "", "inclusive YYYY-MM-DD")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "JSON output")
	cmd.MarkFlagRequired("account")
	cmd.MarkFlagRequired("through")
	return cmd
}
