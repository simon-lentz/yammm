package neo4j

import (
	"context"
	"fmt"
	"slices"
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

// String returns the constraint kind's Neo4j remote type name, so a
// [Constraint] renders legibly wherever it is printed — a diagnostic, a test
// failure, or a consumer's log — instead of showing the bare iota ordinal.
// Matches [IndexKind.String].
func (k ConstraintKind) String() string {
	if s := constraintKindToRemoteType(k); s != unknownRemoteConstraintType {
		return s
	}
	return fmt.Sprintf("ConstraintKind(%d)", int(k))
}

// Constraint is a structured representation of a single Neo4j constraint.
// Construct via [Adapter.ConstraintsStructured]; do not create directly.
type Constraint struct {
	Name       string         // Deterministic constraint name (empty if WithNamedConstraints(false))
	Kind       ConstraintKind // Constraint category
	Label      string         // Fully qualified Neo4j label (e.g., "book_catalog__Publisher")
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

	// Reported once per call, not per type: the substitution is a fact about the
	// adapter's configuration, and repeating it for every type would scale the
	// noise with the schema while adding nothing.
	a.warnNodeKeyDegraded(collector)

	var constraints []Constraint
	for t, label := range a.emittableTypes(ctx, s, collector) {
		constraints = append(constraints, a.constraintsForType(ctx, t, label, collector)...)
	}

	disambiguateConstraintNames(constraints)

	// Edition gating, through the same list [Adapter.DiffConstraints] decides
	// declarability by, so the desired side of a diff and its actual side are
	// filtered identically.
	if kinds := a.declarableConstraintKinds(); len(kinds) < len(allConstraintKinds) {
		filtered := make([]Constraint, 0, len(constraints))
		omitted := make(map[ConstraintKind]int, len(allConstraintKinds))
		for _, c := range constraints {
			if slices.Contains(kinds, c.Kind) {
				filtered = append(filtered, c)
				continue
			}
			omitted[c.Kind]++
		}
		warnEditionOmitted(collector, omitted, len(constraints))
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

// useNodeKeyConstraints reports whether primary keys are encoded as NODE KEY.
//
// NODE KEY is Enterprise-only, so the request is honored only where the target
// can hold it. This must be the SINGLE source of that decision: two emitters
// consult it — [Adapter.primaryKeyConstraints] to choose the kind, and
// [Adapter.notNullConstraints] to decide whether a primary key's NOT NULL is
// already covered — and if they ever disagree, a primary key loses both its
// uniqueness (filtered away as a kind the edition cannot declare) and its NOT
// NULL (skipped as redundant), leaving it wholly unenforced.
//
// The edition cannot be consulted only in the post-emission filter. That filter
// drops by kind, and by then the NODE KEY carries no record that it stood in for
// UNIQUE + NOT NULL, so dropping it discards the UNIQUE half that Community
// supports perfectly well. The substitution has to happen before the kind is
// chosen, which is here. [TestConstraints_NodeKeyCommunity_MatchesPlainCommunity]
// pins the resulting invariant.
func (a *Adapter) useNodeKeyConstraints() bool {
	return a.config.nodeKeyConstraints && a.config.edition != Community
}

// warnNodeKeyDegraded reports the NODE KEY → UNIQUE substitution when the
// configuration asks for a kind the target edition cannot hold. Silence here
// would reproduce the failure this fallback exists to fix, only one layer up:
// the operator asked for stronger primary-key enforcement and must be told the
// request was adjusted rather than honored.
func (a *Adapter) warnNodeKeyDegraded(collector *diag.Collector) {
	// Expressed as "asked for, and declined" rather than by re-deriving the
	// edition test: whatever reasons [Adapter.useNodeKeyConstraints] comes to
	// decline for, the warning follows automatically instead of silently falling
	// out of step with it.
	if !a.config.nodeKeyConstraints || a.useNodeKeyConstraints() {
		return
	}
	issue := diag.NewIssue(diag.Warning, W_NEO4J_NODE_KEY_UNSUPPORTED,
		"NODE KEY constraints require Neo4j Enterprise; emitting UNIQUE for primary keys instead").
		WithDetail(diag.DetailKeyFormat, "neo4j").
		WithDetail(diag.DetailKeyDetail,
			"Community edition supports UNIQUE constraints only, so primary keys are enforced "+
				"as unique but not as NOT NULL. Target Enterprise to emit NODE KEY.").
		Build()
	collector.Collect(issue)
}

// warnEditionOmitted reports, once per call, how many constraints of each
// kind the edition filter dropped out of total. Kinds are listed in
// allConstraintKinds order so two runs render one message. Nothing is
// reported when nothing was dropped.
func warnEditionOmitted(collector *diag.Collector, omitted map[ConstraintKind]int, total int) {
	var parts []string
	dropped := 0
	for _, k := range allConstraintKinds {
		if n := omitted[k]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, constraintKindClause(k)))
			dropped += n
		}
	}
	if dropped == 0 {
		return
	}
	issue := diag.NewIssue(diag.Warning, W_NEO4J_EDITION_CONSTRAINT_OMITTED,
		fmt.Sprintf("the target Neo4j edition cannot hold %d of %d constraints; omitting %s",
			dropped, total, strings.Join(parts, ", "))).
		WithDetail(diag.DetailKeyFormat, "neo4j").
		WithDetail(diag.DetailKeyDetail,
			"Community edition supports UNIQUE constraints only: required properties are not "+
				"enforced as NOT NULL and property types are not enforced by the server. "+
				"Target Enterprise to emit them.").
		Build()
	collector.Collect(issue)
}

// constraintKindClause renders a kind in the DDL vocabulary the options and
// docs use, rather than the SHOW CONSTRAINTS type name String returns.
func constraintKindClause(k ConstraintKind) string {
	//exhaustive:enforce
	switch k {
	case ConstraintUnique:
		return "UNIQUE"
	case ConstraintNotNull:
		return "NOT NULL"
	case ConstraintType:
		return "PROPERTY_TYPE"
	case ConstraintNodeKey:
		return "NODE KEY"
	default:
		return k.String()
	}
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
	if a.useNodeKeyConstraints() {
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

	// Build set of PK property names for NODE KEY dedup. Gated on the same
	// predicate that chooses the kind: skipping a primary key's NOT NULL is only
	// safe where a NODE KEY is actually emitted to cover it.
	useNodeKey := a.useNodeKeyConstraints()
	pkNames := make(map[string]struct{})
	if useNodeKey {
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
		if useNodeKey {
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
	// //exhaustive:enforce because there is no default arm to fall into: a kind
	// added without a case here leaves suffix empty and silently emits a name
	// ending in a bare underscore, which then collides with every other unnamed
	// kind on the same label and properties.
	var suffix string
	//exhaustive:enforce
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
	//exhaustive:enforce
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
		// A Vector maps to a list of floats, NOT to Neo4j's native vector
		// property type, because a list of floats is what this adapter actually
		// writes: the write path passes a Vector through as a driver-native list
		// (the KindVector arm of [Coerce]), and valueType() on the stored property
		// returns exactly LIST<FLOAT NOT NULL>.
		//
		// Neo4j 5.x has no vector property type at all. Neo4j 2026.x does — spelled
		// VECTOR(<dimension>, <coordinate type>) — but a plain list of floats does
		// NOT satisfy it; only a value built with vector(...) does. Emitting it
		// here would therefore declare a constraint this adapter's own writes
		// violate on every insert.
		//
		// The cost is that the declared dimension is not enforced by the
		// constraint: a list of any length satisfies LIST<FLOAT NOT NULL>. The
		// dimension still reaches the database through a @vector index's
		// vector.dimensions, and instance validation rejects a wrong-length vector
		// before it is ever written, so the store-level check is the redundant
		// layer rather than the only one.
		//
		// Adopting the native type would need the write path to emit vector(...)
		// values and version gating for the 5.x floor — and note that the type is
		// WRITTEN as VECTOR(3, FLOAT32) but REPORTED by SHOW CONSTRAINTS as
		// VECTOR<FLOAT32 NOT NULL>(3), so the diff's TypeExpr-to-propertyType
		// comparison would need a normalizer, not just its case fold.
		return "LIST<FLOAT NOT NULL>", true
	case schema.KindList:
		return "", false
	case schema.KindAlias:
		// Unreachable in a completed schema: c is alias-resolved above. Listed to
		// satisfy the exhaustiveness guard; a List/Alias is not a Neo4j scalar type.
		return "", false
	default:
		return "", false
	}
}

// neo4jListElementType maps a yammm list element constraint to a Neo4j type string.
// Returns the element type name used inside LIST<...> syntax (e.g., "STRING NOT NULL").
func neo4jListElementType(c schema.Constraint) (string, error) {
	c = schema.ResolveAlias(c)
	//exhaustive:enforce
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

// disambiguateConstraintNames gives every emitted constraint a unique name,
// appending a short digest of its identity to each member of any group that
// would otherwise share one.
//
// [constraintName] joins the label and property names with underscores, and
// property names may themselves contain underscores, so the encoding is not
// injective ACROSS TYPES: a property `a_b` on type `Item` and a property `b` on
// type `Item_a` both render `{schema}__Item_a_b_not_null`. Because every emitted
// statement carries IF NOT EXISTS, the second CREATE is silently skipped and
// that constraint is never enforced — a NOT NULL or TYPE guarantee the schema
// declares and the database does not have.
//
// Mirrors [disambiguateIndexNames], including the reasons only colliding names
// are suffixed, why the digest is taken over the identity rather than a
// position, and the reservation of non-colliding names via [uniqueDigestName] —
// a suffixed name landing on a name some other constraint already holds
// unsuffixed would recreate the collision it exists to break.
//
// The rename is substituted into the rendered statement rather than re-rendering
// it. The name is the first thing after CREATE CONSTRAINT, so the first
// occurrence is always the name — the label, which the name embeds, appears
// later in the FOR clause.
func disambiguateConstraintNames(constraints []Constraint) {
	counts := make(map[string]int, len(constraints))
	for _, c := range constraints {
		if c.Name != "" {
			counts[c.Name]++
		}
	}
	taken := make(map[string]bool, len(constraints))
	for _, c := range constraints {
		if c.Name != "" && counts[c.Name] < 2 {
			taken[c.Name] = true
		}
	}
	for i, c := range constraints {
		if c.Name == "" || counts[c.Name] < 2 {
			continue
		}
		renamed := uniqueDigestName(c.Name, desiredSemanticKey(c), taken)
		taken[renamed] = true
		constraints[i].Name = renamed
		constraints[i].Statement = strings.Replace(c.Statement, c.Name, renamed, 1)
	}
}
