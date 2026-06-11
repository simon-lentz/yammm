package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/schema"
)

func newValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate <schema.yammm>",
		Short: "Validate a schema file and report diagnostics",
		Args:  cobra.ExactArgs(1),
		RunE:  runValidate,
	}
}

func runValidate(cmd *cobra.Command, args []string) error {
	formatStr, _ := cmd.Flags().GetString("format")
	noColor, _ := cmd.Flags().GetBool("no-color")

	outputFormat, err := cli.ParseOutputFormat(formatStr)
	if err != nil {
		return err
	}

	path := args[0]
	absPath, err := filepath.Abs(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: resolve path %q: %v\n", path, err)
		return &cli.ExitError{Code: cli.ExitUsage}
	}

	s, result := schema.Load(cmd.Context(), absPath)

	renderDiagnostics(cmd, outputFormat, noColor, s, diagRootFor(s, "", absPath), result)

	exitCode := cli.ExitForResult(result)
	if exitCode != cli.ExitOK {
		return &cli.ExitError{Code: exitCode}
	}
	return nil
}
