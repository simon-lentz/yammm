package neo4j

import (
	"context"
	"fmt"
	"strings"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// IndexKind enumerates the categories of index the adapter emits from schema
// annotations. Full-text and point indexes are deferred; the enum is the
// extension point.
type IndexKind int

const (
	IndexRange  IndexKind = iota // Range (lookup) index over one or more scalar properties.
	IndexVector                  // Approximate-nearest-neighbour vector index.
)

// Index is a structured representation of a single Neo4j index derived from a
// schema's @index, @@index, and @vector annotations.
// Construct via [Adapter.IndexesStructured]; do not create directly.
type Index struct {
	Name             string    // Deterministic index name ({label}_{props}_idx or {label}_{prop}_vector_idx)
	Kind             IndexKind // Index category
	Label            string    // Fully qualified Neo4j label (e.g., "book_catalog__Publisher")
	Properties       []string  // Indexed properties in declared order (order is significant for composites)
	VectorDimensions int       // Vector dimension (0 for range indexes)
	VectorSimilarity string    // Vector similarity function, "cosine" or "euclidean" (empty for range indexes)
	Statement        string    // Complete CREATE [VECTOR] INDEX ... IF NOT EXISTS Cypher statement
}

// IndexesForSchema generates Neo4j index statements from a yammm schema's
// annotations.
//
// Returns the same Cypher statements as [Adapter.IndexesStructured], but as
// raw strings rather than structured [Index] values.
func (a *Adapter) IndexesForSchema(ctx context.Context, s *schema.Schema) ([]string, diag.Result) {
	structured, result := a.IndexesStructured(ctx, s)
	if !result.OK() {
		return nil, result
	}
	stmts := make([]string, len(structured))
	for i, idx := range structured {
		stmts[i] = idx.Statement
	}
	return stmts, result
}

// IndexesStructured generates Neo4j index statements from a yammm schema's
// @index, @@index, and @vector annotations and returns them as structured
// [Index] values.
//
// Property-level @index yields a single-property range index; type-level
// @@index yields a composite range index (declared order significant);
// property-level @vector yields an ANN vector index whose dimension comes from
// the property's Vector[N] constraint. Load-time validation already guarantees
// eligibility, so the adapter trusts the sealed model.
//
// Abstract types are skipped (they have no Neo4j label). Part types are NOT
// skipped: they receive a label and constraints today, so they receive index
// DDL too. Types with empty names are skipped. Inherited annotations emit for
// each concrete or part subtype's own label, matching how constraints treat
// inherited properties.
//
// Unlike constraints, indexes are emitted for every edition: range and vector
// indexes are core query features on both Community and Enterprise.
//
// Index names are always emitted; diff and DROP tooling need stable names and
// a new surface has no unnamed back-compat to preserve. Because statements
// carry IF NOT EXISTS, two indexes with the same emitted name would make the
// database silently skip the second, so a schema-wide name collision is
// reported as [E_NEO4J_INDEX_NAME_COLLISION] rather than emitted.
//
// Returns the index statements in deterministic order: types in schema
// declaration order; within each type range indexes then vector indexes (both
// in property order), then composites (in annotation order).
//
// If validation errors are found, returns (nil, result) where result contains
// all issues. Issues use [E_NEO4J_LABEL_COLLISION], [E_NEO4J_INVALID_IDENTIFIER],
// or [E_NEO4J_INDEX_NAME_COLLISION] codes.
func (a *Adapter) IndexesStructured(ctx context.Context, s *schema.Schema) ([]Index, diag.Result) {
	collector := diag.NewCollector(0)
	collector.Merge(a.DetectLabelCollisions(ctx, s))

	var indexes []Index

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

		indexes = append(indexes, indexesForType(t, label, collector)...)
	}

	detectIndexNameCollisions(indexes, collector)

	result := collector.Result()
	if !result.OK() {
		return nil, result
	}
	return indexes, result
}

// indexesForType generates all indexes for one emitted (non-abstract) type in
// deterministic order: range indexes, then vector indexes (both in property
// order), then composite @@index indexes (in annotation order).
func indexesForType(t *schema.Type, label string, collector *diag.Collector) []Index {
	var indexes []Index

	// 1. Range indexes from property-level @index.
	for _, prop := range t.AllPropertiesSlice() {
		if _, ok := prop.Annotation("index"); !ok {
			continue
		}
		if !validIndexProperty(t, prop.Name(), collector) {
			continue
		}
		indexes = append(indexes, rangeIndex(label, []string{prop.Name()}))
	}

	// 2. Vector indexes from property-level @vector.
	for _, prop := range t.AllPropertiesSlice() {
		ann, ok := prop.Annotation("vector")
		if !ok {
			continue
		}
		if !validIndexProperty(t, prop.Name(), collector) {
			continue
		}
		vc, ok := schema.ResolveAlias(prop.Constraint()).(schema.VectorConstraint)
		if !ok {
			continue // Load-time validation guarantees a Vector target; defensive.
		}
		indexes = append(indexes, vectorIndex(label, prop.Name(), vc.Dimension(), ann.Args()[0].Text()))
	}

	// 3. Composite range indexes from type-level @@index.
	for _, ann := range t.AllAnnotationsSlice() {
		if ann.Name() != "index" {
			continue
		}
		args := ann.Args()
		props := make([]string, 0, len(args))
		valid := true
		for _, arg := range args {
			if !validIndexProperty(t, arg.Text(), collector) {
				valid = false
				continue
			}
			props = append(props, arg.Text())
		}
		if !valid || len(props) == 0 {
			continue
		}
		indexes = append(indexes, rangeIndex(label, props))
	}

	return indexes
}

// validIndexProperty reports E_NEO4J_INVALID_IDENTIFIER and returns false when a
// property name is not a valid Neo4j identifier. A valid DSL property name can
// still be an invalid Neo4j identifier (e.g., a Cypher reserved keyword).
func validIndexProperty(t *schema.Type, propName string, collector *diag.Collector) bool {
	if err := ValidateIdentifier(propName, fmt.Sprintf("type %q property", t.Name())); err != nil {
		issue := diag.NewIssue(diag.Error, E_NEO4J_INVALID_IDENTIFIER,
			fmt.Sprintf("invalid property %q on type %q: %s", propName, t.Name(), err)).
			WithDetail(diag.DetailKeyFormat, "neo4j").
			WithDetail(diag.DetailKeyTypeName, t.Name()).
			WithDetail(diag.DetailKeyPropertyName, propName).
			WithDetail(diag.DetailKeyDetail, err.Error()).
			Build()
		collector.Collect(issue)
		return false
	}
	return true
}

// rangeIndex builds a range Index over the given properties (declared order).
func rangeIndex(label string, props []string) Index {
	name := indexName(label, props, IndexRange)
	refs := make([]string, len(props))
	for i, p := range props {
		refs[i] = "n." + p
	}
	stmt := fmt.Sprintf("CREATE INDEX %s IF NOT EXISTS FOR (n:%s) ON (%s)",
		name, label, strings.Join(refs, ", "))
	return Index{
		Name:       name,
		Kind:       IndexRange,
		Label:      label,
		Properties: props,
		Statement:  stmt,
	}
}

// vectorIndex builds a vector Index for one property with the given dimension
// and similarity function. The OPTIONS statement form requires Neo4j 5.15+.
func vectorIndex(label, prop string, dimension int, similarity string) Index {
	name := indexName(label, []string{prop}, IndexVector)
	stmt := fmt.Sprintf(
		"CREATE VECTOR INDEX %s IF NOT EXISTS FOR (n:%s) ON (n.%s) "+
			"OPTIONS {indexConfig: {`vector.dimensions`: %d, `vector.similarity_function`: '%s'}}",
		name, label, prop, dimension, similarity,
	)
	return Index{
		Name:             name,
		Kind:             IndexVector,
		Label:            label,
		Properties:       []string{prop},
		VectorDimensions: dimension,
		VectorSimilarity: similarity,
		Statement:        stmt,
	}
}

// indexName builds a deterministic index name:
//
//	range:  {label}_{prop1}_{prop2}_idx
//	vector: {label}_{prop}_vector_idx
func indexName(label string, props []string, kind IndexKind) string {
	joined := label + "_" + strings.Join(props, "_")
	if kind == IndexVector {
		return joined + "_vector_idx"
	}
	return joined + "_idx"
}

// detectIndexNameCollisions reports E_NEO4J_INDEX_NAME_COLLISION when two or
// more emitted indexes share a name. Underscore-joined names are ambiguous
// across property boundaries (@@index(a, b_c) and @@index(a_b, c) both yield
// {label}_a_b_c_idx), and because statements carry IF NOT EXISTS a collision
// makes the database silently skip the second index. The check is
// index-set-internal: it does not cross-check emitted constraint names, whose
// disjoint suffixes make an index-vs-constraint collision negligible.
func detectIndexNameCollisions(indexes []Index, collector *diag.Collector) {
	byName := make(map[string][]Index, len(indexes))
	for _, idx := range indexes {
		byName[idx.Name] = append(byName[idx.Name], idx)
	}

	reported := make(map[string]bool)
	for _, idx := range indexes {
		group := byName[idx.Name]
		if len(group) < 2 || reported[idx.Name] {
			continue
		}
		reported[idx.Name] = true

		descs := make([]string, len(group))
		for i, g := range group {
			descs[i] = "(" + strings.Join(g.Properties, ", ") + ")"
		}
		issue := diag.NewIssue(diag.Error, E_NEO4J_INDEX_NAME_COLLISION,
			fmt.Sprintf("emitted index name %q is produced by multiple indexes: %s",
				idx.Name, strings.Join(descs, ", "))).
			WithDetail(diag.DetailKeyFormat, "neo4j").
			WithDetail(detailKeyLabel, idx.Label).
			WithDetail(diag.DetailKeyDetail, idx.Name).
			Build()
		collector.Collect(issue)
	}
}
