package main

import (
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

Exit code 0 if valid, warnings included. Exit code 1 if the schema or the
snapshot draws an error. Exit code 2 for a usage error. Exit code 3 when a file
cannot be read, the run is cancelled, or for an internal fault.
Warnings are rendered to stderr.`,
		Args: cobra.ExactArgs(2),
		RunE: withDiagnostics(runSnapshotVerify),
	}

	cmd.Flags().Bool("skip-integrity-check", false, "skip integrity hash verification (for hand-edited files)")
	cmd.Flags().Bool("revalidate", false, "run root instances back through the validator and report findings as warnings")

	registerModuleRootFlag(cmd)
	return cmd
}

func runSnapshotVerify(cmd *cobra.Command, args []string, sink *cli.DiagnosticSink) error {
	skipIntegrity, _ := cmd.Flags().GetBool("skip-integrity-check")
	revalidate, _ := cmd.Flags().GetBool("revalidate")

	schemaPath := args[0]
	snapshotPath := args[1]

	absSchemaPath, err := schemaOperand(schemaPath)
	if err != nil {
		return err
	}
	if err := cli.CheckOperand("snapshot file", snapshotPath); err != nil {
		return err
	}

	// Load schema.
	moduleRoot, loadOpts, err := moduleRootOptions(cmd, sink)
	if err != nil {
		return err
	}
	s, loadErr := schema.Load(cmd.Context(), absSchemaPath, loadOpts...)
	if err := reportSchemaLoad(sink, s, moduleRoot, absSchemaPath, loadErr); err != nil {
		return err
	}

	// Read snapshot file.
	data, err := cli.ReadFile(snapshotPath)
	if err != nil {
		return cli.Runtimef("read snapshot file: %v", err)
	}

	// Build options.
	opts := []snapshot.Option{
		snapshot.WithIntegrityCheck(!skipIntegrity),
		sink.SnapshotWarnings(),
	}
	if revalidate {
		opts = append(opts, snapshot.WithRevalidation(diag.Warning))
	}

	// The exit answers for everything the sink holds, the schema load's
	// warnings among it.
	verifyErr := sink.AddSnapshot(snapshot.Verify(cmd.Context(), data, s, opts...))
	return cli.JoinExitErrors(verifyErr, cli.Exit(cli.ExitForResult(sink.Result())))
}
