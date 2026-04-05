package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/simon-lentz/yammm/adapter/neo4j"
	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/schema"
)

func newNeo4jConstraintsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "constraints <schema.yammm>",
		Short: "Generate Neo4j constraint Cypher statements from a schema",
		Args:  cobra.ExactArgs(1),
		RunE:  runNeo4jConstraints,
	}

	cmd.Flags().Bool("named", true, "generate named constraints")
	cmd.Flags().String("edition", "enterprise", "target Neo4j edition: enterprise or community")
	cmd.Flags().String("separator", "__", "label separator between schema name and type name")

	return cmd
}

func runNeo4jConstraints(cmd *cobra.Command, args []string) error {
	formatStr, _ := cmd.Flags().GetString("format")
	noColor, _ := cmd.Flags().GetBool("no-color")
	named, _ := cmd.Flags().GetBool("named")
	edition, _ := cmd.Flags().GetString("edition")
	separator, _ := cmd.Flags().GetString("separator")

	outputFormat, err := cli.ParseOutputFormat(formatStr)
	if err != nil {
		return err
	}

	schemaPath := args[0]
	absSchemaPath, err := filepath.Abs(schemaPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: resolve path %q: %v\n", schemaPath, err)
		return &cli.ExitError{Code: cli.ExitUsage}
	}

	// Load schema
	s, schemaResult := schema.Load(cmd.Context(), absSchemaPath)
	if schemaResult.HasErrors() {
		renderDiagnostics(cmd, outputFormat, noColor, s, filepath.Dir(absSchemaPath), schemaResult)
		return &cli.ExitError{Code: cli.ExitValidation}
	}

	// Configure adapter
	var opts []neo4j.Option
	opts = append(opts, neo4j.WithNamedConstraints(named))
	opts = append(opts, neo4j.WithLabelSeparator(separator))

	switch strings.ToLower(edition) {
	case "enterprise":
		opts = append(opts, neo4j.WithEdition(neo4j.Enterprise))
	case "community":
		opts = append(opts, neo4j.WithEdition(neo4j.Community))
	default:
		fmt.Fprintf(os.Stderr, "error: invalid edition %q: must be \"enterprise\" or \"community\"\n", edition)
		return &cli.ExitError{Code: cli.ExitUsage}
	}

	adapter := neo4j.New(opts...)

	// Generate constraints
	statements, result := adapter.ConstraintsForSchema(cmd.Context(), s)
	if result.HasErrors() {
		renderDiagnostics(cmd, outputFormat, noColor, nil, "", result)
		return &cli.ExitError{Code: cli.ExitValidation}
	}

	// Print each statement
	w := cmd.OutOrStdout()
	for _, stmt := range statements {
		fmt.Fprintln(w, stmt)
	}

	return nil
}
