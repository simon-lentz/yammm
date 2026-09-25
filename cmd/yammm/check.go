package main

import (
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/schema"
)

func newCheckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check <schema.yammm> <data-file>",
		Short: "Validate data against a schema",
		Long:  "Validate JSON or CSV data against a yammm schema: every instance, primary-key uniqueness, and every required association's target, as load does. A target the data holds but refuses is reported by its own refusal, not as missing. Nothing is written. Input format is auto-detected from file extension.",
		Args:  cobra.ExactArgs(2),
		RunE:  withDiagnostics(runCheck),
	}

	cmd.Flags().String("from", "", "input format override: json or csv")
	cmd.Flags().String("type", "", "type name for CSV data (required for single-type CSV)")
	cmd.Flags().String("type-column", "", "column name containing type names (for multi-type CSV)")

	registerModuleRootFlag(cmd)
	return cmd
}

func runCheck(cmd *cobra.Command, args []string, sink *cli.DiagnosticSink) error {
	schemaPath := args[0]
	dataPath := args[1]
	in, err := dataInputOf(cmd, dataPath)
	if err != nil {
		return err
	}

	absSchemaPath, err := filepath.Abs(schemaPath)
	if err != nil {
		return cli.Usagef("resolve path %q: %v", schemaPath, err)
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

	if _, err := assembleGraph(cmd, sink, s, in, nil); err != nil {
		return err
	}

	exitCode := cli.ExitForResult(sink.Result())
	if exitCode != cli.ExitOK {
		return &cli.ExitError{Code: exitCode}
	}
	return nil
}
