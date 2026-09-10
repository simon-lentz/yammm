package main

import (
	"path/filepath"

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
	absPath, err := filepath.Abs(path)
	if err != nil {
		return cli.Usagef("resolve path %q: %v", path, err)
	}

	moduleRoot, loadOpts, err := moduleRootOptions(cmd)
	if err != nil {
		return err
	}
	s, result := schema.Load(cmd.Context(), absPath, loadOpts...)
	bindSchemaSource(sink, s, moduleRoot, absPath)
	sink.Add(result)

	exitCode := cli.ExitForResult(result)
	if exitCode != cli.ExitOK {
		return &cli.ExitError{Code: exitCode}
	}
	return nil
}
