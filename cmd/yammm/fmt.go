package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/format"
	"github.com/simon-lentz/yammm/internal/termtext"
	"github.com/simon-lentz/yammm/location"
)

func newFmtCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fmt [-l] [-w] <schema.yammm>...",
		Short: "Format schema files",
		Long: "Format .yammm schema files in canonical form, as gofmt formats Go.\n\n" +
			"With neither flag, fmt prints each formatted file to stdout. -l prints the path of each file whose " +
			"formatting differs and exits 0, as gofmt -l does, so a check reads the list and the exit code: " +
			"out=$(yammm fmt -l <schema.yammm>...) && test -z \"$out\". -w writes the formatted text back to each file that differs; " +
			"with -l it also prints that file's path. A file that does not parse exits 1.",
		Args: cobra.MinimumNArgs(1),
		RunE: withDiagnostics(runFmt),
	}

	cmd.Flags().BoolP("list", "l", false,
		"print the path of each file whose formatting differs instead of the formatted text. "+
			"Line endings normalize to LF before formatting, so a CRLF file is listed")
	cmd.Flags().BoolP("write", "w", false, "write the formatted text back to each file that differs, instead of to stdout")

	return cmd
}

// runFmt formats every path in argument order. A failure on one path does not
// stop the list, so one invocation reports every offender — what a pre-commit
// hook over a file list needs — and the exit code is the most severe any path
// produced rather than the last or the numerically largest.
func runFmt(cmd *cobra.Command, args []string, sink *cli.DiagnosticSink) error {
	list, _ := cmd.Flags().GetBool("list")
	write, _ := cmd.Flags().GetBool("write")

	// A syntax diagnostic names its file relative to where fmt was run.
	if wd, err := os.Getwd(); err == nil {
		sink.SetSource(nil, wd)
	}

	// Every operand is judged before any file is formatted, so a refused one
	// leaves every file as it was; each refusal is named.
	refusals := make([]error, 0, len(args))
	for _, path := range args {
		refusals = append(refusals, cli.CheckOperand("schema file", path))
	}
	if err := cli.JoinExitErrors(refusals...); err != nil {
		return err
	}

	errs := make([]error, 0, len(args))
	for _, path := range args {
		errs = append(errs, fmtPath(cmd, sink, path, list, write))
		// A file's diagnostics reach a terminal before the next file's output.
		sink.Flush()
	}
	return cli.JoinExitErrors(errs...)
}

// fmtPath formats one path and reports what went wrong with it, or nil. A
// syntax error is a diagnostic, added to sink, as validate reports it.
func fmtPath(cmd *cobra.Command, sink *cli.DiagnosticSink, path string, list, write bool) error {
	content, err := cli.ReadFile(path)
	if err != nil {
		return err
	}

	formatted, err := format.Source(content)
	if syntaxErr, ok := errors.AsType[*format.SyntaxError](err); ok {
		if result, ok := syntaxResult(path, content, syntaxErr.Issue); ok {
			sink.Add(result)
			return cli.Exit(cli.ExitForResult(result))
		}
	}
	if err != nil {
		return formatFailure(path, err)
	}

	if !list && !write {
		fmt.Fprintf(cmd.OutOrStdout(), "%s", formatted)
		return nil
	}
	if bytes.Equal(formatted, content) {
		return nil
	}
	if list {
		fmt.Fprintln(cmd.OutOrStdout(), termtext.String(path))
	}
	if write {
		return cli.WriteFile(path, formatted)
	}
	return nil
}

// syntaxResult places the formatter's syntax diagnostic in the file at path,
// whose bytes are content: the formatter parses text, so its issue names no
// source. It reports false when the issue has no position or the path names
// no source.
func syntaxResult(path string, content []byte, iss diag.Issue) (diag.Result, bool) {
	if !iss.HasSpan() {
		return diag.Result{}, false
	}
	source, err := location.SourceIDFromPath(path)
	if err != nil {
		return diag.Result{}, false
	}
	span := iss.Span()
	span.Source = source
	span.Start.Offset = fileOffset(content, span.Start.Offset)
	span.End.Offset = fileOffset(content, span.End.Offset)
	c := diag.NewCollectorUnlimited()
	c.Collect(diag.FromIssue(iss).WithSpan(span).Build())
	return c.Result(), true
}

// fileOffset returns the offset in content of the byte at offset in the text
// the formatter parsed, in which each CR LF of content is one LF; a lone CR
// keeps its one byte.
func fileOffset(content []byte, offset int) int {
	at := 0
	for range offset {
		if at+1 < len(content) && content[at] == '\r' && content[at+1] == '\n' {
			at++
		}
		at++
	}
	return at
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
