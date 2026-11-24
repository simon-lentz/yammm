package main

import (
	"github.com/spf13/cobra"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/schema"
)

func newValidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate <schema.yammm>",
		Short: "Validate a schema file and report diagnostics",
		Args:  cobra.ExactArgs(1),
		RunE:  withDiagnostics(runValidate),
	}
	registerModuleRootFlag(cmd)
	return cmd
}

func runValidate(cmd *cobra.Command, args []string, sink *cli.DiagnosticSink) error {
	path := args[0]
	absPath, err := schemaOperand(path)
	if err != nil {
		return err
	}

	moduleRoot, loadOpts, err := moduleRootOptions(cmd, sink)
	if err != nil {
		return err
	}
	s, loadErr := schema.Load(cmd.Context(), absPath, loadOpts...)
	return reportSchemaLoad(sink, s, moduleRoot, absPath, loadErr)
}
