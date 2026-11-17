package neo4j

import (
	"fmt"
	"math"
	"strings"
)

// RemoteConstraint represents a constraint as reported by [ShowConstraints].
//
// Parse from database output via [ParseConstraints].
type RemoteConstraint struct {
	Name string // Constraint name

	// Type is verbatim what the server reported. Its spelling for a uniqueness
	// constraint FOLLOWS THE CYPHER LANGUAGE the query runs under, not the
	// edition: CYPHER 5, the one language of Neo4j 5.x, reports "UNIQUENESS"
	// and "RELATIONSHIP_UNIQUENESS", and CYPHER 25, the default of a new database
	// on 2026.05, "NODE_PROPERTY_UNIQUENESS" and "RELATIONSHIP_PROPERTY_UNIQUENESS".
	// [ShowConstraints] names no language, so the database's default
	// decides. "NODE_PROPERTY_EXISTENCE", "NODE_PROPERTY_TYPE" and "NODE_KEY"
	// are the same under both.
	//
	// A consumer switching on this field must accept both spellings of each
	// uniqueness kind, or it silently stops recognising every UNIQUE constraint
	// when the database's default language changes. The diff folds the node
	// spellings internally; this field is deliberately not folded, so a
	// diagnostic echoes the kind the operator's own database reports.
	Type string

	EntityType      string   // "NODE" or "RELATIONSHIP"
	LabelsOrTypes   []string // Labels or relationship types
	Properties      []string // Constrained properties
	PropertyType    string   // Enforced type of a property type constraint; empty for every other kind
	CreateStatement string   // Cypher to recreate the constraint
}

// RemoteIndex represents an index as reported by SHOW INDEXES, including the
// index backing a constraint — see [RemoteIndex.OwningConstraint].
//
// Parse from database output via [ParseIndexes].
type RemoteIndex struct {
	Name          string         // Index name
	Type          string         // "RANGE", "TEXT", "POINT", "FULLTEXT", "VECTOR", "LOOKUP" (BTREE is Neo4j 4.x-historical)
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
	// [ShowIndexes] yields these rows, and [Diff] needs
	// them: a backing index holds its constraint's name against every CREATE
	// INDEX in the database, and already serves the (label, properties, kind) it
	// covers, so both are conditions under which the server silently no-ops a
	// declared index. It is this field that keeps them out of pairing and drops —
	// a backing index is created and dropped with its constraint and is never
	// independently declarable, so reporting one as an undeclared orphan advises
	// dropping the index behind a primary key. One that serves a declaration
	// pairing left without a row is that declaration's Actual, or, where it is
	// FAILED, its drift's first blocker; a declaration that realises its
	// constraint then drifts too.
	// Where the plan drops the constraint of an online or POPULATING one, the
	// declaration is a Create unless another blocker makes it a Drift.
	//
	// A consumer filtering rows itself must test this field rather than assume
	// the query excluded them.
	OwningConstraint string
}

// IsOnline reports whether the index is in a state that serves queries. An
// unreported state is treated as online: [ShowIndexes] always reports it, so
// only a hand-built record or a caller's own projection omits it.
func (ri RemoteIndex) IsOnline() bool {
	return ri.State == "" || strings.EqualFold(ri.State, "ONLINE")
}

// VectorDimensions returns the configured dimension of a VECTOR index and true,
// or (0, false) when the index reports none it can read: a non-vector index, a
// VECTOR index created without vector.dimensions, which every supported server
// accepts and reports with no dimension, a dimension that is no whole number in
// int's range, or a record with no readable options map.
func (ri RemoteIndex) VectorDimensions() (int, bool) {
	cfg, ok := ri.indexConfig()
	if !ok {
		return 0, false
	}
	return toInt(cfg["vector.dimensions"])
}

// VectorSimilarity returns the configured similarity function of a VECTOR index
// and true, or ("", false) when the index reports none: a record with no
// readable options map, or one whose configuration holds no
// vector.similarity_function string.
//
// The value is the server's, VERBATIM and in the server's casing: Neo4j
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

// ShowConstraints is the Cypher query for fetching all constraints.
// Execute this query against a Neo4j database and pass the results to
// [ParseConstraints]. It names every column the diff reads, as [ShowIndexes]
// does; every server from the supported floor, Neo4j 5.26, reports each one.
const ShowConstraints = "SHOW CONSTRAINTS YIELD name, type, entityType, labelsOrTypes, properties, propertyType, createStatement"

// ShowIndexes is the Cypher query for fetching every index. Execute it against
// a Neo4j database and pass the results to [ParseIndexes].
//
// LOOKUP indexes are INCLUDED although no schema declares one. A LOOKUP index
// can hold any name, and an index's name blocks a create whatever its kind:
// the server refuses a constraint over it and takes no effect from an index
// over it. [RemoteIndex.Declarable] refuses them, so they only block names.
//
// Constraint-backing indexes are deliberately INCLUDED. They are never paired by
// name or dropped — [RemoteIndex.Declarable] rejects them on
// [RemoteIndex.OwningConstraint] — and one that serves a declaration pairing
// left without a row is that declaration's Actual, or, where it is FAILED, its
// drift's first blocker; a declaration that realises its constraint then
// drifts too. Where the
// plan drops the constraint of an online or POPULATING one, the declaration is
// a Create unless another blocker makes it a Drift. A CREATE INDEX whose name
// a backing index holds takes no effect, so that backing index blocks it, and
// neither does one whose (label, properties, kind) a backing index already
// serves. Excluding them here would hide both, and such a declaration would
// report as a Create that takes no effect on every run.
//
// Yields the options map so vector configuration is visible, and the state so an
// index that exists but does not serve queries is distinguishable from a healthy
// one.
const ShowIndexes = "SHOW INDEXES YIELD name, type, entityType, labelsOrTypes, properties, options, state, owningConstraint"

// ParseConstraints parses [ShowConstraints] output, each record from the
// driver's Record.AsMap(), into structured values. It refuses a record with no
// name, and a property type constraint's record with no propertyType, which a
// server returns only to a projection that does not yield it: the type it
// enforces could not be compared.
func ParseConstraints(records []map[string]any) ([]RemoteConstraint, error) {
	result := make([]RemoteConstraint, 0, len(records))
	for i, rec := range records {
		name := parseStringField(rec, "name")
		if name == "" {
			return nil, fmt.Errorf("record %d: missing constraint name", i)
		}
		rc := RemoteConstraint{
			Name:            name,
			Type:            parseStringField(rec, "type"),
			EntityType:      parseStringField(rec, "entityType"),
			LabelsOrTypes:   parseStringSliceField(rec, "labelsOrTypes"),
			Properties:      parseStringSliceField(rec, "properties"),
			PropertyType:    parseStringField(rec, "propertyType"),
			CreateStatement: parseStringField(rec, "createStatement"),
		}
		if reportsNoPropertyType(rc) {
			return nil, fmt.Errorf("record %d: property type constraint %q reports no propertyType", i, name)
		}
		result = append(result, rc)
	}
	return result, nil
}

// ParseIndexes parses SHOW INDEXES output into structured values.
func ParseIndexes(records []map[string]any) ([]RemoteIndex, error) {
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
