package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

func newRootCmd(version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "yammm",
		Short: "Schema validation DSL for graph-based data infrastructure",
		Long: `yammm is a schema validation DSL for graph-based data infrastructure.

Global flags:
  --format    Output format: "text" (default) or "json". It shapes the
              diagnostics every command writes, and the stdout payload of
              "snapshot info". Under "json" a command writes one JSON
              document per stream and suppresses its status summary.
  --no-color  Disable ANSI color in diagnostic output.

Data commands (check, load, export) also accept:
  --from    Data input format override: "json" or "csv".
            Auto-detected from file extension when not specified.`,
		Version:       version,
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	cmd.PersistentFlags().String("format", "text", "output format for diagnostics and for snapshot info's payload: text or json")
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

// withDiagnostics adapts a command that diagnoses to cobra's RunE. It admits
// the output-shaping flags before the command runs and renders the sink after
// it returns, so no command works under a format the CLI does not know and no
// return path leaves a diagnostic unrendered.
func withDiagnostics(run func(*cobra.Command, []string, *cli.DiagnosticSink) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		formatStr, _ := cmd.Flags().GetString("format")
		format, err := cli.ParseOutputFormat(formatStr)
		if err != nil {
			return err
		}
		noColor, _ := cmd.Flags().GetBool("no-color")
		// The terminal test reads the process stream while the writer is
		// cobra's, as it was when every render built its own renderer.
		sink := cli.NewDiagnosticSink(cmd.ErrOrStderr(), format, noColor, cli.IsTTY(os.Stderr.Fd()))
		defer sink.Render()
		return run(cmd, args, sink)
	}
}

// requireSubcommand is the RunE of a command that only groups others.
//
// A grouping command with no RunE is run by cobra as a success: it prints help
// and exits 0, so `yammm snapshot` in a script reports that the snapshot was
// taken. --help is handled before RunE and still prints help.
func requireSubcommand(cmd *cobra.Command, _ []string) error {
	return cli.Usagef("%q requires a subcommand; run %q to list them", cmd.CommandPath(), cmd.CommandPath()+" --help")
}
