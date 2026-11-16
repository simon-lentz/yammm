package main

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/internal/mapping"
	"github.com/simon-lentz/yammm/schema/cypher"
)

// The neo4j subcommands must agree on the mapping they configure or their
// output cannot be compared: `diff` computes the desired side exactly as
// `constraints` and `indexes` emit it, so a flag one of them has and another
// lacks is a plan that reports drift the operator never introduced. Registering
// and reading them here keeps one definition per flag across all three.

// registerLabelFlags adds the flags that decide how a label is composed. Every
// neo4j subcommand takes them, because a label built differently from the one
// the target graph carries makes the schema's owned-label set disjoint from the
// database — every object reads as missing, with nothing saying why.
func registerLabelFlags(cmd *cobra.Command) {
	cmd.Flags().String("separator", "__", "label separator between schema name and type name; an empty value means \"__\"")
	cmd.Flags().String("prefix", "", "global label prefix, if the target graph was generated with one")
}

// registerConstraintFlags adds the flags that decide WHICH constraints the
// emitter produces. `indexes` does not take them — index emission is
// edition-independent and has no shape options — but `constraints` and `diff`
// must both take them, since the diff's desired side is this emitter's output.
func registerConstraintFlags(cmd *cobra.Command) {
	cmd.Flags().String("edition", "enterprise", "target Neo4j edition: enterprise or community")
	cmd.Flags().Bool("node-keys", false, "emit NODE KEY instead of separate UNIQUE + NOT NULL for primary keys (Enterprise; degrades to UNIQUE on --edition community)")
	cmd.Flags().Bool("scalar-types", true, "emit IS :: <TYPE> constraints for scalar, List and Vector properties")
	cmd.Flags().Bool("required-only-types", false, "restrict type constraints to required properties")
}

// labelConfig reads the label flags back into a mapping, refusing a prefix or
// separator whose composed label is not a Neo4j identifier. It refuses rather
// than sanitizes: a quietly repaired label is one the operator did not ask for,
// and every command calls it before work. The sample label is composed by the
// rule [cypher.Config.Label] applies.
func labelConfig(cmd *cobra.Command) (cypher.Config, error) {
	separator, _ := cmd.Flags().GetString("separator")
	prefix, _ := cmd.Flags().GetString("prefix")
	labels := mapping.Labels{Prefix: prefix, Separator: separator}
	if err := cypher.ValidateIdentifier(labels.Compose("schema", "Type"), "label"); err != nil {
		return cypher.Config{}, cli.Usagef("--prefix %q and --separator %q compose a label that is not a Neo4j identifier: %v", prefix, separator, err)
	}
	return cypher.Config{LabelPrefix: prefix, LabelSeparator: separator}, nil
}

// constraintConfig reads the label AND constraint-shape flags back into a
// mapping. It returns a usage error for an unrecognised edition.
func constraintConfig(cmd *cobra.Command) (cypher.Config, error) {
	edition, _ := cmd.Flags().GetString("edition")
	nodeKeys, _ := cmd.Flags().GetBool("node-keys")
	scalarTypes, _ := cmd.Flags().GetBool("scalar-types")
	requiredOnly, _ := cmd.Flags().GetBool("required-only-types")

	c, err := labelConfig(cmd)
	if err != nil {
		return cypher.Config{}, err
	}
	c.NodeKeys = nodeKeys
	switch {
	case !scalarTypes:
		c.TypeConstraints = cypher.NoProperties
	case requiredOnly:
		c.TypeConstraints = cypher.RequiredProperties
	default:
		c.TypeConstraints = cypher.AllProperties
	}

	switch strings.ToLower(edition) {
	case "enterprise":
		c.Edition = cypher.Enterprise
	case "community":
		c.Edition = cypher.Community
	default:
		return cypher.Config{}, cli.Usagef("invalid edition %q: must be %q or %q", edition, "enterprise", "community")
	}
	return c, nil
}
