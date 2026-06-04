package neo4j

import "fmt"

// RemoteConstraint represents a constraint as reported by SHOW CONSTRAINTS YIELD *.
//
// Parse from database output via [ParseRemoteConstraints].
type RemoteConstraint struct {
	Name            string   // Constraint name
	Type            string   // e.g., "UNIQUENESS", "NODE_PROPERTY_EXISTENCE", "NODE_PROPERTY_TYPE", "NODE_KEY"
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

// RemoteIndex represents a non-constraint-backing index as reported by SHOW INDEXES.
//
// Parse from database output via [ParseRemoteIndexes].
type RemoteIndex struct {
	Name          string   // Index name
	Type          string   // "BTREE", "RANGE", "TEXT", "POINT", "FULLTEXT"
	EntityType    string   // "NODE" or "RELATIONSHIP"
	LabelsOrTypes []string // Labels or relationship types
	Properties    []string // Indexed properties
}

// IntrospectConstraintsQuery returns the Cypher query for fetching all constraints.
// Execute this query against a Neo4j database and pass the results to
// [ParseRemoteConstraints].
func IntrospectConstraintsQuery() string {
	return "SHOW CONSTRAINTS YIELD *"
}

// IntrospectIndexesQuery returns the Cypher query for fetching non-constraint-backing indexes.
// Filters out LOOKUP indexes and indexes owned by constraints.
func IntrospectIndexesQuery() string {
	return "SHOW INDEXES YIELD name, type, entityType, labelsOrTypes, properties, owningConstraint " +
		"WHERE owningConstraint IS NULL AND type <> 'LOOKUP'"
}

// IntrospectRelationshipsQuery returns a Cypher query for discovering relationship
// signatures. The labelPrefix parameter scopes the query to schema-owned labels
// for efficiency.
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
// + labelSeparator) and delegates to [IntrospectRelationshipsQuery].
//
// When schemaFilter is empty, all relationships are discovered (no filtering).
func (a *Adapter) IntrospectRelationshipsQueryFor(schemaFilter string) (string, map[string]any) {
	if schemaFilter == "" {
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
			Name:          name,
			Type:          parseStringField(rec, "type"),
			EntityType:    parseStringField(rec, "entityType"),
			LabelsOrTypes: parseStringSliceField(rec, "labelsOrTypes"),
			Properties:    parseStringSliceField(rec, "properties"),
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
