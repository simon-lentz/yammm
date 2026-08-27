package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/diag"
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
	cmd.Flags().Bool("value-conformance", false, "report stored Timestamp/Date/UUID values that do not conform to their constraints")
	cmd.Flags().Bool("revalidate", false, "run every instance back through the validator and report findings as warnings")

	registerModuleRootFlag(cmd)
	return cmd
}

func runSnapshotVerify(cmd *cobra.Command, args []string) error {
	formatStr, _ := cmd.Flags().GetString("format")
	noColor, _ := cmd.Flags().GetBool("no-color")
	skipIntegrity, _ := cmd.Flags().GetBool("skip-integrity-check")
	valueConformance, _ := cmd.Flags().GetBool("value-conformance")
	revalidate, _ := cmd.Flags().GetBool("revalidate")

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
	moduleRoot, loadOpts, err := moduleRootOptions(cmd)
	if err != nil {
		return err
	}
	s, schemaResult := schema.Load(cmd.Context(), absSchemaPath, loadOpts...)
	pending, failed := reportSchemaLoad(cmd, outputFormat, noColor, s, moduleRoot, absSchemaPath, schemaResult)
	if failed {
		return &cli.ExitError{Code: cli.ExitValidation}
	}

	// Read snapshot file.
	data, err := os.ReadFile(snapshotPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: read snapshot file: %v\n", err)
		return &cli.ExitError{Code: cli.ExitRuntime}
	}

	// Build options.
	opts := []snapshot.LoadOption{
		snapshot.WithIntegrityCheck(!skipIntegrity),
		snapshot.WithValueConformance(valueConformance),
	}
	if revalidate {
		opts = append(opts, snapshot.WithRevalidation(diag.Warning))
	}

	// Verify.
	verifyResult := snapshot.Verify(cmd.Context(), data, s, opts...)

	// Render diagnostics — the load's residual warnings folded in, so one
	// invocation writes one result (and in JSON, one document).
	result := cli.MergeResults(pending, verifyResult)
	renderDiagnostics(cmd, outputFormat, noColor, s, diagRootFor(s, moduleRoot, absSchemaPath), result)

	exitCode := cli.ExitForResult(result)
	if exitCode != cli.ExitOK {
		return &cli.ExitError{Code: exitCode}
	}
	return nil
}
