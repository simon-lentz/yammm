package main

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/schema/cypher"
)

// `gen --to cypher` and `neo4j diff` must agree on the mapping they configure:
// the diff's desired side is exactly the script gen emits, so a flag one has
// and the other lacks is a plan reporting drift the operator never introduced.
// Registering and reading them here keeps one definition per flag.

// registerLabelFlags adds the flags that decide how a label is composed. usage
// prefixes each flag's help, so gen can name the target the flag applies to.
func registerLabelFlags(cmd *cobra.Command, usage string) {
	cmd.Flags().String("separator", "__", usage+"label separator between schema name and type name; an empty value means \"__\"")
	cmd.Flags().String("prefix", "", usage+"global label prefix, if the target graph was generated with one")
}

// registerConstraintFlags adds the flags that decide which constraints the
// generator produces, with usage prefixing each flag's help.
func registerConstraintFlags(cmd *cobra.Command, usage string) {
	cmd.Flags().String("edition", "enterprise", usage+"target Neo4j edition: enterprise or community")
	cmd.Flags().Bool("node-keys", false, usage+"emit NODE KEY instead of UNIQUE + NOT NULL for primary keys (Enterprise; UNIQUE alone on --edition community)")
	cmd.Flags().String("type-constraints", "all", usage+"properties that get an IS :: <TYPE> constraint: all, required or none")
}

// constraintConfig reads the label and constraint flags back into a mapping.
// It returns a usage error for an unrecognised edition or type-constraints
// value; the labels are judged over the loaded schema by
// [cypher.Config.CheckLabels], which every generator calls.
func constraintConfig(cmd *cobra.Command) (cypher.Config, error) {
	separator, _ := cmd.Flags().GetString("separator")
	prefix, _ := cmd.Flags().GetString("prefix")
	edition, _ := cmd.Flags().GetString("edition")
	nodeKeys, _ := cmd.Flags().GetBool("node-keys")
	typeConstraints, _ := cmd.Flags().GetString("type-constraints")

	c := cypher.Config{LabelPrefix: prefix, LabelSeparator: separator, NodeKeys: nodeKeys}
	switch strings.ToLower(edition) {
	case "enterprise":
		c.Edition = cypher.Enterprise
	case "community":
		c.Edition = cypher.Community
	default:
		return cypher.Config{}, cli.Usagef("invalid edition %q: must be %q or %q", edition, "enterprise", "community")
	}
	switch strings.ToLower(typeConstraints) {
	case "all":
		c.TypeConstraints = cypher.AllProperties
	case "required":
		c.TypeConstraints = cypher.RequiredProperties
	case "none":
		c.TypeConstraints = cypher.NoProperties
	default:
		return cypher.Config{}, cli.Usagef("invalid type-constraints %q: must be %q, %q or %q", typeConstraints, "all", "required", "none")
	}
	return c, nil
}
