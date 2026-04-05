package neo4j

import (
	"context"
	"fmt"
	"strings"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// ConstraintKind enumerates the types of constraints that can be generated.
type ConstraintKind int

const (
	ConstraintUnique  ConstraintKind = iota // Primary key uniqueness
	ConstraintNotNull                       // Required property
	ConstraintType                          // Property type (scalar or list)
	ConstraintNodeKey                       // Combined uniqueness + NOT NULL (NODE KEY)
)

// Constraint is a structured representation of a single Neo4j constraint.
// Construct via [Adapter.ConstraintsStructured]; do not create directly.
type Constraint struct {
	Name       string         // Deterministic constraint name (empty if WithNamedConstraints(false))
	Kind       ConstraintKind // Constraint category
	Label      string         // Fully qualified Neo4j label (e.g., "msrb_emma__Issuer")
	Properties []string       // Property names involved in the constraint
	TypeExpr   string         // Neo4j type expression (e.g., "STRING", "LIST<STRING NOT NULL>")
	Statement  string         // Complete CREATE CONSTRAINT IF NOT EXISTS ... Cypher statement
}

// ConstraintsForSchema generates Neo4j 5 constraint statements from a yammm schema.
//
// Returns the same Cypher statements as [Adapter.ConstraintsStructured], but as
// raw strings rather than structured [Constraint] values.
func (a *Adapter) ConstraintsForSchema(ctx context.Context, s *schema.Schema) ([]string, diag.Result) {
	structured, result := a.ConstraintsStructured(ctx, s)
	if !result.OK() {
		return nil, result
	}
	stmts := make([]string, len(structured))
	for i, c := range structured {
		stmts[i] = c.Statement
	}
	return stmts, result
}

// ConstraintsStructured generates Neo4j 5 constraint statements from a yammm schema
// and returns them as structured [Constraint] values.
//
// The function produces up to four categories of constraints:
//
//   - UNIQUE:    For primary key properties. Single PKs produce simple UNIQUE;
//     composite PKs produce tuple UNIQUE: REQUIRE (n.a, n.b) IS UNIQUE.
//   - NOT NULL:  For required properties (including primary keys).
//   - TYPE:      For List<T> properties (REQUIRE n.prop IS :: LIST<TYPE>).
//   - SCALAR:    For scalar properties (REQUIRE n.prop IS :: STRING), if
//     [WithScalarTypeConstraints](true) is set (the default).
//
// Abstract types are skipped (they have no Neo4j label).
// Types with empty names are skipped.
// Label collisions are detected before generation (fail-fast).
// All identifiers (labels, property names) are validated against Neo4j rules.
//
// Returns the constraint statements in deterministic order:
// types in schema declaration order, within each type: UNIQUE/NODE KEY -> NOT NULL -> TYPE.
//
// If validation errors are found, returns (nil, result) where result contains
// all issues. Issues use [E_NEO4J_LABEL_COLLISION], [E_NEO4J_INVALID_IDENTIFIER],
// or [E_NEO4J_UNSUPPORTED_TYPE] codes.
func (a *Adapter) ConstraintsStructured(ctx context.Context, s *schema.Schema) ([]Constraint, diag.Result) {
	collector := diag.NewCollector(0)

	collisionResult := a.DetectLabelCollisions(ctx, s)
	collector.Merge(collisionResult)

	var constraints []Constraint

	for _, t := range s.TypesSlice() {
		name := t.Name()
		if name == "" || t.IsAbstract() {
			continue
		}

		label := a.Label(ctx, s.Name(), name)
		if label == "" {
			continue
		}

		if err := ValidateIdentifier(label, fmt.Sprintf("type %q label", name)); err != nil {
			issue := diag.NewIssue(diag.Error, E_NEO4J_INVALID_IDENTIFIER,
				fmt.Sprintf("invalid label for type %q: %s", name, err)).
				WithDetail(diag.DetailKeyFormat, "neo4j").
				WithDetail(diag.DetailKeyTypeName, name).
				WithDetail(detailKeyLabel, label).
				WithDetail(diag.DetailKeyDetail, err.Error()).
				Build()
			collector.Collect(issue)
			continue
		}

		typeConstraints := a.constraintsForType(ctx, t, label, collector)
		constraints = append(constraints, typeConstraints...)
	}

	// Edition gating: Community only supports UNIQUE.
	if a.config.edition == Community {
		filtered := make([]Constraint, 0, len(constraints))
		for _, c := range constraints {
			if c.Kind == ConstraintUnique {
				filtered = append(filtered, c)
			}
		}
		constraints = filtered
	}

	result := collector.Result()
	if !result.OK() {
		return nil, result
	}
	return constraints, result
}

// constraintsForType generates all constraints for a single type.
func (a *Adapter) constraintsForType(_ context.Context, t *schema.Type, label string, collector *diag.Collector) []Constraint {
	var constraints []Constraint

	// 1. PRIMARY KEY constraints (UNIQUE or NODE KEY).
	constraints = append(constraints, a.primaryKeyConstraints(t, label, collector)...)

	// 2. NOT NULL constraints for required properties.
	constraints = append(constraints, a.notNullConstraints(t, label, collector)...)

	// 3. LIST TYPE constraints.
	constraints = append(constraints, a.listTypeConstraints(t, label, collector)...)

	// 4. SCALAR TYPE constraints (if enabled).
	if a.config.scalarTypeConstraints {
		constraints = append(constraints, a.scalarTypeConstraints(t, label, collector)...)
	}

	return constraints
}

// primaryKeyConstraints generates UNIQUE or NODE KEY constraints for primary keys.
func (a *Adapter) primaryKeyConstraints(t *schema.Type, label string, collector *diag.Collector) []Constraint {
	pks := t.PrimaryKeysSlice()
	if len(pks) == 0 {
		return nil
	}

	var pkNames []string
	for _, pk := range pks {
		pkName := pk.Name()
		if pkName == "" {
			continue
		}
		if err := ValidateIdentifier(pkName, fmt.Sprintf("type %q primary key", t.Name())); err != nil {
			issue := diag.NewIssue(diag.Error, E_NEO4J_INVALID_IDENTIFIER,
				fmt.Sprintf("invalid primary key %q on type %q: %s", pkName, t.Name(), err)).
				WithDetail(diag.DetailKeyFormat, "neo4j").
				WithDetail(diag.DetailKeyTypeName, t.Name()).
				WithDetail(diag.DetailKeyPropertyName, pkName).
				WithDetail(diag.DetailKeyDetail, err.Error()).
				Build()
			collector.Collect(issue)
			continue
		}
		pkNames = append(pkNames, pkName)
	}

	if len(pkNames) == 0 {
		return nil
	}

	kind := ConstraintUnique
	suffix := "IS UNIQUE"
	if a.config.nodeKeyConstraints {
		kind = ConstraintNodeKey
		suffix = "IS NODE KEY"
	}

	var propExpr string
	if len(pkNames) == 1 {
		propExpr = "n." + pkNames[0]
	} else {
		refs := make([]string, len(pkNames))
		for i, pk := range pkNames {
			refs[i] = "n." + pk
		}
		propExpr = "(" + strings.Join(refs, ", ") + ")"
	}

	stmt := a.buildStatement(label, pkNames, kind,
		fmt.Sprintf("REQUIRE %s %s", propExpr, suffix))

	return []Constraint{{
		Name:       a.optionalName(label, pkNames, kind),
		Kind:       kind,
		Label:      label,
		Properties: pkNames,
		Statement:  stmt,
	}}
}

// notNullConstraints generates NOT NULL constraints for required properties.
func (a *Adapter) notNullConstraints(t *schema.Type, label string, collector *diag.Collector) []Constraint {
	var constraints []Constraint
	seen := make(map[string]struct{})

	// Build set of PK property names for NODE KEY dedup.
	pkNames := make(map[string]struct{})
	if a.config.nodeKeyConstraints {
		for _, pk := range t.PrimaryKeysSlice() {
			pkNames[pk.Name()] = struct{}{}
		}
	}

	for _, prop := range t.AllPropertiesSlice() {
		propName := prop.Name()
		if propName == "" || !prop.IsRequired() {
			continue
		}
		if _, exists := seen[propName]; exists {
			continue
		}
		// Skip PK properties when NODE KEY is used (NODE KEY implies NOT NULL).
		if a.config.nodeKeyConstraints {
			if _, isPK := pkNames[propName]; isPK {
				continue
			}
		}
		if err := ValidateIdentifier(propName, fmt.Sprintf("type %q property", t.Name())); err != nil {
			issue := diag.NewIssue(diag.Error, E_NEO4J_INVALID_IDENTIFIER,
				fmt.Sprintf("invalid property %q on type %q: %s", propName, t.Name(), err)).
				WithDetail(diag.DetailKeyFormat, "neo4j").
				WithDetail(diag.DetailKeyTypeName, t.Name()).
				WithDetail(diag.DetailKeyPropertyName, propName).
				WithDetail(diag.DetailKeyDetail, err.Error()).
				Build()
			collector.Collect(issue)
			seen[propName] = struct{}{}
			continue
		}
		seen[propName] = struct{}{}

		props := []string{propName}
		stmt := a.buildStatement(label, props, ConstraintNotNull,
			fmt.Sprintf("REQUIRE n.%s IS NOT NULL", propName))

		constraints = append(constraints, Constraint{
			Name:       a.optionalName(label, props, ConstraintNotNull),
			Kind:       ConstraintNotNull,
			Label:      label,
			Properties: props,
			Statement:  stmt,
		})
	}
	return constraints
}

// listTypeConstraints generates IS :: LIST<T> constraints for List properties.
func (a *Adapter) listTypeConstraints(t *schema.Type, label string, collector *diag.Collector) []Constraint {
	var constraints []Constraint
	seen := make(map[string]struct{})

	for _, prop := range t.AllPropertiesSlice() {
		propName := prop.Name()
		if propName == "" {
			continue
		}
		if _, exists := seen[propName]; exists {
			continue
		}
		if a.config.requiredOnlyTypeConstraints && !prop.IsRequired() {
			continue
		}

		c := schema.ResolveAlias(prop.Constraint())
		lc, ok := c.(schema.ListConstraint)
		if !ok {
			continue
		}

		elemType, err := neo4jListElementType(lc.Element())
		if err != nil {
			issue := diag.NewIssue(diag.Error, E_NEO4J_UNSUPPORTED_TYPE,
				fmt.Sprintf("unsupported list element type for property %q on type %q: %s", propName, t.Name(), err)).
				WithDetail(diag.DetailKeyFormat, "neo4j").
				WithDetail(diag.DetailKeyTypeName, t.Name()).
				WithDetail(diag.DetailKeyPropertyName, propName).
				WithDetail(diag.DetailKeyDetail, err.Error()).
				Build()
			collector.Collect(issue)
			seen[propName] = struct{}{}
			continue
		}
		if err := ValidateIdentifier(propName, fmt.Sprintf("type %q list property", t.Name())); err != nil {
			issue := diag.NewIssue(diag.Error, E_NEO4J_INVALID_IDENTIFIER,
				fmt.Sprintf("invalid list property %q on type %q: %s", propName, t.Name(), err)).
				WithDetail(diag.DetailKeyFormat, "neo4j").
				WithDetail(diag.DetailKeyTypeName, t.Name()).
				WithDetail(diag.DetailKeyPropertyName, propName).
				WithDetail(diag.DetailKeyDetail, err.Error()).
				Build()
			collector.Collect(issue)
			seen[propName] = struct{}{}
			continue
		}
		seen[propName] = struct{}{}

		typeExpr := "LIST<" + elemType + ">"
		props := []string{propName}
		stmt := a.buildStatement(label, props, ConstraintType,
			fmt.Sprintf("REQUIRE n.%s IS :: %s", propName, typeExpr))

		constraints = append(constraints, Constraint{
			Name:       a.optionalName(label, props, ConstraintType),
			Kind:       ConstraintType,
			Label:      label,
			Properties: props,
			TypeExpr:   typeExpr,
			Statement:  stmt,
		})
	}
	return constraints
}

// scalarTypeConstraints generates IS :: TYPE constraints for scalar properties.
func (a *Adapter) scalarTypeConstraints(t *schema.Type, label string, collector *diag.Collector) []Constraint {
	var constraints []Constraint
	seen := make(map[string]struct{})

	for _, prop := range t.AllPropertiesSlice() {
		propName := prop.Name()
		if propName == "" {
			continue
		}
		if _, exists := seen[propName]; exists {
			continue
		}
		if a.config.requiredOnlyTypeConstraints && !prop.IsRequired() {
			continue
		}

		c := schema.ResolveAlias(prop.Constraint())

		// Skip lists — handled by listTypeConstraints.
		if c.Kind() == schema.KindList {
			seen[propName] = struct{}{}
			continue
		}

		typeExpr, ok := neo4jScalarType(c)
		if !ok {
			seen[propName] = struct{}{}
			continue
		}

		if err := ValidateIdentifier(propName, fmt.Sprintf("type %q property", t.Name())); err != nil {
			issue := diag.NewIssue(diag.Error, E_NEO4J_INVALID_IDENTIFIER,
				fmt.Sprintf("invalid property %q on type %q: %s", propName, t.Name(), err)).
				WithDetail(diag.DetailKeyFormat, "neo4j").
				WithDetail(diag.DetailKeyTypeName, t.Name()).
				WithDetail(diag.DetailKeyPropertyName, propName).
				WithDetail(diag.DetailKeyDetail, err.Error()).
				Build()
			collector.Collect(issue)
			seen[propName] = struct{}{}
			continue
		}
		seen[propName] = struct{}{}

		props := []string{propName}
		stmt := a.buildStatement(label, props, ConstraintType,
			fmt.Sprintf("REQUIRE n.%s IS :: %s", propName, typeExpr))

		constraints = append(constraints, Constraint{
			Name:       a.optionalName(label, props, ConstraintType),
			Kind:       ConstraintType,
			Label:      label,
			Properties: props,
			TypeExpr:   typeExpr,
			Statement:  stmt,
		})
	}
	return constraints
}

// buildStatement constructs a complete CREATE CONSTRAINT ... Cypher statement.
func (a *Adapter) buildStatement(label string, properties []string, kind ConstraintKind, requireClause string) string {
	var b strings.Builder
	b.WriteString("CREATE CONSTRAINT ")
	if a.config.namedConstraints {
		b.WriteString(constraintName(label, properties, kind))
		b.WriteString(" ")
	}
	b.WriteString("IF NOT EXISTS FOR (n:")
	b.WriteString(label)
	b.WriteString(") ")
	b.WriteString(requireClause)
	return b.String()
}

// optionalName returns a constraint name if named constraints are enabled, otherwise "".
func (a *Adapter) optionalName(label string, properties []string, kind ConstraintKind) string {
	if !a.config.namedConstraints {
		return ""
	}
	return constraintName(label, properties, kind)
}

// constraintName generates a deterministic constraint name: {label}_{prop1}_{prop2}_{kind}.
func constraintName(label string, properties []string, kind ConstraintKind) string {
	var suffix string
	switch kind {
	case ConstraintUnique:
		suffix = "unique"
	case ConstraintNotNull:
		suffix = "not_null"
	case ConstraintType:
		suffix = "type"
	case ConstraintNodeKey:
		suffix = "node_key"
	}
	return label + "_" + strings.Join(properties, "_") + "_" + suffix
}

// neo4jScalarType maps a yammm constraint to a Neo4j scalar type expression.
// Returns ("", false) for constraints that cannot be expressed as Neo4j scalar types.
func neo4jScalarType(c schema.Constraint) (string, bool) {
	c = schema.ResolveAlias(c)
	switch c.Kind() {
	case schema.KindString:
		return "STRING", true
	case schema.KindUUID:
		return "STRING", true
	case schema.KindEnum:
		return "STRING", true
	case schema.KindPattern:
		return "STRING", true
	case schema.KindInteger:
		return "INTEGER", true
	case schema.KindFloat:
		return "FLOAT", true
	case schema.KindBoolean:
		return "BOOLEAN", true
	case schema.KindTimestamp:
		return "ZONED DATETIME", true
	case schema.KindDate:
		return "DATE", true
	case schema.KindVector:
		return "LIST<FLOAT NOT NULL>", true
	case schema.KindList:
		return "", false
	default:
		return "", false
	}
}

// neo4jListElementType maps a yammm list element constraint to a Neo4j type string.
// Returns the element type name used inside LIST<...> syntax (e.g., "STRING NOT NULL").
func neo4jListElementType(c schema.Constraint) (string, error) {
	c = schema.ResolveAlias(c)
	switch c.Kind() {
	case schema.KindString, schema.KindUUID, schema.KindEnum, schema.KindPattern:
		return "STRING NOT NULL", nil
	case schema.KindInteger:
		return "INTEGER NOT NULL", nil
	case schema.KindFloat:
		return "FLOAT NOT NULL", nil
	case schema.KindBoolean:
		return "BOOLEAN NOT NULL", nil
	case schema.KindTimestamp:
		return "ZONED DATETIME NOT NULL", nil
	case schema.KindDate:
		return "DATE NOT NULL", nil
	case schema.KindVector, schema.KindList, schema.KindAlias:
		return "", fmt.Errorf("%w: list element kind %v", ErrUnsupportedListElem, c.Kind())
	default:
		return "", fmt.Errorf("%w: unknown list element kind %v", ErrUnsupportedListElem, c.Kind())
	}
}
