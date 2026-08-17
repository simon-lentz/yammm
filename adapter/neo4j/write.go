package neo4j

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"time"

	"github.com/neo4j/neo4j-go-driver/v6/neo4j/dbtype"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
)

// BatchNodeQuery represents an UNWIND-based batch node upsert.
//
// Construct via [Adapter.BatchNodeQueries]; do not create directly.
type BatchNodeQuery struct {
	Statement string         // Cypher UNWIND $rows AS row MERGE ... SET statement
	Params    map[string]any // Contains "rows" key with []map[string]any value
}

// BatchEdgeQuery represents an UNWIND-based batch relationship merge.
//
// Construct via [Adapter.BatchEdgeQueries]; do not create directly.
type BatchEdgeQuery struct {
	Statement    string         // Cypher UNWIND $rows AS row MATCH ... MERGE ... SET statement
	Params       map[string]any // Contains "rows" key with []map[string]any value
	RelationType string         // Neo4j relationship type (e.g., "IN_REGION")
}

// WriteOption configures write query generation.
type WriteOption func(*writeConfig)

type writeConfig struct {
	nodeChunkSize int
	edgeChunkSize int
}

const (
	defaultNodeChunkSize = 5000
	defaultEdgeChunkSize = 5000
)

func defaultWriteConfig() writeConfig {
	return writeConfig{
		nodeChunkSize: defaultNodeChunkSize,
		edgeChunkSize: defaultEdgeChunkSize,
	}
}

// WithNodeChunkSize sets the maximum number of nodes per UNWIND batch.
// Default: 5000.
func WithNodeChunkSize(size int) WriteOption {
	return func(c *writeConfig) {
		c.nodeChunkSize = size
	}
}

// WithEdgeChunkSize sets the maximum number of edges per UNWIND batch.
// Default: 5000.
func WithEdgeChunkSize(size int) WriteOption {
	return func(c *writeConfig) {
		c.edgeChunkSize = size
	}
}

// BatchNodeQueries generates UNWIND-batched MERGE queries for all instances
// of each type in a graph result.
//
// The immutable-key shape is selected per type: a type with derived @writeOnce
// keys gets the ON CREATE / ON MATCH split, while an unannotated type in the
// same snapshot stays mutable.
//
// Returns one [BatchNodeQuery] per type per chunk. Types with more instances
// than the chunk size produce multiple queries. When two types in the
// snapshot render the same type name, returns an error naming both
// identities — they would share one node shape and one label.
func (a *Adapter) BatchNodeQueries(
	ctx context.Context,
	result *graph.Snapshot,
	shapes *GraphShape,
	opts ...WriteOption,
) ([]*BatchNodeQuery, error) {
	cfg := defaultWriteConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	if err := renderedNameCollision(result); err != nil {
		return nil, err
	}
	var queries []*BatchNodeQuery

	for _, typeID := range result.Types() {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("batch node queries: %w", err)
		}
		typeName := schema.TagForm(result.Schema(), typeID)
		nodeShape, ok := shapes.Types[typeName]
		if !ok {
			return nil, fmt.Errorf("no shape for type %q", typeName)
		}

		schemaType, ok := result.Schema().TypeByID(typeID)
		if !ok {
			return nil, fmt.Errorf("type %q not found in schema", typeName)
		}

		// The effective immutable set is the type's derived @writeOnce keys.
		immutableKeys := derivedImmutableKeys(&nodeShape, schemaType)
		km := MutableKeys
		if len(immutableKeys) > 0 {
			km = ImmutableKeys
		}

		instances := result.InstancesOf(typeID)
		var rows []map[string]any

		for _, inst := range instances {
			keyProps, err := extractKeyProps(inst.Properties(), &nodeShape)
			if err != nil {
				return nil, fmt.Errorf("type %q: %w", typeName, err)
			}

			props, err := propsToParamMap(inst.Properties(), schemaType)
			if err != nil {
				return nil, fmt.Errorf("type %q: %w", typeName, err)
			}
			// Key entries are prefixed to keep them in a namespace disjoint from
			// `props` and `update_props`; see [buildBatchNodeMergeQuery], whose
			// template reads them back as row.key_<name>.
			row := make(map[string]any, len(keyProps)+2)
			for k, v := range keyProps {
				row[batchKeyParamPrefix+k] = v
			}
			row["props"] = props
			if len(immutableKeys) > 0 {
				row["update_props"] = removeKeys(props, immutableKeys)
			}
			rows = append(rows, row)
		}

		stmt := buildBatchNodeMergeQuery(nodeShape.Label, nodeShape.PrimaryKeys, km)
		for _, chunk := range chunkSlice(rows, cfg.nodeChunkSize) {
			queries = append(queries, &BatchNodeQuery{
				Statement: stmt,
				Params:    map[string]any{"rows": chunk},
			})
		}
	}

	return queries, nil
}

// BatchEdgeQueries generates UNWIND-batched MERGE queries for edges,
// grouped by (sourceType, relationType, targetType) signature.
//
// Returns one [BatchEdgeQuery] per signature per chunk. When two types in
// the snapshot render the same type name, returns an error naming both
// identities — the signature and shape lookups key on that name.
func (a *Adapter) BatchEdgeQueries(
	ctx context.Context,
	result *graph.Snapshot,
	shapes *GraphShape,
	opts ...WriteOption,
) ([]*BatchEdgeQuery, error) {
	cfg := defaultWriteConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	if err := renderedNameCollision(result); err != nil {
		return nil, err
	}

	edges := result.Edges()

	// Validate all relationship types upfront (fail fast).
	for _, edge := range edges {
		if err := ValidateIdentifier(edge.Relation(), "relationship type"); err != nil {
			return nil, err
		}
	}

	groups := groupEdgesBySignature(edges)

	// Sort signatures for deterministic output.
	sigs := make([]edgeSignature, 0, len(groups))
	for sig := range groups {
		sigs = append(sigs, sig)
	}
	slices.SortFunc(sigs, func(a, b edgeSignature) int {
		if v := cmp.Compare(a.sourceType, b.sourceType); v != 0 {
			return v
		}
		if v := cmp.Compare(a.relType, b.relType); v != 0 {
			return v
		}
		return cmp.Compare(a.targetType, b.targetType)
	})

	var queries []*BatchEdgeQuery

	for _, sig := range sigs {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("batch edge queries: %w", err)
		}
		sigEdges := groups[sig]

		srcShape, ok := shapes.Types[sig.sourceType]
		if !ok {
			return nil, fmt.Errorf("no shape for source type %q", sig.sourceType)
		}
		tgtShape, ok := shapes.Types[sig.targetType]
		if !ok {
			return nil, fmt.Errorf("no shape for target type %q", sig.targetType)
		}

		// Resolve the schema relation for this signature group so typed edge
		// properties (Timestamp/Date/Float) coerce to driver-native types.
		// Nil when the source type or relation is not found — coerceRelProps
		// then passes properties through unchanged.
		var rel *schema.Relation
		if srcType, ok := result.Schema().Type(sig.sourceType); ok {
			rel, _ = srcType.Relation(sig.relType)
		}

		// Detect if any edge in this group has properties.
		hasProps := false
		for _, edge := range sigEdges {
			if edge.HasProperties() {
				hasProps = true
				break
			}
		}

		var rows []map[string]any
		for _, edge := range sigEdges {
			srcKeys, err := extractKeyProps(edge.Source().Properties(), &srcShape)
			if err != nil {
				return nil, fmt.Errorf("source %q: %w", sig.sourceType, err)
			}
			tgtKeys, err := extractKeyProps(edge.Target().Properties(), &tgtShape)
			if err != nil {
				return nil, fmt.Errorf("target %q: %w", sig.targetType, err)
			}

			row := make(map[string]any)
			for k, v := range srcKeys {
				row[relFromRowPrefix+k] = v
			}
			for k, v := range tgtKeys {
				row[relToRowPrefix+k] = v
			}
			if hasProps {
				if edge.HasProperties() {
					relProps, err := coerceRelProps(edge.Properties().Clone(), rel)
					if err != nil {
						return nil, fmt.Errorf("relation %q: %w", sig.relType, err)
					}
					row["rel_props"] = relProps
				} else {
					row["rel_props"] = map[string]any{}
				}
			}
			rows = append(rows, row)
		}

		stmt := buildBatchRelationshipMergeQuery(
			srcShape.Label, srcShape.PrimaryKeys,
			sig.relType,
			tgtShape.Label, tgtShape.PrimaryKeys,
			hasProps,
		)
		for _, chunk := range chunkSlice(rows, cfg.edgeChunkSize) {
			queries = append(queries, &BatchEdgeQuery{
				Statement:    stmt,
				Params:       map[string]any{"rows": chunk},
				RelationType: sig.relType,
			})
		}
	}

	return queries, nil
}

// renderedNameCollision reports an error when two type identities in the
// snapshot render one type name. The rendering is lossy where the snapshot
// is not, so the writer refuses rather than silently merging the pair.
func renderedNameCollision(snap *graph.Snapshot) error {
	s := snap.Schema()
	seen := make(map[string]schema.TypeID)
	for _, id := range snap.Types() {
		name := schema.TagForm(s, id)
		if first, ok := seen[name]; ok {
			return fmt.Errorf("neo4j adapter: type %s and type %s both render type name %q, so they would share one node shape and label",
				first, id, name)
		}
		seen[name] = id
	}
	return nil
}

// propsToParamMap converts instance properties to a Neo4j-driver-compatible
// map, routing every scalar through [Coerce] and every []any slice through
// [coerceSlice] against the property's schema constraint. This repairs the
// JSON round-trip — a whole-number Float decoded as int64, and Date/Timestamp
// values carried as strings — so the driver receives native types that satisfy
// Neo4j TYPE constraints (IS :: FLOAT, IS :: DATE, IS :: ZONED DATETIME).
// Returns an error, naming the offending property, if a value cannot be coerced
// to its declared kind (e.g. an unparseable Timestamp/Date string). Properties
// are processed in sorted key order, so when several fail the error names the
// lexicographically-first failing property on every run — matching [CoerceParams],
// the failure is reproducible rather than map-iteration-order dependent. The
// output map is independent of key order (maps are unordered); only error
// selection is affected.
func propsToParamMap(props immutable.Properties, schemaType *schema.Type) (map[string]any, error) {
	raw := props.Clone()
	if schemaType == nil {
		return raw, nil
	}

	for _, key := range slices.Sorted(maps.Keys(raw)) {
		val := raw[key]
		if val == nil {
			continue
		}
		prop, found := schemaType.Property(key)
		if !found {
			continue
		}
		cv, err := coerceValue(prop.Constraint(), val)
		if err != nil {
			return nil, fmt.Errorf("property %q: %w", key, err)
		}
		raw[key] = cv
	}
	return raw, nil
}

// coerceSlice converts a []any to a concrete typed slice using the constraint's
// element kind, so a List<T> or Vector property reaches the driver as the
// homogeneous Go slice Neo4j requires ([]string, []float64, []dbtype.Date, ...).
// A Vector is float-valued by definition (matching the eval package's checkVector
// / coerceVector), so it coerces elementwise exactly as a List<Float> would; this
// is what repairs a vector loaded from a pre-v0.12 snapshot, whose whole floats
// were written int-shaped and arrive narrowed to int64. Per-element conversion delegates
// to [Coerce] (the Float width-repair and Date/Timestamp parse rules) or, for
// Integer elements, to [repairInt64] (every Go int/uint width -> int64, mirroring
// Coerce's Float repair), so the repair rules live in one place; coerceSlice owns
// only the slice typing.
//
// An element that is neither the element type nor coercible to it — a non-numeric
// in a List<Float>, an unparseable or wrong-typed value in a List<Date> — is an
// error naming the element index: a heterogeneous []any cannot satisfy a List<T>
// column, so failing here beats shipping a slice the driver rejects. On the
// validated node path this never fires (instance validation already enforced each
// element's type); it guards the direct-Cypher path, where the param map is
// hand-built. A []any value under a scalar (non-List, non-Vector) constraint is a
// shape mismatch — a scalar property cannot hold a list — and is an error too. A
// nested-collection element kind (a List or Vector element, e.g. List<Vector>) has
// no concrete driver slice type at this level and returns the []any unchanged. The
// element switch is exhaustiveness-guarded, so a newly-added ConstraintKind fails
// the build here rather than silently passing a []any to the driver.
func coerceSlice(raw []any, c schema.Constraint) (any, error) {
	c = schema.ResolveAlias(c)
	var elem schema.Constraint
	switch cc := c.(type) {
	case schema.ListConstraint:
		elem = schema.ResolveAlias(cc.Element())
	case schema.VectorConstraint:
		// A Vector's elements are floats; coerce them as a List<Float>'s.
		elem = schema.NewFloatConstraint()
	default:
		return nil, fmt.Errorf("cannot coerce a list value against a scalar %s constraint", c.Kind())
	}
	//exhaustive:enforce
	switch elem.Kind() {
	case schema.KindString, schema.KindUUID, schema.KindEnum, schema.KindPattern:
		out := make([]string, len(raw))
		for i, v := range raw {
			s, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("list element %d: cannot use %T as a %s element (want string)", i, v, elem.Kind())
			}
			out[i] = s
		}
		return out, nil
	case schema.KindInteger:
		out := make([]int64, len(raw))
		for i, v := range raw {
			n, ok := repairInt64(v)
			if !ok {
				return nil, fmt.Errorf("list element %d: cannot use %T as an Integer element (want an integer type)", i, v)
			}
			out[i] = n
		}
		return out, nil
	case schema.KindFloat:
		out := make([]float64, len(raw))
		for i, v := range raw {
			cv, err := Coerce(elem, v)
			if err != nil {
				return nil, fmt.Errorf("list element %d: %w", i, err)
			}
			f, ok := cv.(float64)
			if !ok {
				return nil, fmt.Errorf("list element %d: cannot use %T as a Float element (want a numeric type)", i, v)
			}
			out[i] = f
		}
		return out, nil
	case schema.KindBoolean:
		out := make([]bool, len(raw))
		for i, v := range raw {
			b, ok := v.(bool)
			if !ok {
				return nil, fmt.Errorf("list element %d: cannot use %T as a Boolean element (want bool)", i, v)
			}
			out[i] = b
		}
		return out, nil
	case schema.KindDate:
		out := make([]dbtype.Date, len(raw))
		for i, v := range raw {
			cv, err := Coerce(elem, v)
			if err != nil {
				return nil, fmt.Errorf("list element %d: %w", i, err)
			}
			d, ok := cv.(dbtype.Date)
			if !ok {
				return nil, fmt.Errorf("list element %d: cannot use %T as a Date element (want a YYYY-MM-DD string or time.Time)", i, v)
			}
			out[i] = d
		}
		return out, nil
	case schema.KindTimestamp:
		out := make([]time.Time, len(raw))
		for i, v := range raw {
			cv, err := Coerce(elem, v)
			if err != nil {
				return nil, fmt.Errorf("list element %d: %w", i, err)
			}
			tt, ok := cv.(time.Time)
			if !ok {
				return nil, fmt.Errorf("list element %d: cannot use %T as a Timestamp element (want an RFC3339 string or time.Time)", i, v)
			}
			out[i] = tt
		}
		return out, nil
	case schema.KindVector, schema.KindList, schema.KindAlias:
		// A nested-collection element (List<Vector>, List<List<…>>) has no concrete
		// driver slice type at this level, so the []any passes through unchanged.
		// KindAlias is unreachable here (elem is alias-resolved above) but is listed
		// to satisfy the exhaustiveness guard.
		return raw, nil
	default:
		// Unreachable: schema.Constraint is sealed, so elem.Kind() is always one of
		// the cases above. The //exhaustive:enforce directive fails the build if a
		// new ConstraintKind is added without a case, rather than letting a []any
		// reach the driver un-coerced.
		return nil, fmt.Errorf("coerceSlice: unhandled element kind %v", elem.Kind())
	}
}

// repairInt64 widens any Go signed or unsigned integer to int64, so a List<Integer>
// hand-built with narrower (or unsigned) ints reaches the driver as []int64 — the
// same width repair [Coerce] applies for Float. It reports false for a non-integer
// value, or a uint/uint64 that exceeds the int64 range (matching the validator's
// coerceInteger overflow guard). A float is intentionally not an integer here: a
// fractional value under an Integer constraint is a type error worth surfacing, not
// one to silently truncate.
func repairInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int8:
		return int64(n), true
	case int16:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	case uint:
		u := uint64(n)
		if u > math.MaxInt64 {
			return 0, false
		}
		return int64(u), true
	case uint8:
		return int64(n), true
	case uint16:
		return int64(n), true
	case uint32:
		return int64(n), true
	case uint64:
		if n > math.MaxInt64 {
			return 0, false
		}
		return int64(n), true
	default:
		return 0, false
	}
}

// coerceRelProps coerces a relationship property map against the relation's
// declared property constraints, so typed edge properties (e.g. a Timestamp or
// Float on an association) reach the driver as native types. Each value routes
// through [coerceValue], the same chokepoint the node path uses.
//
// Edge properties are scalar by language rule: List and Vector types on a
// relationship are rejected at schema-load (diag.E_LIST_ON_EDGE /
// diag.E_INVALID_CONSTRAINT, both alias-aware), so the list arm of coerceValue
// is unreachable here. It is shared anyway rather than special-casing a
// scalar-only path, so this stays correct if that rule ever changes.
//
// Properties not declared on rel pass through unchanged; a nil relation, or one
// with no declared properties, returns props untouched. Properties are processed
// in sorted key order, so a coercion error names the lexicographically-first
// failing property on every run (matching [CoerceParams] and [propsToParamMap]).
// Mutates and returns props, which callers pass as a fresh clone.
func coerceRelProps(props map[string]any, rel *schema.Relation) (map[string]any, error) {
	if rel == nil || !rel.HasProperties() {
		return props, nil
	}
	for _, k := range slices.Sorted(maps.Keys(props)) {
		v := props[k]
		p, ok := rel.Property(k)
		if !ok || v == nil {
			continue
		}
		cv, err := coerceValue(p.Constraint(), v)
		if err != nil {
			return nil, fmt.Errorf("relation %q property %q: %w", rel.Name(), k, err)
		}
		props[k] = cv
	}
	return props, nil
}

// coerceKey converts one primary-key value to the driver-native type its
// declared constraint requires, using the same [coerceValue] chokepoint the
// property path uses.
//
// A MERGE binds the key and SETs the property from two different maps, so a key
// left raw where its property is coerced makes the two carry different Go types
// for every transforming kind (Date, Timestamp, Float): the pattern matches no
// stored node, and each write inserts another duplicate. A shape carrying no
// constraint for the key passes the value through, which is what a shape that
// never declared the key's type can honestly do.
func coerceKey(shape *NodeShape, name string, raw any) (any, error) {
	cv, err := coerceValue(shape.keyConstraints[name], raw)
	if err != nil {
		return nil, fmt.Errorf("primary key %q: %w", name, err)
	}
	return cv, nil
}

// extractKeyProps extracts the shape's primary key properties from immutable
// properties, coercing each against its declared constraint. All keys must be
// present and non-nil.
func extractKeyProps(props immutable.Properties, shape *NodeShape) (map[string]any, error) {
	keyNames := shape.PrimaryKeys
	if len(keyNames) == 0 {
		return nil, errors.New("no primary keys defined")
	}

	result := make(map[string]any, len(keyNames))
	var missing []string
	var nilKeys []string

	for _, name := range keyNames {
		val, ok := props.Get(name)
		if !ok {
			missing = append(missing, name)
			continue
		}
		unwrapped := val.Unwrap()
		if unwrapped == nil {
			nilKeys = append(nilKeys, name)
			continue
		}
		cv, err := coerceKey(shape, name, unwrapped)
		if err != nil {
			return nil, err
		}
		result[name] = cv
	}

	if len(missing) > 0 && len(nilKeys) > 0 {
		return nil, fmt.Errorf("missing required primary key(s): %v; nil primary key(s): %v", missing, nilKeys)
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required primary key(s): %v", missing)
	}
	if len(nilKeys) > 0 {
		return nil, fmt.Errorf("nil primary key(s): %v", nilKeys)
	}
	return result, nil
}

// removeKeys creates a copy of props without the specified keys.
func removeKeys(props map[string]any, keys []string) map[string]any {
	result := make(map[string]any, len(props))
	maps.Copy(result, props)
	for _, k := range keys {
		delete(result, k)
	}
	return result
}

// chunkSlice splits a slice into chunks of at most size elements.
func chunkSlice[T any](items []T, size int) [][]T {
	if size <= 0 {
		size = 1
	}
	var chunks [][]T
	for i := 0; i < len(items); i += size {
		end := min(i+size, len(items))
		chunks = append(chunks, items[i:end])
	}
	return chunks
}

// edgeSignature identifies edges that can be batched together.
type edgeSignature struct {
	sourceType string
	relType    string
	targetType string
}

// groupEdgesBySignature groups edges by their (sourceType, relationType, targetType) signature.
func groupEdgesBySignature(edges []*graph.Edge) map[edgeSignature][]*graph.Edge {
	groups := make(map[edgeSignature][]*graph.Edge)
	for _, edge := range edges {
		sig := edgeSignature{
			sourceType: edge.Source().TypeName(),
			relType:    edge.Relation(),
			targetType: edge.Target().TypeName(),
		}
		groups[sig] = append(groups[sig], edge)
	}
	return groups
}
