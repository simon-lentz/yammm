package neo4j

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sort"
	"strings"
	"time"

	"github.com/neo4j/neo4j-go-driver/v6/neo4j/dbtype"
	"github.com/simon-lentz/yammm/graph"
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

// NodeQueryFor generates a single-node MERGE query for a graph instance.
//
// The query uses the NodeShape's label and primary keys for MERGE matching,
// and SET for all properties. If immutable keys are configured, uses
// ON CREATE SET / ON MATCH SET to preserve immutable values.
//
// schemaType provides constraint metadata for schema-aware slice coercion
// (e.g., converting []any to []string for List<String> properties). This
// matches the coercion behavior of [Adapter.BatchNodeQueries].
func (a *Adapter) NodeQueryFor(
	_ context.Context,
	shape *NodeShape,
	inst *graph.Instance,
	schemaType *schema.Type,
	opts ...WriteOption,
) (*NodeQuery, error) {
	cfg := defaultWriteConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	keyProps, err := extractKeyProps(inst, shape.PrimaryKeys)
	if err != nil {
		return nil, fmt.Errorf("type %q: %w", inst.TypeName(), err)
	}

	props := propsToParamMap(inst, schemaType)

	params := make(map[string]any)
	for k, v := range keyProps {
		params["key_"+k] = v
	}
	params["props"] = props

	hasImmutable := len(cfg.immutableKeys) > 0
	if hasImmutable {
		params["update_props"] = removeKeys(props, cfg.immutableKeys)
	}

	stmt := buildNodeMergeQuery(shape.Label, shape.PrimaryKeys, hasImmutable)
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
			keyProps, err := extractKeyProps(inst, nodeShape.PrimaryKeys)
			if err != nil {
				return nil, fmt.Errorf("type %q: %w", typeName, err)
			}

			props := propsToParamMap(inst, schemaType)
			row := make(map[string]any, len(keyProps)+2)
			maps.Copy(row, keyProps)
			row["props"] = props
			if hasImmutable {
				row["update_props"] = removeKeys(props, cfg.immutableKeys)
			}
			rows = append(rows, row)
		}

		stmt := buildBatchNodeMergeQuery(nodeShape.Label, nodeShape.PrimaryKeys, hasImmutable)
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
//nolint:revive // opts reserved for future edge-level write options
func (a *Adapter) EdgeQueryFor(
	_ context.Context,
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

	srcKeys, err := extractKeyProps(edge.Source(), srcShape.PrimaryKeys)
	if err != nil {
		return nil, fmt.Errorf("source %q: %w", edge.Source().TypeName(), err)
	}
	tgtKeys, err := extractKeyProps(edge.Target(), tgtShape.PrimaryKeys)
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

	stmt := buildRelationshipMergeQuery(
		srcShape.Label, srcShape.PrimaryKeys,
		edge.Relation(),
		tgtShape.Label, tgtShape.PrimaryKeys,
		hasProps,
	)

	return &EdgeQuery{Statement: stmt, Params: params}, nil
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
	sort.Slice(sigs, func(i, j int) bool {
		if sigs[i].sourceType != sigs[j].sourceType {
			return sigs[i].sourceType < sigs[j].sourceType
		}
		if sigs[i].relType != sigs[j].relType {
			return sigs[i].relType < sigs[j].relType
		}
		return sigs[i].targetType < sigs[j].targetType
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
			srcKeys, err := extractKeyProps(edge.Source(), srcShape.PrimaryKeys)
			if err != nil {
				return nil, fmt.Errorf("source %q: %w", sig.sourceType, err)
			}
			tgtKeys, err := extractKeyProps(edge.Target(), tgtShape.PrimaryKeys)
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
					row["rel_props"] = edge.Properties().Clone()
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

// --- internal helpers ---

// propsToParamMap converts instance properties to a Neo4j-driver-compatible map.
// Coerces []any slices to concrete typed slices, and scalar Date/Timestamp strings
// to native temporal types, using schema constraint metadata. Without this coercion,
// Neo4j TYPE constraints (IS :: DATE, IS :: ZONED DATETIME) reject string values.
// Date strings are coerced to dbtype.Date; Timestamp strings to time.Time.
func propsToParamMap(inst *graph.Instance, schemaType *schema.Type) map[string]any {
	raw := inst.Properties().Clone()
	if schemaType == nil {
		return raw
	}

	for key, val := range raw {
		if val == nil {
			continue
		}
		if slice, ok := val.([]any); ok {
			prop, found := schemaType.Property(key)
			if found {
				raw[key] = coerceSlice(slice, prop.Constraint())
			}
			continue
		}
		// Coerce scalar Date/Timestamp strings to time.Time so the Neo4j
		// driver sends native temporal types that satisfy TYPE constraints.
		if s, ok := val.(string); ok {
			prop, found := schemaType.Property(key)
			if !found {
				continue
			}
			raw[key] = coerceTemporalScalar(s, prop.Constraint())
		}
	}
	return raw
}

// coerceTemporalScalar converts a string value to a Neo4j-driver-compatible
// temporal type based on the schema constraint:
//   - KindDate → dbtype.Date (Neo4j DATE)
//   - KindTimestamp → time.Time (Neo4j ZONED DATETIME)
//
// Returns the original string if parsing fails or the constraint is not temporal.
func coerceTemporalScalar(s string, c schema.Constraint) any {
	c = effectiveConstraint(c)
	switch c.Kind() {
	case schema.KindDate:
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			return s
		}
		return dbtype.Date(t)
	case schema.KindTimestamp:
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			// Try RFC3339Nano for sub-second precision
			t, err = time.Parse(time.RFC3339Nano, s)
			if err != nil {
				return s
			}
		}
		return t
	default:
		return s
	}
}

// coerceSlice converts []any to a concrete typed slice using schema constraint metadata.
func coerceSlice(raw []any, c schema.Constraint) any {
	c = effectiveConstraint(c)
	lc, ok := c.(schema.ListConstraint)
	if !ok {
		return raw
	}
	elem := effectiveConstraint(lc.Element())
	switch elem.Kind() {
	case schema.KindString, schema.KindUUID, schema.KindEnum, schema.KindPattern:
		out := make([]string, len(raw))
		for i, v := range raw {
			s, ok := v.(string)
			if !ok {
				return raw
			}
			out[i] = s
		}
		return out
	case schema.KindInteger:
		out := make([]int64, len(raw))
		for i, v := range raw {
			n, ok := v.(int64)
			if !ok {
				return raw
			}
			out[i] = n
		}
		return out
	case schema.KindFloat:
		out := make([]float64, len(raw))
		for i, v := range raw {
			f, ok := v.(float64)
			if !ok {
				return raw
			}
			out[i] = f
		}
		return out
	case schema.KindBoolean:
		out := make([]bool, len(raw))
		for i, v := range raw {
			b, ok := v.(bool)
			if !ok {
				return raw
			}
			out[i] = b
		}
		return out
	case schema.KindDate:
		if len(raw) == 0 {
			return raw
		}
		switch raw[0].(type) {
		case time.Time:
			out := make([]dbtype.Date, len(raw))
			for i, v := range raw {
				t, ok := v.(time.Time)
				if !ok {
					return raw
				}
				out[i] = dbtype.Date(t)
			}
			return out
		case string:
			out := make([]dbtype.Date, len(raw))
			for i, v := range raw {
				s, ok := v.(string)
				if !ok {
					return raw
				}
				t, err := time.Parse("2006-01-02", s)
				if err != nil {
					return coerceToStringSlice(raw)
				}
				out[i] = dbtype.Date(t)
			}
			return out
		default:
			return raw
		}
	case schema.KindTimestamp:
		if len(raw) == 0 {
			return raw
		}
		switch raw[0].(type) {
		case time.Time:
			out := make([]time.Time, len(raw))
			for i, v := range raw {
				t, ok := v.(time.Time)
				if !ok {
					return raw
				}
				out[i] = t
			}
			return out
		case string:
			out := make([]time.Time, len(raw))
			for i, v := range raw {
				s, ok := v.(string)
				if !ok {
					return raw
				}
				t, err := time.Parse(time.RFC3339, s)
				if err != nil {
					t, err = time.Parse(time.RFC3339Nano, s)
					if err != nil {
						return coerceToStringSlice(raw)
					}
				}
				out[i] = t
			}
			return out
		default:
			return raw
		}
	default:
		return raw
	}
}

// coerceToStringSlice converts []any to []string, returning the original
// slice if any element is not a string. Used as a fallback when temporal
// parsing fails for list elements.
func coerceToStringSlice(raw []any) any {
	out := make([]string, len(raw))
	for i, v := range raw {
		s, ok := v.(string)
		if !ok {
			return raw
		}
		out[i] = s
	}
	return out
}

// extractKeyProps extracts named primary key properties from an instance.
// All keys must be present and non-nil.
func extractKeyProps(inst *graph.Instance, keyNames []string) (map[string]any, error) {
	if len(keyNames) == 0 {
		return nil, errors.New("no primary keys defined")
	}

	result := make(map[string]any, len(keyNames))
	var missing []string
	var nilKeys []string

	for _, name := range keyNames {
		val, ok := inst.Properties().Get(name)
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

// --- Cypher query builders ---

// buildNodeMergeQuery produces a single-node MERGE query.
func buildNodeMergeQuery(label string, keyNames []string, hasImmutableKeys bool) string {
	var b strings.Builder
	b.WriteString("MERGE (n:")
	b.WriteString(label)
	b.WriteString(" {")
	for i, name := range keyNames {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s: $key_%s", name, name)
	}
	b.WriteString("})")

	if hasImmutableKeys {
		b.WriteString("\nON CREATE SET n += $props")
		b.WriteString("\nON MATCH SET n += $update_props")
	} else {
		b.WriteString("\nSET n += $props")
	}
	return b.String()
}

// buildBatchNodeMergeQuery produces an UNWIND-based batch node MERGE query.
func buildBatchNodeMergeQuery(label string, keyNames []string, hasImmutableKeys bool) string {
	var b strings.Builder
	b.WriteString("UNWIND $rows AS row\n")
	b.WriteString("MERGE (n:")
	b.WriteString(label)
	b.WriteString(" {")
	for i, name := range keyNames {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s: row.%s", name, name)
	}
	b.WriteString("})")

	if hasImmutableKeys {
		b.WriteString("\nON CREATE SET n += row.props")
		b.WriteString("\nON MATCH SET n += row.update_props")
	} else {
		b.WriteString("\nSET n += row.props")
	}
	return b.String()
}

// buildRelationshipMergeQuery produces a single relationship MERGE query.
func buildRelationshipMergeQuery(
	fromLabel string, fromKeyNames []string,
	relType string,
	toLabel string, toKeyNames []string,
	hasProps bool,
) string {
	var b strings.Builder

	b.WriteString("MATCH (from:")
	b.WriteString(fromLabel)
	b.WriteString(" {")
	for i, name := range fromKeyNames {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s: $from_key_%s", name, name)
	}
	b.WriteString("})\n")

	b.WriteString("MATCH (to:")
	b.WriteString(toLabel)
	b.WriteString(" {")
	for i, name := range toKeyNames {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s: $to_key_%s", name, name)
	}
	fmt.Fprintf(&b, "})\nMERGE (from)-[r:%s]->(to)", relType)

	if hasProps {
		b.WriteString("\nSET r += $rel_props")
	}
	return b.String()
}

// buildBatchRelationshipMergeQuery produces an UNWIND-based batch relationship MERGE query.
func buildBatchRelationshipMergeQuery(
	fromLabel string, fromKeyNames []string,
	relType string,
	toLabel string, toKeyNames []string,
	hasProps bool,
) string {
	var b strings.Builder

	b.WriteString("UNWIND $rows AS row\n")
	b.WriteString("MATCH (from:")
	b.WriteString(fromLabel)
	b.WriteString(" {")
	for i, name := range fromKeyNames {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s: row.from_%s", name, name)
	}
	b.WriteString("})\n")

	b.WriteString("MATCH (to:")
	b.WriteString(toLabel)
	b.WriteString(" {")
	for i, name := range toKeyNames {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s: row.to_%s", name, name)
	}
	fmt.Fprintf(&b, "})\nMERGE (from)-[r:%s]->(to)", relType)

	if hasProps {
		b.WriteString("\nSET r += row.rel_props")
	}
	return b.String()
}
