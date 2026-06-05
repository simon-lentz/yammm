package main

import "github.com/spf13/cobra"

func newRootCmd(version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "yammm",
		Short: "Schema validation DSL for graph-based data infrastructure",
		Long: `yammm is a schema validation DSL for graph-based data infrastructure.

Global flags:
  --format    Diagnostic output format: "text" (default) or "json".
              Applies to all commands that produce diagnostics.
  --no-color  Disable ANSI color in diagnostic output.

Data commands (check, load, export) also accept:
  --from    Data input format override: "json" or "csv".
            Auto-detected from file extension when not specified.`,
		Version:       version,
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	cmd.PersistentFlags().String("format", "text", "output format: text or json")
	cmd.PersistentFlags().Bool("no-color", false, "disable ANSI color output")

	cmd.AddCommand(
		newValidateCmd(),
		newFmtCmd(),
		newCheckCmd(),
		newNeo4jCmd(),
		newLoadCmd(),
		newExportCmd(),
		newGenCmd(),
		newSnapshotCmd(),
	)

	return cmd
}
