package neo4j

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/neo4j/neo4j-go-driver/v6/neo4j/dbtype"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// NodeQuery represents a parameterized Cypher query for a single node upsert.
//
// Construct via [Adapter.NodeQueryFor]; do not create directly.
type NodeQuery struct {
	Statement string         // Cypher MERGE ... SET statement
	Params    map[string]any // Query parameters
}

// BatchNodeQuery represents an UNWIND-based batch node upsert.
//
// Construct via [Adapter.BatchNodeQueries]; do not create directly.
type BatchNodeQuery struct {
	Statement string         // Cypher UNWIND $rows AS row MERGE ... SET statement
	Params    map[string]any // Contains "rows" key with []map[string]any value
}

// EdgeQuery represents a parameterized Cypher query for a relationship merge.
//
// Construct via [Adapter.EdgeQueryFor]; do not create directly.
type EdgeQuery struct {
	Statement string         // Cypher MATCH ... MERGE ... SET statement
	Params    map[string]any // Query parameters
}

// BatchEdgeQuery represents an UNWIND-based batch relationship merge.
//
// Construct via [Adapter.BatchEdgeQueries]; do not create directly.
type BatchEdgeQuery struct {
	Statement    string         // Cypher UNWIND $rows AS row MATCH ... MERGE ... SET statement
	Params       map[string]any // Contains "rows" key with []map[string]any value
	RelationType string         // Neo4j relationship type (e.g., "IN_STATE")
}

// WriteOption configures write query generation.
type WriteOption func(*writeConfig)

type writeConfig struct {
	immutableKeys []string
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

// WithImmutableKeys specifies properties that should only be set when a node
// is first created (ON CREATE SET), not when merging with an existing node.
// Example: "first_seen_at" should not be overwritten on re-ingestion.
func WithImmutableKeys(keys ...string) WriteOption {
	return func(c *writeConfig) {
		c.immutableKeys = keys
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

// NodeSource provides the data needed to generate a node MERGE query.
//
// Both [*graph.Instance] and [*instance.ValidInstance] satisfy this interface.
type NodeSource interface {
	TypeName() string
	Properties() immutable.Properties
}

// NodeQueryFor generates a single-node MERGE query for an instance.
//
// The query uses the NodeShape's label and primary keys for MERGE matching,
// and SET for all properties. If immutable keys are configured, uses
// ON CREATE SET / ON MATCH SET to preserve immutable values.
//
// schemaType provides constraint metadata for schema-aware slice coercion
// (e.g., converting []any to []string for List<String> properties). This
// matches the coercion behavior of [Adapter.BatchNodeQueries].
//
// Any type satisfying [NodeSource] may be passed — both [*graph.Instance]
// (graph-based path) and [*instance.ValidInstance] (streaming path) work.
//
//nolint:revive // ctx reserved for future use (cancellation, tracing)
func (a *Adapter) NodeQueryFor(
	ctx context.Context,
	shape *NodeShape,
	inst NodeSource,
	schemaType *schema.Type,
	opts ...WriteOption,
) (*NodeQuery, error) {
	cfg := defaultWriteConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	keyProps, err := extractKeyProps(inst.Properties(), shape.PrimaryKeys)
	if err != nil {
		return nil, fmt.Errorf("type %q: %w", inst.TypeName(), err)
	}

	props, err := propsToParamMap(inst.Properties(), schemaType)
	if err != nil {
		return nil, fmt.Errorf("type %q: %w", inst.TypeName(), err)
	}

	params := make(map[string]any)
	for k, v := range keyProps {
		params["key_"+k] = v
	}
	params["props"] = props

	hasImmutable := len(cfg.immutableKeys) > 0
	km := MutableKeys
	if hasImmutable {
		km = ImmutableKeys
		params["update_props"] = removeKeys(props, cfg.immutableKeys)
	}

	stmt := BuildNodeMergeQuery(shape.Label, shape.PrimaryKeys, km)
	return &NodeQuery{Statement: stmt, Params: params}, nil
}

// BatchNodeQueries generates UNWIND-batched MERGE queries for all instances
// of each type in a graph result.
//
// Returns one [BatchNodeQuery] per type per chunk. Types with more instances
// than the chunk size produce multiple queries.
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

	hasImmutable := len(cfg.immutableKeys) > 0
	km := MutableKeys
	if hasImmutable {
		km = ImmutableKeys
	}
	var queries []*BatchNodeQuery

	for _, typeName := range result.Types() {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("batch node queries: %w", err)
		}
		nodeShape, ok := shapes.Types[typeName]
		if !ok {
			return nil, fmt.Errorf("no shape for type %q", typeName)
		}

		schemaType, ok := result.Schema().Type(typeName)
		if !ok {
			return nil, fmt.Errorf("type %q not found in schema", typeName)
		}

		instances := result.InstancesOf(typeName)
		var rows []map[string]any

		for _, inst := range instances {
			keyProps, err := extractKeyProps(inst.Properties(), nodeShape.PrimaryKeys)
			if err != nil {
				return nil, fmt.Errorf("type %q: %w", typeName, err)
			}

			props, err := propsToParamMap(inst.Properties(), schemaType)
			if err != nil {
				return nil, fmt.Errorf("type %q: %w", typeName, err)
			}
			row := make(map[string]any, len(keyProps)+2)
			maps.Copy(row, keyProps)
			row["props"] = props
			if hasImmutable {
				row["update_props"] = removeKeys(props, cfg.immutableKeys)
			}
			rows = append(rows, row)
		}

		stmt := BuildBatchNodeMergeQuery(nodeShape.Label, nodeShape.PrimaryKeys, km)
		for _, chunk := range chunkSlice(rows, cfg.nodeChunkSize) {
			queries = append(queries, &BatchNodeQuery{
				Statement: stmt,
				Params:    map[string]any{"rows": chunk},
			})
		}
	}

	return queries, nil
}

// EdgeQueryFor generates a single relationship MERGE query for a graph edge.
//
// Edge properties are passed through uncoerced: EdgeQueryFor takes a resolved
// [*graph.Edge] with no schema handle, so unlike the schema-aware
// [Adapter.EdgeQueriesFor] and [Adapter.BatchEdgeQueries] it cannot map typed
// relationship properties (Timestamp/Date/Float) to driver-native types. No
// current schema declares typed relationship properties, so this is latent;
// thread a [*schema.Relation] through this signature when one first does.
//
//nolint:revive // opts reserved for future edge-level write options
func (a *Adapter) EdgeQueryFor(
	ctx context.Context,
	edge *graph.Edge,
	shapes *GraphShape,
	opts ...WriteOption,
) (*EdgeQuery, error) {
	if err := ValidateIdentifier(edge.Relation(), "relationship type"); err != nil {
		return nil, err
	}

	srcShape, ok := shapes.Types[edge.Source().TypeName()]
	if !ok {
		return nil, fmt.Errorf("no shape for source type %q", edge.Source().TypeName())
	}
	tgtShape, ok := shapes.Types[edge.Target().TypeName()]
	if !ok {
		return nil, fmt.Errorf("no shape for target type %q", edge.Target().TypeName())
	}

	srcKeys, err := extractKeyProps(edge.Source().Properties(), srcShape.PrimaryKeys)
	if err != nil {
		return nil, fmt.Errorf("source %q: %w", edge.Source().TypeName(), err)
	}
	tgtKeys, err := extractKeyProps(edge.Target().Properties(), tgtShape.PrimaryKeys)
	if err != nil {
		return nil, fmt.Errorf("target %q: %w", edge.Target().TypeName(), err)
	}

	params := make(map[string]any)
	for k, v := range srcKeys {
		params["from_key_"+k] = v
	}
	for k, v := range tgtKeys {
		params["to_key_"+k] = v
	}

	hasProps := edge.HasProperties()
	if hasProps {
		params["rel_props"] = edge.Properties().Clone()
	}

	stmt := BuildRelationshipMergeQuery(
		srcShape.Label, srcShape.PrimaryKeys,
		edge.Relation(),
		tgtShape.Label, tgtShape.PrimaryKeys,
		hasProps,
	)

	return &EdgeQuery{Statement: stmt, Params: params}, nil
}

// EdgeQueriesFor generates relationship MERGE queries for all association
// edges of a validated instance.
//
// Unlike [Adapter.EdgeQueryFor] (which operates on a single resolved [*graph.Edge]),
// this method works directly with a [*instance.ValidInstance], generating one
// [EdgeQuery] per edge target across all association relations.
//
// The schemaType resolves relation metadata (target type, cardinality).
// The shapes map must contain [NodeShape] entries for both the source type
// and all target types referenced by the instance's edges.
//
//nolint:revive // opts reserved for future edge-level write options
func (a *Adapter) EdgeQueriesFor(
	ctx context.Context,
	inst *instance.ValidInstance,
	schemaType *schema.Type,
	shapes *GraphShape,
	opts ...WriteOption,
) ([]*EdgeQuery, error) {
	srcShape, ok := shapes.Types[inst.TypeName()]
	if !ok {
		return nil, fmt.Errorf("no shape for source type %q", inst.TypeName())
	}

	var queries []*EdgeQuery

	for relationName, edgeData := range inst.Edges() {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("edge queries for: %w", err)
		}

		rel, ok := schemaType.Relation(relationName)
		if !ok || rel.Kind() != schema.RelationAssociation {
			continue
		}

		if err := ValidateIdentifier(relationName, "relationship type"); err != nil {
			return nil, err
		}

		targetTypeName := rel.TargetID().Name()
		tgtShape, ok := shapes.Types[targetTypeName]
		if !ok {
			return nil, fmt.Errorf("no shape for target type %q (relation %q)", targetTypeName, relationName)
		}

		srcKeys, err := extractKeyProps(inst.Properties(), srcShape.PrimaryKeys)
		if err != nil {
			return nil, fmt.Errorf("source %q: %w", inst.TypeName(), err)
		}

		for target := range edgeData.TargetsIter() {
			tgtKeys, err := extractKeyFromImmutableKey(target.TargetKey(), tgtShape.PrimaryKeys)
			if err != nil {
				return nil, fmt.Errorf("target %q (relation %q): %w", targetTypeName, relationName, err)
			}

			params := make(map[string]any)
			for k, v := range srcKeys {
				params["from_key_"+k] = v
			}
			for k, v := range tgtKeys {
				params["to_key_"+k] = v
			}

			hasProps := target.HasProperties()
			if hasProps {
				relProps, err := coerceRelProps(target.Properties().Clone(), rel)
				if err != nil {
					return nil, fmt.Errorf("target %q (relation %q): %w", targetTypeName, relationName, err)
				}
				params["rel_props"] = relProps
			}

			stmt := BuildRelationshipMergeQuery(
				srcShape.Label, srcShape.PrimaryKeys,
				relationName,
				tgtShape.Label, tgtShape.PrimaryKeys,
				hasProps,
			)

			queries = append(queries, &EdgeQuery{Statement: stmt, Params: params})
		}
	}

	return queries, nil
}

// BatchEdgeQueries generates UNWIND-batched MERGE queries for edges,
// grouped by (sourceType, relationType, targetType) signature.
//
// Returns one [BatchEdgeQuery] per signature per chunk.
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
			srcKeys, err := extractKeyProps(edge.Source().Properties(), srcShape.PrimaryKeys)
			if err != nil {
				return nil, fmt.Errorf("source %q: %w", sig.sourceType, err)
			}
			tgtKeys, err := extractKeyProps(edge.Target().Properties(), tgtShape.PrimaryKeys)
			if err != nil {
				return nil, fmt.Errorf("target %q: %w", sig.targetType, err)
			}

			row := make(map[string]any)
			for k, v := range srcKeys {
				row["from_"+k] = v
			}
			for k, v := range tgtKeys {
				row["to_"+k] = v
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

		stmt := BuildBatchRelationshipMergeQuery(
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

// propsToParamMap converts instance properties to a Neo4j-driver-compatible
// map, routing every scalar through [Coerce] and every []any slice through
// [coerceSlice] against the property's schema constraint. This repairs the
// JSON round-trip — a whole-number Float decoded as int64, and Date/Timestamp
// values carried as strings — so the driver receives native types that satisfy
// Neo4j TYPE constraints (IS :: FLOAT, IS :: DATE, IS :: ZONED DATETIME).
// Returns an error, naming the offending property, if a value cannot be coerced
// to its declared kind (e.g. an unparseable Timestamp/Date string).
func propsToParamMap(props immutable.Properties, schemaType *schema.Type) (map[string]any, error) {
	raw := props.Clone()
	if schemaType == nil {
		return raw, nil
	}

	for key, val := range raw {
		if val == nil {
			continue
		}
		prop, found := schemaType.Property(key)
		if !found {
			continue
		}
		if slice, ok := val.([]any); ok {
			cv, err := coerceSlice(slice, prop.Constraint())
			if err != nil {
				return nil, fmt.Errorf("property %q: %w", key, err)
			}
			raw[key] = cv
			continue
		}
		cv, err := Coerce(schema.ResolveAlias(prop.Constraint()).Kind(), val)
		if err != nil {
			return nil, fmt.Errorf("property %q: %w", key, err)
		}
		raw[key] = cv
	}
	return raw, nil
}

// coerceSlice converts a []any to a concrete typed slice using the list
// constraint's element kind. Per-element value conversion delegates to
// [Coerce] so the Float int-repair and temporal-parse rules live in one place;
// coerceSlice owns only the slice typing. Returns an error if an element fails
// coercion (e.g. an unparseable temporal string). An element that is simply not
// the expected Go type falls back to the raw slice, preserving the prior
// best-effort behavior for type mismatches.
func coerceSlice(raw []any, c schema.Constraint) (any, error) {
	c = schema.ResolveAlias(c)
	lc, ok := c.(schema.ListConstraint)
	if !ok {
		return raw, nil
	}
	elem := schema.ResolveAlias(lc.Element())
	switch elem.Kind() {
	case schema.KindString, schema.KindUUID, schema.KindEnum, schema.KindPattern:
		out := make([]string, len(raw))
		for i, v := range raw {
			s, ok := v.(string)
			if !ok {
				return raw, nil
			}
			out[i] = s
		}
		return out, nil
	case schema.KindInteger:
		out := make([]int64, len(raw))
		for i, v := range raw {
			n, ok := v.(int64)
			if !ok {
				return raw, nil
			}
			out[i] = n
		}
		return out, nil
	case schema.KindFloat:
		out := make([]float64, len(raw))
		for i, v := range raw {
			cv, err := Coerce(schema.KindFloat, v)
			if err != nil {
				return nil, err
			}
			f, ok := cv.(float64)
			if !ok {
				return raw, nil
			}
			out[i] = f
		}
		return out, nil
	case schema.KindBoolean:
		out := make([]bool, len(raw))
		for i, v := range raw {
			b, ok := v.(bool)
			if !ok {
				return raw, nil
			}
			out[i] = b
		}
		return out, nil
	case schema.KindDate:
		out := make([]dbtype.Date, len(raw))
		for i, v := range raw {
			cv, err := Coerce(schema.KindDate, v)
			if err != nil {
				return nil, err
			}
			d, ok := cv.(dbtype.Date)
			if !ok {
				return raw, nil
			}
			out[i] = d
		}
		return out, nil
	case schema.KindTimestamp:
		out := make([]time.Time, len(raw))
		for i, v := range raw {
			cv, err := Coerce(schema.KindTimestamp, v)
			if err != nil {
				return nil, err
			}
			tt, ok := cv.(time.Time)
			if !ok {
				return raw, nil
			}
			out[i] = tt
		}
		return out, nil
	default:
		return raw, nil
	}
}

// coerceRelProps coerces a relationship property map against the relation's
// declared property constraints, so typed edge properties (e.g. a Timestamp or
// Float on an association) reach the driver as native types. Properties not
// declared on rel pass through unchanged; a nil relation, or one with no
// declared properties, returns props untouched. Mutates and returns props,
// which callers pass as a fresh clone.
func coerceRelProps(props map[string]any, rel *schema.Relation) (map[string]any, error) {
	if rel == nil || !rel.HasProperties() {
		return props, nil
	}
	for k, v := range props {
		p, ok := rel.Property(k)
		if !ok || v == nil {
			continue
		}
		cv, err := Coerce(schema.ResolveAlias(p.Constraint()).Kind(), v)
		if err != nil {
			return nil, fmt.Errorf("relation %q property %q: %w", rel.Name(), k, err)
		}
		props[k] = cv
	}
	return props, nil
}

// extractKeyProps extracts named primary key properties from immutable properties.
// All keys must be present and non-nil.
func extractKeyProps(props immutable.Properties, keyNames []string) (map[string]any, error) {
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
		result[name] = unwrapped
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

// extractKeyFromImmutableKey extracts named key properties from a positional immutable.Key.
// It zips key components with the provided names. The key must have exactly len(keyNames) components.
func extractKeyFromImmutableKey(key immutable.Key, keyNames []string) (map[string]any, error) {
	if len(keyNames) == 0 {
		return nil, errors.New("no primary keys defined")
	}
	if key.Len() != len(keyNames) {
		return nil, fmt.Errorf("key has %d components but %d key names provided", key.Len(), len(keyNames))
	}

	result := make(map[string]any, len(keyNames))
	var nilKeys []string
	for i, name := range keyNames {
		val := key.Get(i).Unwrap()
		if val == nil {
			nilKeys = append(nilKeys, name)
			continue
		}
		result[name] = val
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
