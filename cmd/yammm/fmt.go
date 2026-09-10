package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/format"
	"github.com/simon-lentz/yammm/location"
)

func newFmtCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fmt <schema.yammm>...",
		Short: "Format schema files",
		Long:  "Format .yammm schema files using canonical formatting (like gofmt).",
		Args:  cobra.MinimumNArgs(1),
		RunE:  withDiagnostics(runFmt),
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
func runFmt(cmd *cobra.Command, args []string, sink *cli.DiagnosticSink) error {
	write, _ := cmd.Flags().GetBool("write")
	check, _ := cmd.Flags().GetBool("check")

	// Hand-rolled rather than cobra's MarkFlagsMutuallyExclusive: the root sets
	// SilenceErrors and SilenceUsage, so cobra's own flag validation exits 2
	// printing nothing at all, and a hook misconfigured with both flags would
	// fail with no diagnosis.
	if check && write {
		return cli.Usagef("--check and --write are mutually exclusive")
	}

	// A syntax diagnostic names its file relative to where fmt was run.
	if wd, err := os.Getwd(); err == nil {
		sink.SetSource(nil, wd)
	}

	errs := make([]error, 0, len(args))
	for _, path := range args {
		errs = append(errs, fmtPath(cmd, sink, path, write, check))
	}
	return cli.JoinExitErrors(errs...)
}

// fmtPath formats one path and reports what went wrong with it, or nil. A
// syntax error is a diagnostic, added to sink, as validate reports it.
func fmtPath(cmd *cobra.Command, sink *cli.DiagnosticSink, path string, write, check bool) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	formatted, err := format.TokenStream(string(content))
	if syntaxErr, ok := errors.AsType[*format.SyntaxError](err); ok {
		if result, ok := syntaxResult(path, syntaxErr.Issue); ok {
			sink.Add(result)
			return &cli.ExitError{Code: cli.ExitValidation}
		}
	}
	if err != nil {
		return formatFailure(path, err)
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

// syntaxResult places the formatter's syntax diagnostic in the file at path:
// the formatter parses text, so its issue names no source. It reports false
// when the issue has no position or the path names no source.
func syntaxResult(path string, iss diag.Issue) (diag.Result, bool) {
	if !iss.HasSpan() {
		return diag.Result{}, false
	}
	source, err := location.SourceIDFromPath(path)
	if err != nil {
		return diag.Result{}, false
	}
	span := iss.Span()
	span.Source = source
	c := diag.NewCollectorUnlimited()
	c.Collect(diag.FromIssue(iss).WithSpan(span).Build())
	return c.Result(), true
}

// formatFailure maps a formatter error to the exit code it earns. A refusal is
// the formatter's defect, not the input's, so a hook blocks the commit on exit 3
// rather than reporting the schema as bad.
func formatFailure(path string, err error) error {
	if errors.Is(err, format.ErrNotPreserved) {
		return cli.Runtimef("%s: %v; the file is left unchanged", path, err)
	}
	return cli.Validationf("%s: %v", path, err)
}
