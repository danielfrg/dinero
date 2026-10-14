package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/danielfrg/dinero/internal/config"
	"github.com/danielfrg/dinero/internal/plaid"
	"github.com/spf13/cobra"
)

func newLinkCommand(deps Dependencies, configPath *string) *cobra.Command {
	var update, resume string
	var noBrowser bool
	var timeout time.Duration
	cmd := &cobra.Command{Use: "link [NAME]", Short: "Connect or repair a Plaid Item using Hosted Link", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if timeout <= 0 {
			return errors.New("Link timeout must be positive")
		}
		if resume != "" {
			if len(args) != 0 || update != "" || cmd.Flags().Changed("no-browser") || cmd.Flags().Changed("timeout") {
				return errors.New("--resume accepts only a local recovery path and --config")
			}
			r, e := plaid.ResumeLink(cmd.Context(), resume, config.ResolvePath(*configPath))
			if e != nil {
				return e
			}
			_, e = fmt.Fprintf(cmd.OutOrStdout(), "Saved Link credentials for %s\n", r.Alias)
			return e
		}
		alias := update
		if update != "" && len(args) != 0 {
			return errors.New("use NAME or --update NAME")
		}
		if update == "" {
			if len(args) != 1 {
				return errors.New("Link alias is required")
			}
			alias = args[0]
		}
		r, e := plaid.RunLink(cmd.Context(), plaid.LinkOptions{ConfigPath: config.ResolvePath(*configPath), Alias: alias, Update: update != "", NoBrowser: noBrowser, Timeout: timeout, Provider: deps.LinkProvider, Clock: deps.LinkClock, OpenBrowser: deps.OpenBrowser, ShowURL: func(url string) error { _, e := fmt.Fprintln(cmd.ErrOrStderr(), "Open Hosted Link: "+url); return e }})
		if e != nil {
			return e
		}
		_, e = fmt.Fprintf(cmd.OutOrStdout(), "Saved Link credentials for %s (%d mapped accounts)\n", r.Alias, len(r.Accounts))
		return e
	}}
	cmd.Flags().StringVar(&update, "update", "", "repair an existing Item alias")
	cmd.Flags().StringVar(&resume, "resume", "", "resume persistence from a protected local recovery file")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "print the Hosted Link URL without opening a browser")
	cmd.Flags().DurationVar(&timeout, "timeout", plaid.DefaultLinkTimeout, "Hosted Link deadline")
	return cmd
}
