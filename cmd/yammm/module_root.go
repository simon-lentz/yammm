package main

import (
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/schema"
)

// Every command that loads a schema takes --module-root with one meaning, on
// the neo4j_flags.go precedent: a flag one command has and another lacks is a
// schema that validates under `gen` and fails under `validate`.

// registerModuleRootFlag adds the shared --module-root flag.
func registerModuleRootFlag(cmd *cobra.Command) {
	cmd.Flags().String("module-root", "", "root directory for module-style imports (default: the nearest ancestor holding yammm.mod, else the schema's directory)")
}

// moduleRootOptions returns --module-root as an absolute root and the load
// options for it, including the sink's source capture so a failed load renders
// its excerpt. An unset flag yields "" and no root option, so the loader
// discovers the root; a given root is judged as a path operand is, and a
// working directory that cannot be read is a filesystem answer.
func moduleRootOptions(cmd *cobra.Command, sink *cli.DiagnosticSink) (string, []schema.LoadOption, error) {
	root, _ := cmd.Flags().GetString("module-root")
	capture := sink.CaptureSchemaSources()
	if !cmd.Flags().Changed("module-root") {
		return "", []schema.LoadOption{capture}, nil
	}
	if err := cli.CheckSourceOperand("module root", root); err != nil {
		return "", nil, err
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", nil, cli.Runtimef("resolve module root %q: %v", root, err)
	}
	return abs, []schema.LoadOption{schema.WithModuleRoot(abs), capture}, nil
}

// schemaOperand returns the absolute path of a schema operand, refusing one
// [cli.CheckSourceOperand] refuses. A working directory that cannot be read is a
// filesystem answer, so it exits 3.
func schemaOperand(path string) (string, error) {
	if err := cli.CheckSourceOperand("schema file", path); err != nil {
		return "", err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", cli.Runtimef("resolve schema file %q: %v", path, err)
	}
	return abs, nil
}
