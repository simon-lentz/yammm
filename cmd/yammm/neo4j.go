package main

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newNeo4jCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "neo4j",
		Short: "Neo4j database operations",
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return bindNeo4jFlags(cmd)
		},
	}

	cmd.PersistentFlags().String("uri", "", "Neo4j bolt URI (e.g., neo4j://localhost:7687) [$YAMMM_NEO4J_URI]")
	cmd.PersistentFlags().String("username", "neo4j", "Neo4j username [$YAMMM_NEO4J_USERNAME]")
	cmd.PersistentFlags().String("password", "", "Neo4j password [$YAMMM_NEO4J_PASSWORD]")
	cmd.PersistentFlags().String("database", "neo4j", "Neo4j database name [$YAMMM_NEO4J_DATABASE]")

	cmd.AddCommand(
		newNeo4jConstraintsCmd(),
		newNeo4jDiffCmd(),
		newNeo4jIntrospectCmd(),
	)

	return cmd
}

// bindNeo4jFlags binds Neo4j persistent flags to environment variables.
// When a flag is not set on the command line but the corresponding env var
// is present, the env value is written back into the cobra flag so that
// subcommands can continue using cmd.Flags().GetString() unchanged.
func bindNeo4jFlags(cmd *cobra.Command) error {
	v := viper.New()
	v.SetEnvPrefix("YAMMM_NEO4J")
	v.AutomaticEnv()

	flags := []string{"uri", "username", "password", "database"}
	for _, name := range flags {
		if err := v.BindPFlag(name, cmd.Flags().Lookup(name)); err != nil {
			return err
		}

		if !cmd.Flags().Changed(name) && v.IsSet(name) {
			if err := cmd.Flags().Set(name, v.GetString(name)); err != nil {
				return err
			}
		}
	}
	return nil
}
