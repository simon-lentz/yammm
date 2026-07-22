package neo4j

import (
	"fmt"
	"slices"
	"strings"
)

// ConstraintDiffResult is the four-way classification of desired vs actual constraints.
type ConstraintDiffResult struct {
	// Match contains constraints that exist in both schema and database with
	// identical semantic definitions.
	Match []ConstraintMatch

	// Drift contains constraints that exist in both by semantic key but have
	// different definitions (e.g., type expression mismatch).
	Drift []ConstraintDrift

	// Create contains constraints defined in the schema that are absent from
	// the database.
	Create []Constraint

	// Drop contains schema-owned constraints in the database that have no
	// corresponding definition in the schema.
	Drop []RemoteConstraint
}

// ConstraintMatch pairs a desired constraint with its matching actual constraint.
type ConstraintMatch struct {
	Desired Constraint
	Actual  RemoteConstraint
}

// ConstraintDrift pairs a desired constraint with an actual constraint that
// has the same semantic key but a different definition.
type ConstraintDrift struct {
	Desired Constraint
	Actual  RemoteConstraint
	Reason  string // Human-readable drift description
}

// DiffConstraints performs a semantic four-way diff between desired schema
// constraints and actual database constraints.
//
// Only constraints on labels owned by the given schema are considered.
// Schema ownership is determined by the adapter's label convention:
// labels starting with the schema name prefix (built from schemaName,
// the adapter's label separator, and label prefix).
//
// Matching is by semantic equivalence (same label, same properties, same kind),
// not by constraint name.
func (a *Adapter) DiffConstraints(
	desired []Constraint,
	actual []RemoteConstraint,
	schemaName string,
) *ConstraintDiffResult {
	result := &ConstraintDiffResult{}

	// Build the schema label prefix for ownership filtering.
	labelPrefix := a.config.labelPrefix +
		SanitizeIdentifier(schemaName) +
		a.config.labelSeparator

	// Filter actual constraints to schema-owned only.
	var owned []RemoteConstraint
	for _, rc := range actual {
		if isSchemaOwned(rc.LabelsOrTypes, labelPrefix) {
			owned = append(owned, rc)
		}
	}

	// Index actual constraints by semantic key.
	actualByKey := make(map[string]RemoteConstraint, len(owned))
	for _, rc := range owned {
		key := remoteSemanticKey(rc)
		actualByKey[key] = rc
	}

	// Match desired against actual.
	matchedKeys := make(map[string]bool)
	for _, d := range desired {
		key := desiredSemanticKey(d)
		rc, found := actualByKey[key]
		if !found {
			result.Create = append(result.Create, d)
			continue
		}
		matchedKeys[key] = true

		// Check for drift (type expression mismatch on TYPE constraints).
		if d.Kind == ConstraintType && d.TypeExpr != "" && rc.PropertyType != "" {
			if d.TypeExpr != rc.PropertyType {
				result.Drift = append(result.Drift, ConstraintDrift{
					Desired: d,
					Actual:  rc,
					Reason:  fmt.Sprintf("property type mismatch: schema %s, database %s", d.TypeExpr, rc.PropertyType),
				})
				continue
			}
		}

		result.Match = append(result.Match, ConstraintMatch{
			Desired: d,
			Actual:  rc,
		})
	}

	// Remaining unmatched actual constraints are drops.
	for _, rc := range owned {
		key := remoteSemanticKey(rc)
		if !matchedKeys[key] {
			result.Drop = append(result.Drop, rc)
		}
	}

	return result
}

// isSchemaOwned reports whether any of the given labels starts with the schema
// label prefix. Shared by the constraint ([DiffConstraints]) and index
// ([DiffIndexes]) diff paths.
func isSchemaOwned(labels []string, labelPrefix string) bool {
	for _, label := range labels {
		if strings.HasPrefix(label, labelPrefix) {
			return true
		}
	}
	return false
}

// desiredSemanticKey builds a lookup key from a desired Constraint.
// Format: "label|prop1,prop2|kind"
func desiredSemanticKey(c Constraint) string {
	props := slices.Clone(c.Properties)
	slices.Sort(props)
	return c.Label + "|" + strings.Join(props, ",") + "|" + constraintKindToRemoteType(c.Kind)
}

// remoteSemanticKey builds a lookup key from a RemoteConstraint.
// Format: "label|prop1,prop2|type"
func remoteSemanticKey(rc RemoteConstraint) string {
	label := ""
	if len(rc.LabelsOrTypes) > 0 {
		label = rc.LabelsOrTypes[0]
	}
	props := slices.Clone(rc.Properties)
	slices.Sort(props)
	return label + "|" + strings.Join(props, ",") + "|" + rc.Type
}

// constraintKindToRemoteType maps a ConstraintKind to the corresponding
// Neo4j remote constraint type string.
func constraintKindToRemoteType(kind ConstraintKind) string {
	switch kind {
	case ConstraintUnique:
		return "UNIQUENESS"
	case ConstraintNotNull:
		return "NODE_PROPERTY_EXISTENCE"
	case ConstraintType:
		return "NODE_PROPERTY_TYPE"
	case ConstraintNodeKey:
		return "NODE_KEY"
	default:
		return "UNKNOWN"
	}
}
