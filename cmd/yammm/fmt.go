package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/format"
)

func newFmtCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fmt <schema.yammm>...",
		Short: "Format schema files",
		Long:  "Format .yammm schema files using canonical formatting (like gofmt).",
		Args:  cobra.MinimumNArgs(1),
		RunE:  runFmt,
	}

	cmd.Flags().BoolP("write", "w", false, "write result to source file instead of stdout")
	cmd.Flags().Bool("check", false,
		"print the path of every unformatted file and exit non-zero; prints nothing when all are formatted. "+
			"Line endings normalize to LF before formatting, so a CRLF file reports as unformatted")

	return cmd
}

// runFmt formats every path in argument order. A failure on one path does not
// stop the list, so one invocation reports every offender — what a pre-commit
// hook over a file list needs — and the exit code is the most severe any path
// produced rather than the last or the numerically largest.
func runFmt(cmd *cobra.Command, args []string) error {
	write, _ := cmd.Flags().GetBool("write")
	check, _ := cmd.Flags().GetBool("check")

	// The whole flag set is validated before any path is opened, so a
	// misconfigured invocation fails without having half-formatted a list.
	formatStr, _ := cmd.Flags().GetString("format")
	if _, err := cli.ParseOutputFormat(formatStr); err != nil {
		return err
	}

	// Hand-rolled rather than cobra's MarkFlagsMutuallyExclusive: the root sets
	// SilenceErrors and SilenceUsage, so cobra's own flag validation exits 2
	// printing nothing at all, and a hook misconfigured with both flags would
	// fail with no diagnosis.
	if check && write {
		return cli.Usagef("--check and --write are mutually exclusive")
	}

	errs := make([]error, 0, len(args))
	for _, path := range args {
		errs = append(errs, fmtPath(cmd, path, write, check))
	}
	return cli.JoinExitErrors(errs...)
}

// fmtPath formats one path and reports what went wrong with it, or nil.
func fmtPath(cmd *cobra.Command, path string, write, check bool) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	formatted, err := format.TokenStream(string(content))
	if err != nil {
		return cli.Validationf("%s: %v", path, err)
	}

	switch {
	case check:
		if formatted == string(content) {
			return nil
		}
		// gofmt -l: the path alone, nothing else — so the failure carries the
		// code and no message.
		fmt.Fprintln(cmd.OutOrStdout(), path)
		return &cli.ExitError{Code: cli.ExitValidation}

	case write:
		if formatted == string(content) {
			return nil // already formatted
		}

		if err := cli.WriteFile(path, []byte(formatted)); err != nil {
			return err
		}
		return nil

	default:
		fmt.Fprint(cmd.OutOrStdout(), formatted)
		return nil
	}
}
