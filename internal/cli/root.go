package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/danielfrg/dinero/internal/config"
	"github.com/spf13/cobra"
)

const Version = "1.0.0-go"

// Dependencies lets commands share config access without global state.
type Dependencies struct {
	LoadConfig func(string) (config.Config, error)
}

func NewRoot(deps Dependencies) *cobra.Command {
	if deps.LoadConfig == nil {
		deps.LoadConfig = config.Load
	}
	var configPath string
	root := &cobra.Command{
		Use:           "dinero",
		Short:         "Personal finance tracking tools",
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&configPath, "config", "", "configuration file path")
	root.AddCommand(&cobra.Command{
		Use:   "config",
		Short: "Show the configuration with secrets redacted",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := config.ResolvePath(configPath)
			cfg, err := deps.LoadConfig(path)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintln(cmd.ErrOrStderr(), path); err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), cfg.Redacted())
			return err
		},
	})
	root.AddCommand(newSearchCommand(deps, &configPath), newBalanceCommand(deps, &configPath))
	return root
}

func Execute(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) int {
	root := NewRoot(Dependencies{})
	root.SetArgs(args)
	root.SetIn(in)
	root.SetOut(out)
	root.SetErr(errOut)
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(errOut, sanitizeError(err))
		return 1
	}
	return 0
}

func sanitizeError(err error) string {
	if os.IsNotExist(err) {
		return "configuration file not found"
	}
	return err.Error()
}
