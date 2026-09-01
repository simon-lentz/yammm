package neo4j

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"iter"
	"strings"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// IndexKind enumerates the categories of index the adapter emits from schema
// annotations. Point indexes are deferred; the enum is the extension point.
type IndexKind int

const (
	IndexRange    IndexKind = iota // Range (lookup) index over one or more scalar properties.
	IndexVector                    // Approximate-nearest-neighbour vector index.
	IndexFulltext                  // Fulltext index over one or more text properties.
)

// allIndexKinds lists every [IndexKind], for the code that must enumerate the
// enum rather than switch on one value — notably [RemoteIndex.Declarable],
// deciding which remote index types the diff owns. A kind missing from this
// list is invisible to the diff: its remote indexes are reported neither as
// matched nor as a drop, and the operator sees a clean diff while a whole class
// of declared index goes uncompared.
//
// Written out rather than derived from [indexKindToRemoteType]: any derivation
// that walks the enum has to assume where its values sit, and a kind placed
// below IndexRange or at a non-contiguous value escapes that assumption
// silently. [TestIndexKind_EnumerationIsComplete] sweeps a numeric range instead,
// so it can find a kind this list omits wherever the kind sits.
var allIndexKinds = []IndexKind{IndexRange, IndexVector, IndexFulltext}

// Index is a structured representation of a single Neo4j index derived from a
// schema's @index, @@index, @vector, @fulltext, and @@fulltext annotations.
// Construct via [Adapter.IndexesStructured]; do not create directly.
type Index struct {
	// Deterministic index name: {label}_{props}_idx for range,
	// {label}_{prop}_vector_idx for vector, {label}_{props}_fulltext_idx for
	// fulltext, plus a short hex digest suffix when two indexes would otherwise
	// share a name (see [disambiguateIndexNames]). Reconstructing the name from
	// label and properties is therefore not safe; read it from here.
	Name             string
	Kind             IndexKind // Index category
	Label            string    // Fully qualified Neo4j label (e.g., "book_catalog__Publisher")
	Properties       []string  // Indexed properties in declared order (order is significant for composites)
	VectorDimensions int       // Vector dimension (0 for range and fulltext indexes)
	VectorSimilarity string    // Vector similarity function, "cosine" or "euclidean" (empty for range and fulltext indexes)
	Statement        string    // Complete CREATE [VECTOR|FULLTEXT] INDEX ... IF NOT EXISTS Cypher statement
}

// IndexesForSchema generates Neo4j index statements from a yammm schema's
// annotations.
//
// Returns the same Cypher statements as [Adapter.IndexesStructured], but as
// raw strings rather than structured [Index] values — including the nil result
// for a schema that declares no index annotations, so a caller distinguishing
// nil from empty gets the same answer from both entry points.
func (a *Adapter) IndexesForSchema(ctx context.Context, s *schema.Schema) ([]string, diag.Result) {
	structured, result := a.IndexesStructured(ctx, s)
	if !result.OK() || len(structured) == 0 {
		return nil, result
	}
	stmts := make([]string, len(structured))
	for i, idx := range structured {
		stmts[i] = idx.Statement
	}
	return stmts, result
}

// String returns the index kind's Neo4j type name, so an [Index] renders
// legibly wherever it is printed — a diagnostic, a test failure, or a
// consumer's log — instead of showing the bare iota ordinal.
func (k IndexKind) String() string {
	if s := indexKindToRemoteType(k); s != unknownRemoteIndexType {
		return s
	}
	return fmt.Sprintf("IndexKind(%d)", int(k))
}

// IndexesStructured generates Neo4j index statements from a yammm schema's
// @index, @@index, @vector, @fulltext, and @@fulltext annotations and returns
// them as structured [Index] values.
//
// Property-level @index yields a single-property range index; type-level
// @@index yields a composite range index (declared order significant);
// property-level @vector yields an ANN vector index whose dimension comes from
// the property's Vector[N] constraint; property-level @fulltext and type-level
// @@fulltext yield fulltext indexes over text properties. Load-time validation
// guarantees target
// eligibility for every schema a public entry point returns; the adapter
// re-checks anyway — that each named property exists and that its type can
// carry the index it was annotated with — as defense against a model
// assembled outside those guarantees, rather than emitting DDL for a property
// that does not exist or cannot be indexed.
//
// Abstract types are skipped (they have no Neo4j label). Part types are NOT
// skipped: they receive a label and constraints today, so they receive index
// DDL too. Types with empty names are skipped. Inherited annotations emit for
// each concrete or part subtype's own label, matching how constraints treat
// inherited properties.
//
// Unlike constraints, indexes are emitted for every edition: range, vector,
// and fulltext indexes are core query features on both Community and
// Enterprise.
//
// Index names are always emitted; diff and DROP tooling need stable names and
// a new surface has no unnamed back-compat to preserve. Because statements
// carry IF NOT EXISTS, two indexes sharing an emitted name would make the
// database silently skip the second, so names that would collide are
// disambiguated with a short deterministic digest — see
// [disambiguateIndexNames], which also explains why the readable name cannot be
// made injective.
//
// Returns the index statements in deterministic order: types in schema
// declaration order; within each type range indexes, then vector indexes, then
// fulltext indexes (each in property order), then type-level composites —
// @@index and @@fulltext interleaved, in annotation order.
//
// If validation errors are found, returns (nil, result) where result contains
// all issues. Issues use [E_NEO4J_LABEL_COLLISION], [E_NEO4J_INVALID_IDENTIFIER],
// [E_NEO4J_UNKNOWN_PROPERTY], or [E_NEO4J_INVALID_INDEX_TARGET] codes.
func (a *Adapter) IndexesStructured(ctx context.Context, s *schema.Schema) ([]Index, diag.Result) {
	collector := diag.NewCollector(0)
	collector.Merge(a.DetectLabelCollisions(ctx, s))

	// Schema-scoped, so one bad annotation on an abstract mixin draws one
	// diagnostic rather than one per concrete subtype that emits for it.
	reported := make(reportedTargets)

	var indexes []Index
	for t, label := range a.emittableTypes(ctx, s, collector) {
		indexes = append(indexes, indexesForType(t, label, collector, reported)...)
	}

	disambiguateIndexNames(indexes)
	for i := range indexes {
		indexes[i].Statement = renderIndexStatement(indexes[i])
	}

	result := collector.Result()
	if !result.OK() {
		return nil, result
	}
	return indexes, result
}

// emittableTypes yields each (type, label) pair for the named, non-abstract
// types of s whose label is a valid Neo4j identifier, in schema declaration
// order.
//
// A type whose label fails identifier validation is skipped after collecting
// E_NEO4J_INVALID_IDENTIFIER. This is the shared gate of
// [Adapter.ConstraintsStructured], [Adapter.IndexesStructured], and
// [Adapter.ShapeForSchema].
//
// Callers that also run [Adapter.DetectLabelCollisions] walk the type list
// twice. That is deliberate: the collision check is public API with its own
// contract — it must see labels this gate skips (see [Adapter.labeledTypes]) —
// and a schema's type list is small enough that one extra pass costs less than
// the coupling a shared walk would create between a diagnostic and an emitter.
//
// The type name is trimmed, and the trimmed form is what the label is built
// from. [Adapter.ShapeForSchema]'s map is keyed by [schema.TypeID], so no
// lookup depends on the trim. Both schema front
// doors reject a name with surrounding whitespace anyway (the grammar's UC_WORD
// production; [schema.NewBuilder] reports E_INVALID_NAME), so the trim only
// matters to a construction path that bypasses both.
func (a *Adapter) emittableTypes(ctx context.Context, s *schema.Schema, collector *diag.Collector) iter.Seq2[*schema.Type, string] {
	return func(yield func(*schema.Type, string) bool) {
		for t, label := range a.labeledTypes(ctx, s) {
			name := strings.TrimSpace(t.Name())
			if err := ValidateIdentifier(label, fmt.Sprintf("type %q label", name)); err != nil {
				collector.Collect(invalidLabelIssue(name, label, err))
				continue
			}
			if !yield(t, label) {
				return
			}
		}
	}
}

// labeledTypes yields each (type, label) pair for the named, non-abstract types
// of s AND OF EVERY SCHEMA IN ITS IMPORT CLOSURE, in closure order then schema
// declaration order, WITHOUT validating the label.
//
// The closure is the scope because [Adapter.ShapeForSchema] gives every closure
// member's types a label and the write path then writes nodes under it. A walk
// over the entry schema's own types alone left an imported type with a label and
// node writes, and with no constraint, no index and no ownership.
//
// INVARIANT: the set this yields is both the EMIT set and the OWN set.
// [Adapter.ConstraintsForSchema], [Adapter.IndexesForSchema],
// [Adapter.DetectLabelCollisions] and [Adapter.OwnedLabels] all reach their
// types through this function, so what the adapter creates and what it claims
// cannot diverge. That is what keeps the diff safe: a label is owned exactly
// when it is desired, so widening the walk can never make [Adapter.DiffConstraints]
// plan a DROP for a label the emitter still writes. Narrowing one caller without
// the others would break that, which is why they share one walk.
//
// Split out from [Adapter.emittableTypes] because [Adapter.DetectLabelCollisions]
// needs the same walk but must not skip an invalid label: two types colliding on
// a label that is also an invalid identifier are still a collision, and the
// public collision check would otherwise go silent on exactly the schemas most
// likely to have one.
func (a *Adapter) labeledTypes(ctx context.Context, s *schema.Schema) iter.Seq2[*schema.Type, string] {
	return func(yield func(*schema.Type, string) bool) {
		for _, member := range s.Closure() {
			for _, t := range member.TypesSlice() {
				name := strings.TrimSpace(t.Name())
				if name == "" || t.IsAbstract() {
					continue
				}
				// The label composes from the DECLARING member's name, not the
				// entry schema's: an imported type keeps the label its own
				// schema gives it, which is the label its nodes are written
				// under.
				label := a.Label(ctx, member.Name(), name)
				if label == "" {
					continue
				}
				if !yield(t, label) {
					return
				}
			}
		}
	}
}

// identifierSubject names what an E_NEO4J_INVALID_IDENTIFIER is about.
//
// Each field means one thing, so a consumer reading a detail key does not have
// to know which site raised the issue. TypeName is a TYPE; Relation is the
// declaring relation of an edge property, which is where a relation-scoped
// property is attributed instead.
type identifierSubject struct {
	TypeName string
	Relation string
	Property string
	Label    string
}

// invalidIdentifierIssue builds the one E_NEO4J_INVALID_IDENTIFIER every site
// emits, and returns the builder so a caller can add its own span.
//
// One constructor per code, so a key means the same thing everywhere. Six sites
// built this by hand in three detail shapes, and one passed a
// [schema.DeclaringScope] rendering under the TYPE key — which is a RELATION
// name for an edge property, so a consumer reading "type" could get a relation.
func invalidIdentifierIssue(msg string, subj identifierSubject, err error) *diag.IssueBuilder {
	b := diag.NewIssue(diag.Error, E_NEO4J_INVALID_IDENTIFIER, msg).
		WithDetail(diag.DetailKeyFormat, "neo4j").
		WithDetail(diag.DetailKeyDetail, err.Error())
	if subj.TypeName != "" {
		b = b.WithDetail(diag.DetailKeyTypeName, subj.TypeName)
	}
	if subj.Relation != "" {
		b = b.WithDetail(diag.DetailKeyRelationName, subj.Relation)
	}
	if subj.Property != "" {
		b = b.WithDetail(diag.DetailKeyPropertyName, subj.Property)
	}
	if subj.Label != "" {
		b = b.WithDetail(detailKeyLabel, subj.Label)
	}
	return b
}

// invalidLabelIssue builds the E_NEO4J_INVALID_IDENTIFIER diagnostic for a type
// whose label is not a usable Neo4j identifier.
func invalidLabelIssue(typeName, label string, err error) diag.Issue {
	return invalidIdentifierIssue(
		fmt.Sprintf("invalid label for type %q: %s", typeName, err),
		identifierSubject{TypeName: typeName, Label: label},
		err,
	).Build()
}

// indexesForType generates all indexes for one emitted (non-abstract) type in
// deterministic order: range indexes, then vector indexes, then fulltext
// indexes (each in property order), then type-level composites — @@index and
// @@fulltext interleaved, in annotation order.
//
// Two declarations that describe the SAME index — @index on a property plus a
// single-property @@index over it, or @fulltext plus a single-property
// @@fulltext, which load-time validation accepts because neither placement's
// check inspects the other — collapse to one. Emitting both would put two
// identical definitions in the output, which [disambiguateIndexNames] would
// then give two different names, so the schema would ask the database for the
// same index twice. The dedup key carries the kind, so @index and @fulltext on
// one property stay two distinct indexes.
func indexesForType(t *schema.Type, label string, collector *diag.Collector, reported reportedTargets) []Index {
	var indexes []Index
	emitted := make(map[string]bool)
	add := func(idx Index) {
		key := desiredIndexKey(idx)
		if emitted[key] {
			return
		}
		emitted[key] = true
		indexes = append(indexes, idx)
	}

	// 1-3. Property-level @index, @vector, and @fulltext, collected in one walk
	// of the merged property set. Ranges add inline; vector and fulltext defer
	// to their own slices so the documented range-vector-fulltext order holds.
	var vectorIndexes, fulltextIndexes []Index
	for prop := range t.AllProperties() {
		if ann, ok := prop.Annotation("index"); ok {
			if validScalarTarget(t, prop, ann, collector, reported) {
				add(rangeIndex(label, []string{prop.Name()}))
			}
		}
		if ann, ok := prop.Annotation("fulltext"); ok {
			if validFulltextTarget(t, prop, ann, collector, reported) {
				fulltextIndexes = append(fulltextIndexes,
					fulltextIndex(label, []string{prop.Name()}))
			}
		}
		ann, ok := prop.Annotation("vector")
		if !ok {
			continue
		}
		// The type assertion drives both the eligibility check and the emission,
		// so the resolved constraint is produced once and there is no branch for
		// a failure the check has already ruled out.
		vc, isVector := schema.ResolveAlias(prop.Constraint()).(schema.VectorConstraint)
		if !validVectorTarget(t, prop, ann, vc, isVector, collector, reported) {
			continue
		}
		vectorIndexes = append(vectorIndexes,
			vectorIndex(label, prop.Name(), vc.Dimension(), ann.Args()[0].Text()))
	}
	for _, idx := range vectorIndexes {
		add(idx)
	}
	for _, idx := range fulltextIndexes {
		add(idx)
	}

	// 4. Type-level composites from @@index and @@fulltext — ONE walk branching
	// on the name, so the two kinds emit interleaved in annotation order rather
	// than grouped by kind.
	for ann := range t.AllAnnotations() {
		isFulltext := ann.Name() == "fulltext"
		if ann.Name() != "index" && !isFulltext {
			continue
		}
		args := ann.Args()
		props := make([]string, 0, len(args))
		valid := true
		for _, arg := range args {
			prop, ok := t.Property(arg.Text())
			if !ok {
				reportIndexTarget(collector, reported,
					indexTargetRef{ann: ann, name: arg.Text(), code: E_NEO4J_UNKNOWN_PROPERTY},
					ann, E_NEO4J_UNKNOWN_PROPERTY, targetScope(t, nil),
					fmt.Sprintf("index annotation names unknown property %q", arg.Text()),
					"property not declared on the annotated type or any resolved ancestor")
				valid = false
				continue
			}
			var memberValid bool
			if isFulltext {
				memberValid = validFulltextTarget(t, prop, ann, collector, reported)
			} else {
				memberValid = validScalarTarget(t, prop, ann, collector, reported)
			}
			if !memberValid {
				valid = false
				continue
			}
			props = append(props, arg.Text())
		}
		if !valid || len(props) == 0 {
			continue
		}
		if isFulltext {
			add(fulltextIndex(label, props))
		} else {
			add(rangeIndex(label, props))
		}
	}

	return indexes
}

// reportedTargets dedups an adapter-level index diagnostic to one per thing the
// user has to edit, across the whole schema rather than per type.
//
// What that thing is depends on the diagnostic, and the two subjects are not
// interchangeable — either choice applied to the other diagnostic produces noise
// in one direction or silence in the other:
//
//   - An illegal Neo4j identifier is the PROPERTY's problem — one rename fixes
//     every annotation that names it — so the subject is the declared property.
//     Keyed by annotation, the same reserved keyword draws one diagnostic for its
//     @index and another for the @@index that also lists it.
//   - An unknown or ineligible target is the ANNOTATION's problem — the
//     property may be fine, or may not exist at all — so the subject is the
//     annotation. Keyed by type, a mixin with ten heirs draws ten identical
//     diagnostics, burying every other independent error.
//
// Both subjects are stable across the merged view: linearization shares the
// declaring ancestor's *Annotation with every heir rather than cloning it, and
// [schema.Property.Origin] maps a synthesized copy back to the property that
// was actually declared. Two unrelated types with the same mistake therefore
// still draw two diagnostics, which is right — each needs its own edit.
type reportedTargets map[indexTargetRef]bool

// indexTargetRef identifies the subject of a diagnostic. Exactly one of ann and
// prop is set; name carries the property name for both, so a multi-argument
// @@index naming two bad properties reports both.
type indexTargetRef struct {
	ann  *schema.Annotation
	prop *schema.Property
	name string
	// code discriminates two different problems that can share a subject. One
	// @@index on a mixin can be an unknown property on one heir and an
	// ineligible one on another; without the code the first diagnostic
	// suppresses the second and the user learns of them one load apart.
	code diag.Code
}

// reportIndexTarget collects one diagnostic per subject, anchored on the
// annotation's own span so the message points at the declaration.
//
// The diagnostic deliberately does NOT name a concrete type. An inherited
// annotation or property is declared once and emitted by every heir, so naming
// whichever heir the walk reached first blames a type that may not declare the
// thing at all, and contradicts the span. The declaring scope is named instead,
// and it is the same for every heir.
func reportIndexTarget(
	collector *diag.Collector, reported reportedTargets,
	ref indexTargetRef, ann *schema.Annotation,
	code diag.Code, subj identifierSubject, msg, detail string,
) {
	if reported[ref] {
		return
	}
	reported[ref] = true
	subj.Property = ref.name
	if code == E_NEO4J_INVALID_IDENTIFIER {
		collector.Collect(invalidIdentifierIssue(msg, subj, errors.New(detail)).
			WithSpan(ann.Span()).Build())
		return
	}
	b := diag.NewIssue(diag.Error, code, msg).
		WithSpan(ann.Span()).
		WithDetail(diag.DetailKeyFormat, "neo4j").
		WithDetail(diag.DetailKeyPropertyName, ref.name).
		WithDetail(diag.DetailKeyDetail, detail)
	if subj.TypeName != "" {
		b = b.WithDetail(diag.DetailKeyTypeName, subj.TypeName)
	}
	if subj.Relation != "" {
		b = b.WithDetail(diag.DetailKeyRelationName, subj.Relation)
	}
	collector.Collect(b.Build())
}

// targetScope names what a diagnostic should be attributed to, keeping a type
// and a relation apart.
//
// The declaring scope of the property, when there is one: an inherited property
// or annotation is reported once for the whole schema, so naming whichever heir
// the walk reached first attributes it to a type that may declare nothing
// involved, and reordering the source would change the name. Where the property
// does not exist at all — an @@index argument naming nothing — there is no
// declaring scope, and the annotated type is the closest true answer.
//
// A relation-scoped property is declared on an EDGE, so its scope is a relation
// name and it is reported as one. Rendering it into the type detail told a
// consumer a relation was a type.
func targetScope(t *schema.Type, prop *schema.Property) identifierSubject {
	if prop == nil {
		return identifierSubject{TypeName: t.Name()}
	}
	scope := prop.Origin().DeclaringScope()
	if scope.Kind() == schema.ScopeRelation {
		return identifierSubject{Relation: scope.RelationName()}
	}
	return identifierSubject{TypeName: scope.TypeRef().String()}
}

// String renders the subject for a diagnostic MESSAGE, which names the
// declaring scope whether that is a type or a relation.
func (s identifierSubject) String() string {
	if s.Relation != "" {
		return s.Relation
	}
	return s.TypeName
}

// validIndexName reports whether a property's own name can appear in emitted
// DDL. The subject is the property: one rename fixes every annotation naming
// it, so reporting per annotation would repeat the same message for a reserved
// keyword listed under both @index and @@index.
func validIndexName(t *schema.Type, prop *schema.Property, ann *schema.Annotation,
	collector *diag.Collector, reported reportedTargets,
) bool {
	name := prop.Name()
	scope := targetScope(t, prop)
	// The context names the DECLARING scope, matching the message and the detail:
	// this diagnostic is deduped once per declared property for the whole schema,
	// so embedding the emitting heir would make the error text disagree with its
	// own span and change with source order.
	err := ValidateIdentifier(name, scope.String()+" property")
	if err == nil {
		return true
	}
	reportIndexTarget(collector, reported,
		indexTargetRef{prop: prop.Origin(), name: name, code: E_NEO4J_INVALID_IDENTIFIER},
		ann, E_NEO4J_INVALID_IDENTIFIER, scope,
		fmt.Sprintf("invalid property %q declared in %s: %s", name, scope, err),
		err.Error())
	return false
}

// validScalarTarget reports whether a property named by @index or @@index can be
// emitted: a usable Neo4j identifier, and a type the loader's own rule accepts.
//
// The type check is defense in depth. Every public entry point rejects the
// dangling references that once sealed cleanly, but a model assembled outside
// those guarantees could still carry @index on a List or on an unresolved
// alias — trusting it would emit a range index over a property the DSL's own
// rule forbids.
func validScalarTarget(t *schema.Type, prop *schema.Property, ann *schema.Annotation,
	collector *diag.Collector, reported reportedTargets,
) bool {
	if !validIndexName(t, prop, ann, collector, reported) {
		return false
	}
	if indexableScalar(prop.Constraint()) {
		return true
	}
	reportIndexTarget(collector, reported,
		indexTargetRef{ann: ann, name: prop.Name(), code: E_NEO4J_INVALID_INDEX_TARGET},
		ann, E_NEO4J_INVALID_INDEX_TARGET, targetScope(t, prop),
		fmt.Sprintf("index annotation names property %q, which is not an indexable scalar", prop.Name()),
		"a range index requires a scalar property")
	return false
}

// validVectorTarget reports whether a property named by @vector can be emitted.
// The caller has already resolved the constraint and passes the result, so the
// assertion happens once and drives both the check and the emission.
//
// The type check is defense in depth on the same terms as [validScalarTarget],
// with a sharper consequence: emitting nothing for a @vector whose target is
// not a Vector drops the declared ANN index entirely — no DDL and no
// diagnostic, so every vector query falls back to a brute-force scan and the
// diff reports no problem because the index is missing from the desired set
// too. A non-positive dimension is rejected for a related reason: Neo4j
// refuses the statement at execution, aborting a DDL apply midway through.
func validVectorTarget(t *schema.Type, prop *schema.Property, ann *schema.Annotation,
	vc schema.VectorConstraint, isVector bool,
	collector *diag.Collector, reported reportedTargets,
) bool {
	if !validIndexName(t, prop, ann, collector, reported) {
		return false
	}
	report := func(msg, detail string) bool {
		reportIndexTarget(collector, reported,
			indexTargetRef{ann: ann, name: prop.Name(), code: E_NEO4J_INVALID_INDEX_TARGET},
			ann, E_NEO4J_INVALID_INDEX_TARGET, targetScope(t, prop), msg, detail)
		return false
	}
	if !isVector {
		return report(
			fmt.Sprintf("@vector names property %q, which is not a Vector", prop.Name()),
			"a vector index requires a Vector property",
		)
	}
	if vc.Dimension() <= 0 {
		return report(
			fmt.Sprintf("@vector names property %q with dimension %d", prop.Name(), vc.Dimension()),
			"a vector index requires a positive dimension; Neo4j rejects the CREATE VECTOR INDEX statement otherwise",
		)
	}
	return true
}

// validFulltextTarget reports whether a property named by @fulltext or
// @@fulltext can be emitted: a usable Neo4j identifier, and a text type the
// loader's own rule accepts.
//
// The type check is defense in depth on the same terms as [validScalarTarget]:
// every public entry point rejects the references that once sealed cleanly, but
// a model assembled outside those guarantees could still carry @fulltext on a
// non-text property, and trusting it would emit a fulltext index over a
// property the DSL's own rule forbids.
func validFulltextTarget(t *schema.Type, prop *schema.Property, ann *schema.Annotation,
	collector *diag.Collector, reported reportedTargets,
) bool {
	if !validIndexName(t, prop, ann, collector, reported) {
		return false
	}
	if fulltextEligibleScalar(prop.Constraint()) {
		return true
	}
	reportIndexTarget(collector, reported,
		indexTargetRef{ann: ann, name: prop.Name(), code: E_NEO4J_INVALID_INDEX_TARGET},
		ann, E_NEO4J_INVALID_INDEX_TARGET, targetScope(t, prop),
		fmt.Sprintf("fulltext annotation names property %q, which is not a text property", prop.Name()),
		"a fulltext index requires a String, Pattern, or Enum property")
	return false
}

// fulltextEligibleScalar mirrors the loader's @fulltext / @@fulltext
// eligibility rule (String, Pattern, Enum — the text kinds) so the adapter
// re-checks exactly what the loader would have checked had the target's type
// resolved. An unresolved alias resolves to KindAlias and is correctly
// ineligible: nothing is known about it, so nothing licenses emitting DDL.
func fulltextEligibleScalar(c schema.Constraint) bool {
	if c == nil {
		return false
	}
	//exhaustive:enforce
	switch schema.ResolveAlias(c).Kind() {
	case schema.KindString, schema.KindPattern, schema.KindEnum:
		return true
	case schema.KindUUID, schema.KindInteger, schema.KindFloat, schema.KindBoolean,
		schema.KindDate, schema.KindTimestamp, schema.KindVector, schema.KindList,
		schema.KindAlias:
		return false
	default:
		return false
	}
}

// indexableScalar mirrors the loader's @index / @@index eligibility rule so the
// adapter re-checks exactly what the loader would have checked had the target's
// type resolved. An unresolved alias resolves to KindAlias and is correctly
// ineligible: nothing is known about it, so nothing licenses emitting DDL.
func indexableScalar(c schema.Constraint) bool {
	if c == nil {
		return false
	}
	//exhaustive:enforce
	switch schema.ResolveAlias(c).Kind() {
	case schema.KindString, schema.KindUUID, schema.KindEnum, schema.KindPattern,
		schema.KindInteger, schema.KindFloat, schema.KindBoolean,
		schema.KindDate, schema.KindTimestamp:
		return true
	case schema.KindVector, schema.KindList, schema.KindAlias:
		return false
	default:
		return false
	}
}

// rangeIndex builds a range Index over the given properties (declared order).
// Statement is filled in by [renderIndexStatement] once naming has settled; see
// [disambiguateIndexNames].
func rangeIndex(label string, props []string) Index {
	return Index{
		Name:       indexName(label, props, IndexRange),
		Kind:       IndexRange,
		Label:      label,
		Properties: props,
	}
}

// fulltextIndex builds a fulltext Index over the given properties (declared
// order). Statement is filled in by [renderIndexStatement] once naming has
// settled; see [disambiguateIndexNames].
func fulltextIndex(label string, props []string) Index {
	return Index{
		Name:       indexName(label, props, IndexFulltext),
		Kind:       IndexFulltext,
		Label:      label,
		Properties: props,
	}
}

// vectorIndex builds a vector Index for one property with the given dimension
// and similarity function. The OPTIONS statement form requires Neo4j 5.15+.
func vectorIndex(label, prop string, dimension int, similarity string) Index {
	return Index{
		Name:             indexName(label, []string{prop}, IndexVector),
		Kind:             IndexVector,
		Label:            label,
		Properties:       []string{prop},
		VectorDimensions: dimension,
		VectorSimilarity: similarity,
	}
}

// renderIndexStatement builds the CREATE statement for an index. It runs after
// [disambiguateIndexNames] so the statement always carries the index's final
// name — rewriting an already-rendered statement would mean substring-patching
// generated Cypher, where the label is a prefix of the name.
func renderIndexStatement(idx Index) string {
	if idx.Kind == IndexVector {
		return fmt.Sprintf(
			"CREATE VECTOR INDEX %s IF NOT EXISTS FOR (n:%s) ON (n.%s) "+
				"OPTIONS {indexConfig: {`vector.dimensions`: %d, `vector.similarity_function`: '%s'}}",
			idx.Name, idx.Label, idx.Properties[0], idx.VectorDimensions, idx.VectorSimilarity,
		)
	}
	refs := make([]string, len(idx.Properties))
	for i, p := range idx.Properties {
		refs[i] = "n." + p
	}
	// Fulltext uses the ON EACH list form; range uses the ON tuple form.
	if idx.Kind == IndexFulltext {
		return fmt.Sprintf("CREATE FULLTEXT INDEX %s IF NOT EXISTS FOR (n:%s) ON EACH [%s]",
			idx.Name, idx.Label, strings.Join(refs, ", "))
	}
	return fmt.Sprintf("CREATE INDEX %s IF NOT EXISTS FOR (n:%s) ON (%s)",
		idx.Name, idx.Label, strings.Join(refs, ", "))
}

// indexName builds a deterministic index name:
//
//	range:    {label}_{prop1}_{prop2}_idx
//	vector:   {label}_{prop}_vector_idx
//	fulltext: {label}_{prop1}_{prop2}_fulltext_idx
func indexName(label string, props []string, kind IndexKind) string {
	joined := label + "_" + strings.Join(props, "_")
	//exhaustive:enforce
	switch kind {
	case IndexRange:
		return joined + "_idx"
	case IndexVector:
		return joined + "_vector_idx"
	case IndexFulltext:
		return joined + "_fulltext_idx"
	default:
		return joined + "_idx"
	}
}

// disambiguateIndexNames gives every emitted index a unique name, appending a
// short digest of its semantic identity to each member of any group that would
// otherwise share one.
//
// The readable name is not injective and cannot be made so: it joins the label
// and the property names with underscores, and a property name may itself
// contain underscores, so @@index(a, b_c) and @@index(a_b, c) both render
// {label}_a_b_c_idx — as do an `a_b` property on type `Item` and a `b` property
// on type `Item_a`. Ordinary snake_case property names make this reachable
// rather than exotic. Neo4j identifiers admit only letters, digits, and
// underscores, so no separator exists that a property name cannot contain.
//
// Because every emitted statement carries IF NOT EXISTS, two indexes sharing a
// name make the database silently skip the second. Reporting that as an error
// would be worse than useless: index emission is all-or-nothing, so one unlucky
// property-name pair would suppress every unrelated index in the schema and
// leave the user renaming a domain property to get any DDL at all. The digest is
// taken over the injective identity key, so distinct indexes always get distinct
// names, and it is deterministic, so a name is stable across runs.
//
// Only colliding names are suffixed, so the overwhelmingly common index keeps
// its readable name. A name that changes because a newly added index collided
// with it is not a problem for the diff: pairing falls back to semantic identity
// when a name does not match, so the existing remote index is still recognised.
func disambiguateIndexNames(indexes []Index) {
	counts := make(map[string]int, len(indexes))
	for _, idx := range indexes {
		counts[idx.Name]++
	}
	// Names that do not collide keep theirs, and are reserved so a suffixed name
	// cannot land on one.
	taken := make(map[string]bool, len(indexes))
	for _, idx := range indexes {
		if counts[idx.Name] < 2 {
			taken[idx.Name] = true
		}
	}
	for i, idx := range indexes {
		if counts[idx.Name] < 2 {
			continue
		}
		indexes[i].Name = uniqueDigestName(idx.Name, desiredIndexKey(idx), taken)
		taken[indexes[i].Name] = true
	}
}

// uniqueDigestName appends the shortest prefix of a digest over identity that is
// not already taken.
//
// A fixed-width digest would be an unchecked assumption: 24 bits is ample for
// the handful of names in one collision group, but "ample" is not "guaranteed",
// and a collision here silently costs the schema an index it declared — the very
// outcome disambiguation exists to prevent. Lengthening on demand makes the
// guarantee actual rather than probable, is deterministic (the digest is over
// the identity, and the scan order is the emission order), and leaves the common
// case at six hex characters.
func uniqueDigestName(base, identity string, taken map[string]bool) string {
	sum := sha256.Sum256([]byte(identity))
	for n := 3; n <= len(sum); n++ {
		candidate := base + "_" + hex.EncodeToString(sum[:n])
		if !taken[candidate] {
			return candidate
		}
	}
	// Unreachable for any realistic input: two distinct identities would have to
	// share all 256 bits. Returning the full digest keeps the function total.
	return base + "_" + hex.EncodeToString(sum[:])
}
