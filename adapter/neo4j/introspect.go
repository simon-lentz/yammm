package neo4j

import (
	"fmt"
	"math"
	"strings"
)

// RemoteConstraint represents a constraint as reported by SHOW CONSTRAINTS YIELD *.
//
// Parse from database output via [ParseRemoteConstraints].
type RemoteConstraint struct {
	Name string // Constraint name

	// Type is verbatim what the server reported, and its spelling for a node
	// uniqueness constraint DEPENDS ON THE SERVER GENERATION: Neo4j 5.x reports
	// "UNIQUENESS", 2026.x reports "NODE_PROPERTY_UNIQUENESS". The other kinds
	// ("NODE_PROPERTY_EXISTENCE", "NODE_PROPERTY_TYPE", "NODE_KEY") are stable.
	//
	// A consumer switching on this field must accept both uniqueness spellings,
	// or it silently stops recognising every UNIQUE constraint the moment the
	// database is upgraded. The diff and [Adapter.InferSchema] fold them
	// internally; this field is deliberately not folded, so a diagnostic echoes
	// the kind the operator's own database reports.
	Type string

	EntityType      string   // "NODE" or "RELATIONSHIP"
	LabelsOrTypes   []string // Labels or relationship types
	Properties      []string // Constrained properties
	PropertyType    string   // Enforced type (empty for non-type constraints or Neo4j < 5.9)
	CreateStatement string   // Cypher to recreate the constraint
}

// RemoteRelationship represents a discovered relationship signature
// from a graph-walking query.
//
// Parse from database output via [ParseRemoteRelationships].
type RemoteRelationship struct {
	RelationType string   // e.g., "WORKS_AT", "IN_REGION"
	SourceLabels []string // Labels on source nodes
	TargetLabels []string // Labels on target nodes
}

// RemoteIndex represents an index as reported by SHOW INDEXES, including the
// index backing a constraint — see [RemoteIndex.OwningConstraint].
//
// Parse from database output via [ParseRemoteIndexes].
type RemoteIndex struct {
	Name          string         // Index name
	Type          string         // "RANGE", "TEXT", "POINT", "FULLTEXT", "VECTOR" (BTREE is Neo4j 4.x-historical)
	EntityType    string         // "NODE" or "RELATIONSHIP"
	LabelsOrTypes []string       // Labels or relationship types
	Properties    []string       // Indexed properties
	Options       map[string]any // Raw index options (nil if absent); vector config lives under "indexConfig"

	// State is the index's population state — "ONLINE", "POPULATING", or
	// "FAILED" — or empty when the server did not report it. Only an ONLINE
	// index actually serves queries: a FAILED one exists in SHOW INDEXES with
	// every other column matching its declaration while serving nothing, so
	// comparing definitions alone reports a permanently broken index as in sync.
	State string

	// OwningConstraint names the constraint this index backs, or is empty for a
	// standalone index.
	//
	// [IntrospectIndexesQuery] yields these rows, and [Adapter.DiffIndexes] needs
	// them: a backing index holds its constraint's name against every CREATE
	// INDEX in the database, and already serves the (label, properties, kind) it
	// covers, so both are conditions under which the server silently no-ops a
	// declared index. It is this field that keeps them out of matches and drops —
	// a backing index is created and dropped with its constraint and is never
	// independently declarable, so reporting one as an undeclared orphan advises
	// dropping the index behind a primary key.
	//
	// A consumer filtering rows itself must test this field rather than assume
	// the query excluded them.
	OwningConstraint string
}

// IsOnline reports whether the index is in a state that serves queries. An
// unreported state is treated as online: older servers and hand-built records
// omit the column, and inventing drift for every such row would make the diff
// unusable against them.
func (ri RemoteIndex) IsOnline() bool {
	return ri.State == "" || strings.EqualFold(ri.State, "ONLINE")
}

// VectorDimensions returns the configured dimension of a VECTOR index and true,
// or (0, false) when the index carries no readable vector configuration — a
// non-vector index, an older server that does not report options, or a
// malformed options map.
func (ri RemoteIndex) VectorDimensions() (int, bool) {
	cfg, ok := ri.indexConfig()
	if !ok {
		return 0, false
	}
	return toInt(cfg["vector.dimensions"])
}

// VectorSimilarity returns the configured similarity function of a VECTOR index
// and true, or ("", false) when the index carries no readable vector
// configuration.
//
// The value is the server's, VERBATIM and in the server's casing: Neo4j 5.15+
// reports "COSINE" where the schema side is lowercase "cosine", because DSL
// validation forces the keyword lowercase. Compare it case-insensitively —
// [vectorDrift] uses strings.EqualFold for exactly this reason. A consumer that
// compares it against a lowercase literal flags every correctly-configured
// vector index as drifted.
func (ri RemoteIndex) VectorSimilarity() (string, bool) {
	cfg, ok := ri.indexConfig()
	if !ok {
		return "", false
	}
	s, ok := cfg["vector.similarity_function"].(string)
	if !ok {
		return "", false
	}
	return s, true
}

// indexConfig returns the nested "indexConfig" map from the index options, or
// (nil, false) when options are absent or malformed.
func (ri RemoteIndex) indexConfig() (map[string]any, bool) {
	if ri.Options == nil {
		return nil, false
	}
	cfg, ok := ri.Options["indexConfig"].(map[string]any)
	if !ok {
		return nil, false
	}
	return cfg, true
}

// IntrospectConstraintsQuery returns the Cypher query for fetching all constraints.
// Execute this query against a Neo4j database and pass the results to
// [ParseRemoteConstraints].
//
// YIELD * is REQUIRED here and must not be narrowed to an explicit projection,
// which is the obvious-looking hardening and would break the adapter's supported
// server range.
//
// SHOW CONSTRAINTS rejects a YIELD naming a column the server does not have —
// it is an error, not an empty column — and propertyType does not exist before
// Neo4j 5.9. On 5.8 the statement yields exactly id, name, type, entityType,
// labelsOrTypes, properties, ownedIndex, options and createStatement; a
// projection listing propertyType fails outright there, while YIELD * simply
// returns no such key and [ConstraintDiffResult.Unverified] represents the
// resulting gap. Trading that for a hard failure on every 5.0-5.8 server is a
// bad exchange.
//
// [IntrospectIndexesQuery] is explicit for the opposite reason: every column it
// names has been present across the supported range, so it can state its
// dependencies without narrowing what it works against.
func IntrospectConstraintsQuery() string {
	return "SHOW CONSTRAINTS YIELD *"
}

// IntrospectIndexesQuery returns the Cypher query for fetching every index the
// diff must consider. Filters out only LOOKUP indexes, which are built in and
// cannot be declared or dropped.
//
// Constraint-backing indexes are deliberately INCLUDED. They are never matched
// or dropped — [RemoteIndex.Declarable] rejects them on
// [RemoteIndex.OwningConstraint] — but they block: a server silently no-ops a
// CREATE INDEX whose name they hold, and also one whose (label, properties,
// kind) they already serve. Excluding them here would put both blocks outside
// what the diff can observe, and a blocked declaration reports as a Create the
// server ignores on every run.
//
// Yields the options map so vector configuration is visible, and the state so an
// index that exists but does not serve queries is distinguishable from a healthy
// one.
func IntrospectIndexesQuery() string {
	return "SHOW INDEXES YIELD name, type, entityType, labelsOrTypes, properties, options, state, owningConstraint " +
		"WHERE type <> 'LOOKUP'"
}

// IntrospectRelationshipsQuery returns a Cypher query for discovering
// relationship signatures, optionally narrowed by a label prefix.
//
// The prefix is a HEURISTIC FILTER, not an ownership test. A label starting
// with it may belong to a sibling schema whose sanitized name shares the
// prefix, and a label belonging to this schema is missed if a caller-configured
// prefix or separator does not reproduce the one the writer used.
// [Adapter.OwnedLabels] is the exact set, and it needs a compiled schema —
// which the introspect path does not have, because it exists to produce one.
//
// The scan is unbounded by design: `MATCH (a)-[r]->(b)` visits every
// relationship in the database, and no index serves a label-prefix predicate,
// so the filter reduces what is RETURNED and not what is READ. Cost grows with
// the graph, not with the schema.
//
// Returns the query string and a parameter map. When labelPrefix is empty,
// all relationships are discovered (no filtering).
func IntrospectRelationshipsQuery(labelPrefix string) (string, map[string]any) {
	if labelPrefix == "" {
		return "MATCH (a)-[r]->(b) " +
			"WITH type(r) AS relType, labels(a) AS srcLabels, labels(b) AS tgtLabels " +
			"RETURN DISTINCT relType, srcLabels, tgtLabels", nil
	}

	query := "MATCH (a)-[r]->(b) " +
		"WHERE any(l IN labels(a) WHERE l STARTS WITH $prefix) " +
		"WITH type(r) AS relType, labels(a) AS srcLabels, labels(b) AS tgtLabels " +
		"RETURN DISTINCT relType, srcLabels, tgtLabels"
	return query, map[string]any{"prefix": labelPrefix}
}

// IntrospectRelationshipsQueryFor returns a schema-scoped relationship discovery query.
// It builds the label prefix from the adapter's configuration (labelPrefix + schemaFilter
// + labelSeparator) and delegates to [IntrospectRelationshipsQuery], whose godoc
// states what a prefix can and cannot establish.
//
// The filter is TRIMMED first, exactly as [Adapter.Label] trims a schema name
// before composing a label. Testing the raw value instead meant an all-space
// filter took the scoped path and built a prefix no written label carries,
// discovering nothing and reporting it as an empty database.
//
// When schemaFilter is empty or all space, all relationships are discovered.
func (a *Adapter) IntrospectRelationshipsQueryFor(schemaFilter string) (string, map[string]any) {
	if strings.TrimSpace(schemaFilter) == "" {
		return IntrospectRelationshipsQuery("")
	}
	prefix := a.config.labelPrefix +
		SanitizeIdentifier(schemaFilter) +
		a.config.labelSeparator
	return IntrospectRelationshipsQuery(prefix)
}

// ParseRemoteConstraints parses SHOW CONSTRAINTS YIELD * output into structured values.
//
// Input is []map[string]any from the driver's Record.AsMap() output.
// Handles version-dependent columns gracefully: propertyType (added in Neo4j 5.9+)
// produces an empty string when absent or null.
func ParseRemoteConstraints(records []map[string]any) ([]RemoteConstraint, error) {
	result := make([]RemoteConstraint, 0, len(records))
	for i, rec := range records {
		name := parseStringField(rec, "name")
		if name == "" {
			return nil, fmt.Errorf("record %d: missing constraint name", i)
		}

		result = append(result, RemoteConstraint{
			Name:            name,
			Type:            parseStringField(rec, "type"),
			EntityType:      parseStringField(rec, "entityType"),
			LabelsOrTypes:   parseStringSliceField(rec, "labelsOrTypes"),
			Properties:      parseStringSliceField(rec, "properties"),
			PropertyType:    parseStringField(rec, "propertyType"),
			CreateStatement: parseStringField(rec, "createStatement"),
		})
	}
	return result, nil
}

// ParseRemoteIndexes parses SHOW INDEXES output into structured values.
func ParseRemoteIndexes(records []map[string]any) ([]RemoteIndex, error) {
	result := make([]RemoteIndex, 0, len(records))
	for i, rec := range records {
		name := parseStringField(rec, "name")
		if name == "" {
			return nil, fmt.Errorf("record %d: missing index name", i)
		}

		result = append(result, RemoteIndex{
			Name:             name,
			Type:             parseStringField(rec, "type"),
			EntityType:       parseStringField(rec, "entityType"),
			LabelsOrTypes:    parseStringSliceField(rec, "labelsOrTypes"),
			Properties:       parseStringSliceField(rec, "properties"),
			Options:          parseMapField(rec, "options"),
			State:            parseStringField(rec, "state"),
			OwningConstraint: parseStringField(rec, "owningConstraint"),
		})
	}
	return result, nil
}

// ParseRemoteRelationships parses relationship signature query output.
// Expected record keys: "relType", "srcLabels", "tgtLabels".
func ParseRemoteRelationships(records []map[string]any) ([]RemoteRelationship, error) {
	result := make([]RemoteRelationship, 0, len(records))
	for i, rec := range records {
		relType := parseStringField(rec, "relType")
		if relType == "" {
			return nil, fmt.Errorf("record %d: missing relType", i)
		}

		result = append(result, RemoteRelationship{
			RelationType: relType,
			SourceLabels: parseStringSliceField(rec, "srcLabels"),
			TargetLabels: parseStringSliceField(rec, "tgtLabels"),
		})
	}
	return result, nil
}

// parseStringField extracts a string value from a record map.
// Returns "" if the key is missing, nil, or not a string.
func parseStringField(rec map[string]any, key string) string {
	v, ok := rec[key]
	if !ok || v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// parseStringSliceField extracts a []string from a record map.
// Neo4j driver returns []any for list values; this converts each element.
// Returns nil if the key is missing, nil, or not a list.
func parseStringSliceField(rec map[string]any, key string) []string {
	v, ok := rec[key]
	if !ok || v == nil {
		return nil
	}
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(list))
	for _, elem := range list {
		if s, ok := elem.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

// parseMapField extracts a map[string]any from a record map.
// Returns nil if the key is missing, nil, or not a map.
//
// The map is copied, like every other parsed field: the caller owns the record
// slice and may pool or rewrite it between introspection polls, and a retained
// reference would let that mutate the Options of every RemoteIndex already
// parsed — including copies already filed under Match or Drop.
//
// Nested maps are copied too. A shallow copy would leave the one map this
// package actually reads — the "indexConfig" holding a vector index's dimension
// and similarity — still aliased to the caller's record, which is precisely the
// value a later drift comparison depends on.
func parseMapField(rec map[string]any, key string) map[string]any {
	v, ok := rec[key]
	if !ok || v == nil {
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	return cloneNestedMap(m)
}

// cloneNestedMap deep-copies the map-valued entries of m. Values that are not
// maps are left as-is: they are strings, numbers, and booleans read out of a
// driver record, which callers do not mutate in place.
func cloneNestedMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = cloneValue(v)
	}
	return out
}

// cloneValue deep-copies the container shapes a driver record can hold. Index
// options genuinely contain lists — a POINT index reports its spatial bounds as
// lists of doubles, and a FULLTEXT index its analyzer settings — so copying only
// maps would leave those aliased to the caller's record, which is the aliasing
// [parseMapField] documents itself as preventing. Scalars are values already.
func cloneValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return cloneNestedMap(t)
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = cloneValue(e)
		}
		return out
	default:
		return v
	}
}

// intBits is the platform's int width, so the float64 range guard in [toInt]
// bounds against the type it converts to rather than assuming 64 bits.
const intBits = 32 << (^uint(0) >> 63)

// toInt coerces a driver-shaped numeric value to int. The Neo4j driver reports
// Cypher integers as int64; int and float64 are accepted defensively. Returns
// (0, false) for any other shape, and for a float64 that is not an exact
// integer or does not fit an int — a value that cannot be interpreted must read
// as unreadable rather than silently truncate, because the caller's next step is
// to compare it for drift and a truncated value would compare equal to a
// dimension the database does not actually have.
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int64:
		if int64(int(n)) != n {
			return 0, false
		}
		return int(n), true
	case int:
		return n, true
	case float64:
		// Bounded against the platform's actual int width. float64(math.MaxInt)
		// rounds UP to 2^(bits-1), so comparing with `n > math.MaxInt` lets
		// exactly that value through and then converts it out of range — the
		// silent truncation this guard exists to prevent. Ldexp gives the bound
		// exactly: -2^(bits-1) IS math.MinInt and is allowed; +2^(bits-1) is one
		// past math.MaxInt and is not.
		limit := math.Ldexp(1, intBits-1)
		if n != math.Trunc(n) || n < -limit || n >= limit {
			return 0, false
		}
		return int(n), true
	default:
		return 0, false
	}
}
