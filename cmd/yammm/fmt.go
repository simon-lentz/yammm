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
// hook over a file list needs. The exit code is the most severe one any path
// produced, with a usage failure outranking a validation failure, so it does not
// depend on the order the paths were given in.
func runFmt(cmd *cobra.Command, args []string) error {
	write, _ := cmd.Flags().GetBool("write")
	check, _ := cmd.Flags().GetBool("check")

	// Hand-rolled rather than cobra's MarkFlagsMutuallyExclusive: the root sets
	// SilenceErrors and SilenceUsage, so cobra's own flag validation exits 2
	// printing nothing at all, and a hook misconfigured with both flags would
	// fail with no diagnosis.
	if check && write {
		fmt.Fprintln(os.Stderr, "error: --check and --write are mutually exclusive")
		return &cli.ExitError{Code: cli.ExitUsage}
	}

	worst := cli.ExitOK
	for _, path := range args {
		if code := fmtPath(cmd, path, write, check); code > worst {
			// ExitUsage (2) outranks ExitValidation (1) by value, which is the
			// precedence this needs.
			worst = code
		}
	}
	if worst != cli.ExitOK {
		return &cli.ExitError{Code: worst}
	}
	return nil
}

// fmtPath formats one path and returns the exit code it contributes.
func fmtPath(cmd *cobra.Command, path string, write, check bool) int {
	content, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return cli.ExitUsage
	}

	formatted, err := format.TokenStream(string(content))
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
		return cli.ExitValidation
	}

	switch {
	case check:
		if formatted == string(content) {
			return cli.ExitOK
		}
		// gofmt -l: the path alone, nothing else.
		fmt.Fprintln(cmd.OutOrStdout(), path)
		return cli.ExitValidation

	case write:
		if formatted == string(content) {
			return cli.ExitOK // already formatted
		}

		info, err := os.Stat(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: stat %s: %v\n", path, err)
			return cli.ExitUsage
		}

		if err := os.WriteFile(path, []byte(formatted), info.Mode().Perm()); err != nil {
			fmt.Fprintf(os.Stderr, "error: write %s: %v\n", path, err)
			return cli.ExitValidation
		}
		return cli.ExitOK

	default:
		fmt.Fprint(cmd.OutOrStdout(), formatted)
		return cli.ExitOK
	}
}
