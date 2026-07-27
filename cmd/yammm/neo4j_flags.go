package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/simon-lentz/yammm/adapter/neo4j"
	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

// The neo4j subcommands must agree on how the adapter is configured or their
// output cannot be compared: `diff` computes the desired side exactly as
// `constraints` and `indexes` emit it, so a flag one of them has and another
// lacks is a plan that reports drift the operator never introduced. Registering
// and reading them here keeps one definition per flag across all three.

// registerLabelFlags adds the flags that decide how a label is composed. Every
// neo4j subcommand takes them, because a label built differently from the one
// the target graph carries makes the schema's owned-label set disjoint from the
// database — every object reads as missing, with nothing saying why.
func registerLabelFlags(cmd *cobra.Command) {
	cmd.Flags().String("separator", "__", "label separator between schema name and type name")
	cmd.Flags().String("prefix", "", "global label prefix, if the target graph was generated with one")
}

// registerConstraintFlags adds the flags that decide WHICH constraints the
// emitter produces. `indexes` does not take them — index emission is
// edition-independent and has no shape options — but `constraints` and `diff`
// must both take them, since the diff's desired side is this emitter's output.
func registerConstraintFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("named", true, "generate named constraints")
	cmd.Flags().String("edition", "enterprise", "target Neo4j edition: enterprise or community")
	cmd.Flags().Bool("node-keys", false, "emit NODE KEY instead of separate UNIQUE + NOT NULL for primary keys (Neo4j 5.7+, Enterprise)")
	cmd.Flags().Bool("scalar-types", true, "emit IS :: <TYPE> constraints for scalar properties")
	cmd.Flags().Bool("required-only-types", false, "restrict type constraints to required properties")
}

// labelOptions reads the label flags back into adapter options.
func labelOptions(cmd *cobra.Command) []neo4j.Option {
	separator, _ := cmd.Flags().GetString("separator")
	prefix, _ := cmd.Flags().GetString("prefix")
	return []neo4j.Option{
		neo4j.WithLabelSeparator(separator),
		neo4j.WithLabelPrefix(prefix),
	}
}

// constraintOptions reads the label AND constraint-shape flags back into
// adapter options. It reports a usage error for an unrecognised edition, having
// already written the message.
func constraintOptions(cmd *cobra.Command) ([]neo4j.Option, error) {
	named, _ := cmd.Flags().GetBool("named")
	edition, _ := cmd.Flags().GetString("edition")
	nodeKeys, _ := cmd.Flags().GetBool("node-keys")
	scalarTypes, _ := cmd.Flags().GetBool("scalar-types")
	requiredOnly, _ := cmd.Flags().GetBool("required-only-types")

	opts := labelOptions(cmd)
	opts = append(
		opts,
		neo4j.WithNamedConstraints(named),
		neo4j.WithNodeKeyConstraints(nodeKeys),
		neo4j.WithScalarTypeConstraints(scalarTypes),
		neo4j.WithRequiredOnlyTypeConstraints(requiredOnly),
	)

	switch strings.ToLower(edition) {
	case "enterprise":
		opts = append(opts, neo4j.WithEdition(neo4j.Enterprise))
	case "community":
		opts = append(opts, neo4j.WithEdition(neo4j.Community))
	default:
		fmt.Fprintf(os.Stderr, "error: invalid edition %q: must be \"enterprise\" or \"community\"\n", edition)
		return nil, &cli.ExitError{Code: cli.ExitUsage}
	}
	return opts, nil
}
