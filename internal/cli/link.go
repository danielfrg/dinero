package cli

import (
	"context"
	"errors"
	"fmt"
	"github.com/mattn/go-isatty"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"sort"
	"strings"
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
		r, e := plaid.RunLink(cmd.Context(), plaid.LinkOptions{ConfigPath: config.ResolvePath(*configPath), Alias: alias, Update: update != "", NoBrowser: noBrowser, Timeout: timeout, Provider: deps.LinkProvider, Clock: deps.LinkClock, OpenBrowser: deps.OpenBrowser, NameAccounts: accountNaming(cmd, deps, alias), ShowURL: func(url string) error { _, e := fmt.Fprintln(cmd.ErrOrStderr(), "Open Hosted Link: "+url); return e }})
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

func accountNaming(cmd *cobra.Command, deps Dependencies, alias string) func(context.Context, []plaid.LinkedAccount, map[string]string) (map[string]string, error) {
	return func(ctx context.Context, accounts []plaid.LinkedAccount, prior map[string]string) (map[string]string, error) {
		names := plaid.DefaultAccountNames(alias, accounts, prior)
		terminal := false
		if deps.IsTerminal != nil {
			terminal = deps.IsTerminal(cmd.InOrStdin())
		} else if file, ok := cmd.InOrStdin().(*os.File); ok {
			terminal = isatty.IsTerminal(file.Fd())
		}
		if !terminal {
			return names, nil
		}
		ordered := append([]plaid.LinkedAccount(nil), accounts...)
		sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
		for _, a := range ordered {
			if prior[a.ID] != "" {
				continue
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "Account name [%s]: ", names[a.ID]); err != nil {
				return nil, err
			}
			line, err := readAccountName(ctx, cmd.InOrStdin())
			if err != nil {
				return nil, err
			}
			if name := strings.TrimSpace(line); name != "" {
				names[a.ID] = name
			}
		}
		return names, nil
	}
}

// Poll a terminal/file descriptor before each read so cancellation never waits
// for the operator to press Enter. Canonical terminal input becomes readable
// only after a line is available; reading bytewise avoids buffered input loss.
func readAccountName(ctx context.Context, in io.Reader) (string, error) {
	var line strings.Builder
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if file, ok := in.(*os.File); ok {
			poll := []unix.PollFd{{Fd: int32(file.Fd()), Events: unix.POLLIN}}
			ready, err := unix.Poll(poll, 100)
			if err == unix.EINTR {
				continue
			}
			if err != nil {
				return "", err
			}
			if ready == 0 {
				continue
			}
		}
		var b [1]byte
		n, err := in.Read(b[:])
		if n > 0 {
			if b[0] == '\n' {
				return line.String(), nil
			}
			line.WriteByte(b[0])
			if line.Len() > 4096 {
				return "", errors.New("account name is too long")
			}
		}
		if err != nil {
			return "", err
		}
	}
}
