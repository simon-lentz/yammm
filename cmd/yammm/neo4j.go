package main

import (
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func newNeo4jCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "neo4j",
		Short: "Neo4j database operations",
		// Without a RunE cobra prints help and exits 0, so a script that drops
		// the subcommand reports success having done nothing. --help still
		// preempts this, being handled before RunE runs.
		RunE: requireSubcommand,
		// A mistyped subcommand is reported as unknown, by name.
		Args: cobra.NoArgs,
	}

	cmd.AddCommand(newNeo4jDiffCmd())

	return cmd
}

// registerConnectionFlags adds the flags that name a database and its
// credentials, each also read from its YAMMM_NEO4J_* variable. Only a command
// that connects takes them, so none is accepted and then ignored.
func registerConnectionFlags(cmd *cobra.Command) {
	cmd.Flags().String("uri", "", "Neo4j bolt URI (e.g., neo4j://localhost:7687) [$YAMMM_NEO4J_URI]")
	cmd.Flags().String("username", "neo4j", "Neo4j username [$YAMMM_NEO4J_USERNAME]")
	cmd.Flags().String("password", "", "Neo4j password [$YAMMM_NEO4J_PASSWORD]")
	cmd.Flags().String("database", "neo4j", "Neo4j database name [$YAMMM_NEO4J_DATABASE]")
	cmd.PreRunE = func(cmd *cobra.Command, _ []string) error { return bindNeo4jFlags(cmd) }
}

// bindNeo4jFlags sets each connection flag the command line did not set from
// its YAMMM_NEO4J_* variable, YAMMM_NEO4J_URI for --uri. A variable set to
// the empty string is read as unset, so it keeps the flag's default.
func bindNeo4jFlags(cmd *cobra.Command) error {
	for _, name := range []string{"uri", "username", "password", "database"} {
		if cmd.Flags().Changed(name) {
			continue
		}
		if v := os.Getenv("YAMMM_NEO4J_" + strings.ToUpper(name)); v != "" {
			if err := cmd.Flags().Set(name, v); err != nil {
				return err
			}
		}
	}
	return nil
}
