package main

import (
	"github.com/spf13/cobra"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/schema"
)

func newCheckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check <schema.yammm> <data-file>",
		Short: "Validate data against a schema",
		Long:  "Validate JSON data against a yammm schema: every instance, primary-key uniqueness, and every required association's target, as load does. A target the data holds but refuses is reported by its own refusal, not as missing. Nothing is written.",
		Args:  cobra.ExactArgs(2),
		RunE:  withDiagnostics(runCheck),
	}

	registerModuleRootFlag(cmd)
	return cmd
}

func runCheck(cmd *cobra.Command, args []string, sink *cli.DiagnosticSink) error {
	schemaPath := args[0]
	dataPath := args[1]
	if err := checkDataOperands(dataPath); err != nil {
		return err
	}

	absSchemaPath, err := schemaOperand(schemaPath)
	if err != nil {
		return err
	}

	// Load schema
	moduleRoot, loadOpts, err := moduleRootOptions(cmd, sink)
	if err != nil {
		return err
	}
	s, schemaResult := schema.Load(cmd.Context(), absSchemaPath, loadOpts...)
	if err := reportSchemaLoad(sink, s, moduleRoot, absSchemaPath, schemaResult); err != nil {
		return err
	}

	if _, err := assembleGraph(cmd, sink, s, []string{dataPath}, nil); err != nil {
		return err
	}

	return cli.Exit(cli.ExitForResult(sink.Result()))
}
