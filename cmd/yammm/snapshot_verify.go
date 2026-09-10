package main

import (
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
		Long: `Validate a .ys file without materialising the snapshot.
Checks schema compatibility, structural integrity, and edge references.
No snapshot and no instance objects are built. The file is read whole, so
peak memory scales with the document's size.

Exit code 0 if valid. Exit code 1 if errors are found.
Warnings are rendered to stderr.`,
		Args: cobra.ExactArgs(2),
		RunE: withDiagnostics(runSnapshotVerify),
	}

	cmd.Flags().Bool("skip-integrity-check", false, "skip integrity hash verification (for hand-edited files)")
	cmd.Flags().Bool("value-conformance", false, "report stored Timestamp/Date/UUID values that do not conform to their constraints")
	cmd.Flags().Bool("revalidate", false, "run every instance back through the validator and report findings as warnings")

	registerModuleRootFlag(cmd)
	return cmd
}

func runSnapshotVerify(cmd *cobra.Command, args []string, sink *cli.DiagnosticSink) error {
	skipIntegrity, _ := cmd.Flags().GetBool("skip-integrity-check")
	valueConformance, _ := cmd.Flags().GetBool("value-conformance")
	revalidate, _ := cmd.Flags().GetBool("revalidate")

	schemaPath := args[0]
	snapshotPath := args[1]

	absSchemaPath, err := filepath.Abs(schemaPath)
	if err != nil {
		return cli.Usagef("resolve path %q: %v", schemaPath, err)
	}

	// Load schema.
	moduleRoot, loadOpts, err := moduleRootOptions(cmd)
	if err != nil {
		return err
	}
	s, schemaResult := schema.Load(cmd.Context(), absSchemaPath, loadOpts...)
	if err := reportSchemaLoad(sink, s, moduleRoot, absSchemaPath, schemaResult); err != nil {
		return err
	}

	// Read snapshot file.
	data, err := os.ReadFile(snapshotPath)
	if err != nil {
		return cli.Runtimef("read snapshot file: %v", err)
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
	sink.Add(snapshot.Verify(cmd.Context(), data, s, opts...))

	exitCode := cli.ExitForResult(sink.Result())
	if exitCode != cli.ExitOK {
		return &cli.ExitError{Code: exitCode}
	}
	return nil
}
