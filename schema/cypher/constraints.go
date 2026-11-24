package cypher

import (
	"fmt"
	"slices"
	"strings"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/mapping"
	"github.com/simon-lentz/yammm/schema"
)

// ConstraintKind enumerates the types of constraints that can be generated.
type ConstraintKind int

// The zero ConstraintKind names no kind.
const (
	ConstraintUnique  ConstraintKind = iota + 1 // Uniqueness of a key: a primary key, or a part's composed key
	ConstraintNotNull                           // Presence: a required property, or a part's composed key
	ConstraintType                              // Property type (scalar or list)
	ConstraintNodeKey                           // Combined uniqueness + NOT NULL (NODE KEY)
)

// String returns the constraint kind's Neo4j remote type name, so a
// [Constraint] renders legibly wherever it is printed — a diagnostic, a test
// failure, or a consumer's log — instead of showing the bare iota ordinal.
// Matches [IndexKind.String].
func (k ConstraintKind) String() string {
	if s := constraintKindToRemoteType(k); s != mapping.UnknownRemoteType {
		return s
	}
	return fmt.Sprintf("ConstraintKind(%d)", int(k))
}

// Constraint is a structured representation of a single Neo4j constraint.
// Construct via [Config.Constraints]; do not create directly.
type Constraint struct {
	Name       string         // Deterministic constraint name
	Kind       ConstraintKind // Constraint category
	Label      string         // Fully qualified Neo4j label (e.g., "book_catalog__Publisher")
	Properties []string       // Property names involved in the constraint
	TypeExpr   string         // Neo4j type expression (e.g., "STRING", "LIST<STRING NOT NULL>")
	Statement  string         // Complete CREATE CONSTRAINT IF NOT EXISTS ... Cypher statement
}

// Constraints generates Neo4j 5 constraint statements from a yammm schema
// and returns them as structured [Constraint] values.
//
// The function produces these constraints:
//
//   - UNIQUE:    For a primary key, on a type that is not a part: a key of one
//     property is a simple UNIQUE, and a composite key a tuple UNIQUE,
//     REQUIRE (n.a, n.b) IS UNIQUE.
//   - NODE KEY:  In place of UNIQUE under [Config.NodeKeys], on an
//     edition that holds it.
//   - NOT NULL:  For required properties, and for primary keys other than
//     those a NODE KEY constraint covers.
//   - TYPE:      For List<T> and Vector properties
//     (REQUIRE n.prop IS :: LIST<TYPE>), and for scalar properties
//     (REQUIRE n.prop IS :: STRING), on the properties [Config.TypeConstraints]
//     selects.
//
// A part type is keyed on its composed path: it emits a UNIQUE and a NOT NULL
// constraint on that path's property in place of its primary key's UNIQUE.
// Abstract types are skipped: no node carries their label.
// The labels are judged first by [Config.CheckLabels], and on its error the
// call returns that error alone. Every declared property name a returned
// statement writes is then judged by [IsIdentifier]: an invalid one draws one
// issue per declared property, naming the scope that declares it.
//
// Returns the constraint statements in deterministic order: types in import
// closure order, then schema declaration order; within each type: UNIQUE/NODE
// KEY -> NOT NULL -> TYPE.
//
// A refused schema returns nil beside a *[diag.ResultError] holding every
// failure: [E_NEO4J_INVALID_IDENTIFIER] and [E_NEO4J_UNSUPPORTED_TYPE]. The
// warnings [W_NEO4J_NODE_KEY_UNSUPPORTED] and
// [W_NEO4J_EDITION_CONSTRAINT_OMITTED] report what the edition changed; they go
// to [WithWarnings] once the labels pass [Config.CheckLabels], beside a later
// failure too, and never into the error.
// Constraints takes [WithWarnings], the one [Option].
func (c Config) Constraints(s schema.Schema, opts ...Option) ([]Constraint, error) {
	cfg := newCallConfig(opts)
	if err := c.CheckLabels(s); err != nil {
		return nil, err
	}
	collector := diag.NewCollector(diag.NoLimit)
	collector.RouteWarnings(cfg.warnings)

	// Reported once per call, not per type: the substitution is a fact about the
	// configuration, and repeating it for every type would scale the
	// noise with the schema while adding nothing.
	c.warnNodeKeyDegraded(collector)

	var constraints []Constraint
	names := c.newPropertyNameCheck(collector)
	for t, label := range c.labels().LabeledTypes(s) {
		constraints = append(constraints, c.constraintsForType(t, label, names, collector)...)
	}

	disambiguateConstraintNames(constraints)

	// Edition gating, through the same list the neo4j package's diff decides
	// declarability by, so the desired side of a diff and its actual side are
	// filtered identically.
	if kinds := c.declarableConstraintKinds(); len(kinds) < len(allConstraintKinds) {
		filtered := make([]Constraint, 0, len(constraints))
		omitted := make(map[ConstraintKind]int, len(allConstraintKinds))
		for _, con := range constraints {
			if slices.Contains(kinds, con.Kind) {
				filtered = append(filtered, con)
				continue
			}
			omitted[con.Kind]++
		}
		warnEditionOmitted(collector, omitted, len(constraints))
		constraints = filtered
	}

	if err := collector.Err(); err != nil {
		return nil, err
	}
	return constraints, nil
}

// constraintsForType generates all constraints for a single type.
//
// A part type keys its node on the composed path, never on its declared
// primary key: sibling-scoped uniqueness is not a Neo4j constraint, so a
// UNIQUE there would reject legal data. The declared key's NOT NULL and
// TYPE constraints still emit through the ordinary walks below.
func (c Config) constraintsForType(t schema.Type, label string, names *propertyNameCheck, collector *diag.Collector) []Constraint {
	var constraints []Constraint

	// 1. PRIMARY KEY constraints (UNIQUE or NODE KEY); the composed-key
	// pair for a part.
	if t.IsPart() {
		constraints = append(constraints, c.composedKeyConstraints(label)...)
	} else {
		constraints = append(constraints, c.primaryKeyConstraints(t, label, names)...)
	}

	// 2. NOT NULL constraints for required properties.
	constraints = append(constraints, c.notNullConstraints(t, label, names)...)

	// 3 and 4. PROPERTY-TYPE constraints — `REQUIRE n.p IS :: T` — for lists
	// and for scalars alike, under one gate.
	//
	// One gate because they are one kind of statement. A `Vector[4]` and a
	// `List<Float>` emit the SAME expression, `IS :: LIST<FLOAT NOT NULL>`, and
	// gating them differently meant an option suppressed the constraint for one
	// declaration and not for the other. The Community edition's gating also
	// drops them all.
	//
	// The list walk runs under either setting: it is the pass that judges a
	// list's element, and the gate is on emission, never on that judgement.
	constraints = append(constraints, c.listTypeConstraints(t, label, names, collector)...)
	if c.TypeConstraints != NoProperties {
		constraints = append(constraints, c.scalarTypeConstraints(t, label, names)...)
	}

	return constraints
}

// propertyNameCheck judges the property names one call's constraint statements
// write as Neo4j identifiers. Its subject is the declared property, as the index
// pass's is ([reportedTargets]): one rename fixes every heir and every statement
// that writes the name, so it is reported once, naming the scope that declares
// it. A statement of a kind the edition does not declare is dropped before it is
// returned, so it writes no name and judges none.
type propertyNameCheck struct {
	kinds     []ConstraintKind
	valid     map[schema.Property]bool
	collector *diag.Collector
}

func (c Config) newPropertyNameCheck(collector *diag.Collector) *propertyNameCheck {
	return &propertyNameCheck{
		kinds:     c.declarableConstraintKinds(),
		valid:     make(map[schema.Property]bool),
		collector: collector,
	}
}

// writes reports whether a statement of kind on t may write prop's name, which
// the pass calls what ("primary key", "property", "list property"). A statement
// of a kind the edition drops is built, so the omission warning counts it. A
// name judged invalid is reported once and written by no returned statement.
func (c *propertyNameCheck) writes(t schema.Type, prop schema.Property, what string, kind ConstraintKind) bool {
	if !slices.Contains(c.kinds, kind) {
		return true
	}
	origin := prop.Origin()
	if valid, judged := c.valid[origin]; judged {
		return valid
	}
	name := prop.Name()
	scope := targetScope(t, prop)
	problem := mapping.IdentifierProblem(name)
	c.valid[origin] = problem == ""
	if problem != "" {
		scope.Property = name
		reason := "the name " + problem
		c.collector.Collect(mapping.InvalidIdentifierIssue(
			fmt.Sprintf("invalid %s %q declared in %s: %s", what, name, scope, reason),
			scope,
			reason,
		).Build())
	}
	return problem == ""
}

// useNodeKeyConstraints reports whether primary keys are encoded as NODE KEY.
//
// NODE KEY is Enterprise-only, so the request is honored only where the target
// can hold it. This must be the SINGLE source of that decision: two emitters
// consult it — [Config.primaryKeyConstraints] to choose the kind, and
// [Config.notNullConstraints] to decide whether a primary key's NOT NULL is
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
func (c Config) useNodeKeyConstraints() bool {
	return c.NodeKeys && !c.community()
}

// warnNodeKeyDegraded reports the NODE KEY → UNIQUE substitution when the
// configuration asks for a kind the target edition cannot hold. Silence here
// would reproduce the failure this fallback exists to fix, only one layer up:
// the operator asked for stronger primary-key enforcement and must be told the
// request was adjusted rather than honored.
func (c Config) warnNodeKeyDegraded(collector *diag.Collector) {
	// Expressed as "asked for, and declined" rather than by re-deriving the
	// edition test: whatever reasons [Config.useNodeKeyConstraints] comes to
	// decline for, the warning follows automatically instead of silently falling
	// out of step with it.
	if !c.NodeKeys || c.useNodeKeyConstraints() {
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
func (c Config) primaryKeyConstraints(t schema.Type, label string, names *propertyNameCheck) []Constraint {
	pks := slices.Collect(t.PrimaryKeys())
	if len(pks) == 0 {
		return nil
	}

	kind := ConstraintUnique
	suffix := "IS UNIQUE"
	if c.useNodeKeyConstraints() {
		kind = ConstraintNodeKey
		suffix = "IS NODE KEY"
	}

	var pkNames []string
	for _, pk := range pks {
		pkName := pk.Name()
		if pkName == "" || !names.writes(t, pk, "primary key", kind) {
			continue
		}
		pkNames = append(pkNames, pkName)
	}

	if len(pkNames) == 0 {
		return nil
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

	stmt := c.buildStatement(label, pkNames, kind,
		fmt.Sprintf("REQUIRE %s %s", propExpr, suffix))

	return []Constraint{{
		Name:       constraintName(label, pkNames, kind),
		Kind:       kind,
		Label:      label,
		Properties: pkNames,
		Statement:  stmt,
	}}
}

// composedKeyConstraints emits the part-label identity pair: UNIQUE and
// NOT NULL on the composed key the write path assigns to every part node.
func (c Config) composedKeyConstraints(label string) []Constraint {
	props := []string{mapping.ComposedKeyProp}
	unique := c.buildStatement(label, props, ConstraintUnique,
		fmt.Sprintf("REQUIRE n.%s IS UNIQUE", mapping.ComposedKeyProp))
	notNull := c.buildStatement(label, props, ConstraintNotNull,
		fmt.Sprintf("REQUIRE n.%s IS NOT NULL", mapping.ComposedKeyProp))
	return []Constraint{
		{
			Name:       constraintName(label, props, ConstraintUnique),
			Kind:       ConstraintUnique,
			Label:      label,
			Properties: []string{mapping.ComposedKeyProp},
			Statement:  unique,
		},
		{
			Name:       constraintName(label, props, ConstraintNotNull),
			Kind:       ConstraintNotNull,
			Label:      label,
			Properties: []string{mapping.ComposedKeyProp},
			Statement:  notNull,
		},
	}
}

// notNullConstraints generates NOT NULL constraints for required properties.
func (c Config) notNullConstraints(t schema.Type, label string, names *propertyNameCheck) []Constraint {
	var constraints []Constraint
	seen := make(map[string]struct{})

	// Build set of PK property names for NODE KEY dedup. Gated on the same
	// predicate that chooses the kind: skipping a primary key's NOT NULL is only
	// safe where a NODE KEY is actually emitted to cover it — and never for a
	// part, whose declared key gets no NODE KEY, so skipping would strip the
	// one guarantee the key still has.
	useNodeKey := c.useNodeKeyConstraints() && !t.IsPart()
	pkNames := make(map[string]struct{})
	if useNodeKey {
		for pk := range t.PrimaryKeys() {
			pkNames[pk.Name()] = struct{}{}
		}
	}

	for prop := range t.AllProperties() {
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
		seen[propName] = struct{}{}
		if !names.writes(t, prop, "property", ConstraintNotNull) {
			continue
		}

		props := []string{propName}
		stmt := c.buildStatement(label, props, ConstraintNotNull,
			fmt.Sprintf("REQUIRE n.%s IS NOT NULL", propName))

		constraints = append(constraints, Constraint{
			Name:       constraintName(label, props, ConstraintNotNull),
			Kind:       ConstraintNotNull,
			Label:      label,
			Properties: props,
			Statement:  stmt,
		})
	}
	return constraints
}

// listTypeConstraints generates IS :: LIST<T> constraints for List properties,
// and refuses a list whose element Neo4j cannot hold under every setting, the
// ones that emit no such constraint included.
func (c Config) listTypeConstraints(t schema.Type, label string, names *propertyNameCheck, collector *diag.Collector) []Constraint {
	var constraints []Constraint
	seen := make(map[string]struct{})

	for prop := range t.AllProperties() {
		propName := prop.Name()
		if propName == "" {
			continue
		}
		if _, exists := seen[propName]; exists {
			continue
		}
		pc := schema.ResolveAlias(prop.Constraint())
		lc, ok := pc.(schema.ListConstraint)
		if !ok {
			continue
		}

		elemType, err := mapping.ListElementType(lc.Element())
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
		// The options gate emission, never the element check above: docs/SPEC.md
		// rejects a nested-collection property with E_NEO4J_UNSUPPORTED_TYPE
		// unconditionally. They are read before the name is checked, since with
		// no statement to hold it the name is not this pass's to refuse.
		if !c.typeConstraintsFor(prop.IsRequired()) {
			continue
		}
		seen[propName] = struct{}{}
		if !names.writes(t, prop, "list property", ConstraintType) {
			continue
		}

		typeExpr := "LIST<" + elemType + ">"
		props := []string{propName}
		stmt := c.buildStatement(label, props, ConstraintType,
			fmt.Sprintf("REQUIRE n.%s IS :: %s", propName, typeExpr))

		constraints = append(constraints, Constraint{
			Name:       constraintName(label, props, ConstraintType),
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
func (c Config) scalarTypeConstraints(t schema.Type, label string, names *propertyNameCheck) []Constraint {
	var constraints []Constraint
	seen := make(map[string]struct{})

	for prop := range t.AllProperties() {
		propName := prop.Name()
		if propName == "" {
			continue
		}
		if _, exists := seen[propName]; exists {
			continue
		}
		if !c.typeConstraintsFor(prop.IsRequired()) {
			continue
		}

		pc := schema.ResolveAlias(prop.Constraint())

		// Skip lists — handled by listTypeConstraints.
		if pc.Kind() == schema.KindList {
			seen[propName] = struct{}{}
			continue
		}

		typeExpr, ok := neo4jScalarType(pc)
		if !ok {
			seen[propName] = struct{}{}
			continue
		}

		seen[propName] = struct{}{}
		if !names.writes(t, prop, "property", ConstraintType) {
			continue
		}

		props := []string{propName}
		stmt := c.buildStatement(label, props, ConstraintType,
			fmt.Sprintf("REQUIRE n.%s IS :: %s", propName, typeExpr))

		constraints = append(constraints, Constraint{
			Name:       constraintName(label, props, ConstraintType),
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
func (c Config) buildStatement(label string, properties []string, kind ConstraintKind, requireClause string) string {
	var b strings.Builder
	b.WriteString("CREATE CONSTRAINT ")
	b.WriteString(constraintName(label, properties, kind))
	b.WriteString(" ")
	b.WriteString("IF NOT EXISTS FOR (n:")
	b.WriteString(label)
	b.WriteString(") ")
	b.WriteString(requireClause)
	return b.String()
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
		// property type, because a list of floats is what the write path actually
		// writes: the neo4j write path types a []any Vector as []float64 and
		// sends a []float64 as it is, and valueType() on the
		// stored property returns exactly LIST<FLOAT NOT NULL>.
		//
		// Neo4j 5.x has no vector property type at all. Neo4j 2026.x does — spelled
		// VECTOR(<dimension>, <coordinate type>) — but a plain list of floats does
		// NOT satisfy it; only a value built with vector(...) does. Emitting it
		// here would therefore declare a constraint the write path's own writes
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

// desiredSemanticKey builds the semantic identity of a desired Constraint. A
// constraint's members are a set, so they are sorted.
func desiredSemanticKey(con Constraint) string {
	return mapping.ConstraintKey(con.Label, constraintKindToRemoteType(con.Kind), con.Properties)
}

// constraintKindToRemoteType maps a ConstraintKind to the corresponding
// Neo4j remote constraint type string. //exhaustive:enforce makes a newly added
// kind a CI failure rather than a silent fall-through to
// [mapping.UnknownRemoteType], which matches no remote constraint and would
// make every constraint of that kind a perpetual Create paired with a
// perpetual Drop.
func constraintKindToRemoteType(kind ConstraintKind) string {
	//exhaustive:enforce
	switch kind {
	case ConstraintUnique:
		return mapping.Uniqueness
	case ConstraintNotNull:
		return mapping.NodePropertyExistence
	case ConstraintType:
		return mapping.NodePropertyType
	case ConstraintNodeKey:
		return mapping.NodeKey
	default:
		return mapping.UnknownRemoteType
	}
}

// declarableConstraintKinds returns the kinds this configuration emits, which
// is what a diff can own: the kinds whose remote type
// [mapping.DeclarableConstraintTypes] lists for the edition.
func (c Config) declarableConstraintKinds() []ConstraintKind {
	types := mapping.DeclarableConstraintTypes(c.community())
	var kinds []ConstraintKind
	for _, k := range allConstraintKinds {
		if slices.Contains(types, constraintKindToRemoteType(k)) {
			kinds = append(kinds, k)
		}
	}
	return kinds
}

// allConstraintKinds lists every [ConstraintKind], for the code that must
// enumerate the enum rather than switch on one value. A kind missing from this
// list is never emitted under any edition.
// [TestConstraintKind_EnumerationIsComplete] sweeps a numeric range wide enough
// to find kinds this list omits, wherever they sit.
var allConstraintKinds = []ConstraintKind{
	ConstraintUnique, ConstraintNotNull, ConstraintType, ConstraintNodeKey,
}
