package main

import (
	"github.com/spf13/cobra"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

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

// requireSubcommand is the RunE of a command that only groups others.
//
// A grouping command with no RunE is run by cobra as a success: it prints help
// and exits 0, so `yammm snapshot` in a script reports that the snapshot was
// taken. --help is handled before RunE and still prints help.
func requireSubcommand(cmd *cobra.Command, _ []string) error {
	return cli.Usagef("%q requires a subcommand; run %q to list them", cmd.CommandPath(), cmd.CommandPath()+" --help")
}
