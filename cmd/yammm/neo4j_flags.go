package main

import (
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
	cmd.Flags().Bool("node-keys", false, "emit NODE KEY instead of separate UNIQUE + NOT NULL for primary keys (Neo4j 5.7+, Enterprise; degrades to UNIQUE on --edition community)")
	cmd.Flags().Bool("scalar-types", true, "emit IS :: <TYPE> constraints for scalar properties")
	cmd.Flags().Bool("required-only-types", false, "restrict type constraints to required properties")
}

// labelOptions reads the label flags back into adapter options, refusing an
// empty separator and a prefix or separator whose composed label is not a
// Neo4j identifier. It refuses rather than sanitizes: a quietly repaired label
// is one the operator did not ask for, and every command calls it before work.
func labelOptions(cmd *cobra.Command) ([]neo4j.Option, error) {
	separator, _ := cmd.Flags().GetString("separator")
	prefix, _ := cmd.Flags().GetString("prefix")
	if separator == "" {
		return nil, cli.Usagef("--separator must not be empty: a label composed without one cannot be split back into its schema and type")
	}
	opts := []neo4j.Option{
		neo4j.WithLabelSeparator(separator),
		neo4j.WithLabelPrefix(prefix),
	}
	if err := neo4j.ValidateIdentifier(neo4j.New(opts...).Label(cmd.Context(), "schema", "Type"), "label"); err != nil {
		return nil, cli.Usagef("--prefix %q and --separator %q compose a label that is not a Neo4j identifier: %v", prefix, separator, err)
	}
	return opts, nil
}

// constraintOptions reads the label AND constraint-shape flags back into
// adapter options. It returns a usage error for an unrecognised edition.
func constraintOptions(cmd *cobra.Command) ([]neo4j.Option, error) {
	named, _ := cmd.Flags().GetBool("named")
	edition, _ := cmd.Flags().GetString("edition")
	nodeKeys, _ := cmd.Flags().GetBool("node-keys")
	scalarTypes, _ := cmd.Flags().GetBool("scalar-types")
	requiredOnly, _ := cmd.Flags().GetBool("required-only-types")

	opts, err := labelOptions(cmd)
	if err != nil {
		return nil, err
	}
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
		return nil, cli.Usagef("invalid edition %q: must be %q or %q", edition, "enterprise", "community")
	}
	return opts, nil
}
