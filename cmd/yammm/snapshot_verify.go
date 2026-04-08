package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

func newSnapshotVerifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify <schema.yammm> <snapshot.ys>",
		Short: "Validate a snapshot file against a schema",
		Long: `Validate a .ys file without loading the full snapshot into memory.
Checks schema compatibility, structural integrity, and edge references.

Exit code 0 if valid. Exit code 1 if errors are found.
Warnings are rendered to stderr.`,
		Args: cobra.ExactArgs(2),
		RunE: runSnapshotVerify,
	}

	cmd.Flags().Bool("skip-integrity-check", false, "skip integrity hash verification (for hand-edited files)")

	return cmd
}

func runSnapshotVerify(cmd *cobra.Command, args []string) error {
	formatStr, _ := cmd.Flags().GetString("format")
	noColor, _ := cmd.Flags().GetBool("no-color")
	skipIntegrity, _ := cmd.Flags().GetBool("skip-integrity-check")

	outputFormat, err := cli.ParseOutputFormat(formatStr)
	if err != nil {
		return err
	}

	schemaPath := args[0]
	snapshotPath := args[1]

	absSchemaPath, err := filepath.Abs(schemaPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: resolve path %q: %v\n", schemaPath, err)
		return &cli.ExitError{Code: cli.ExitUsage}
	}

	// Load schema.
	s, schemaResult := schema.Load(cmd.Context(), absSchemaPath)
	if schemaResult.HasErrors() {
		renderDiagnostics(cmd, outputFormat, noColor, s, filepath.Dir(absSchemaPath), schemaResult)
		return &cli.ExitError{Code: cli.ExitValidation}
	}

	// Read snapshot file.
	data, err := os.ReadFile(snapshotPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: read snapshot file: %v\n", err)
		return &cli.ExitError{Code: cli.ExitRuntime}
	}

	// Build options.
	var opts []snapshot.LoadOption
	if skipIntegrity {
		opts = append(opts, snapshot.WithSkipIntegrityCheck())
	}

	// Verify.
	result := snapshot.Verify(cmd.Context(), data, s, opts...)

	// Render diagnostics.
	renderDiagnostics(cmd, outputFormat, noColor, s, filepath.Dir(absSchemaPath), result)

	exitCode := cli.ExitForResult(result)
	if exitCode != cli.ExitOK {
		return &cli.ExitError{Code: exitCode}
	}
	return nil
}
