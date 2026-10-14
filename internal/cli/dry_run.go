package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

func writeDryRunFooter(out io.Writer) error {
	_, err := fmt.Fprintln(out, "Dry run: no data was changed.")
	return err
}

func emitDryRun(cmd *cobra.Command, jsonOutput bool, value any, message string) error {
	if err := emit(cmd, jsonOutput, value, strings.TrimRight(message, "\n")); err != nil {
		return err
	}
	if jsonOutput {
		return nil
	}
	return writeDryRunFooter(cmd.OutOrStdout())
}
