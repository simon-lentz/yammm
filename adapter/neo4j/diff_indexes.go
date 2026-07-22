package neo4j

import (
	"fmt"
	"strings"
)

// IndexDiffResult is the four-way classification of desired vs actual indexes.
type IndexDiffResult struct {
	// Match contains indexes that exist in both schema and database with
	// identical semantic definitions.
	Match []IndexMatch

	// Drift contains indexes that exist in both by semantic key but have
	// different definitions (a vector index whose dimension or similarity
	// function differs).
	Drift []IndexDrift

	// Create contains indexes defined in the schema that are absent from the
	// database.
	Create []Index

	// Drop contains schema-owned indexes in the database that have no
	// corresponding declaration in the schema.
	Drop []RemoteIndex
}

// IndexMatch pairs a desired index with its matching actual index.
type IndexMatch struct {
	Desired Index
	Actual  RemoteIndex
}

// IndexDrift pairs a desired index with an actual index that has the same
// semantic key but a different definition.
type IndexDrift struct {
	Desired Index
	Actual  RemoteIndex
	Reason  string // Human-readable drift description
}

// DiffIndexes performs a semantic four-way diff between desired schema indexes
// and actual database indexes.
//
// Only indexes on labels owned by the given schema are considered. Matching is
// by semantic equivalence (same label, same properties in declared order, same
// kind), not by index name.
//
// Composite property order is significant: a same-set/different-order remote
// index classifies as Create + Drop, a deliberate divergence from
// [DiffConstraints], which sorts properties. SHOW INDEXES reports properties in
// index-definition order, so the order-sensitive key is sound.
//
// A schema-owned remote index with no declaration is reported as a Drop — the
// drift the index feature exists to surface. Drops are reported, never applied.
func (a *Adapter) DiffIndexes(
	desired []Index,
	actual []RemoteIndex,
	schemaName string,
) *IndexDiffResult {
	result := &IndexDiffResult{}

	// Build the schema label prefix for ownership filtering.
	labelPrefix := a.config.labelPrefix +
		SanitizeIdentifier(schemaName) +
		a.config.labelSeparator

	// Filter actual indexes to schema-owned only.
	var owned []RemoteIndex
	for _, ri := range actual {
		if isSchemaOwned(ri.LabelsOrTypes, labelPrefix) {
			owned = append(owned, ri)
		}
	}

	// Index actual indexes by semantic key.
	actualByKey := make(map[string]RemoteIndex, len(owned))
	for _, ri := range owned {
		actualByKey[remoteIndexKey(ri)] = ri
	}

	// Match desired against actual.
	matchedKeys := make(map[string]bool)
	for _, d := range desired {
		key := desiredIndexKey(d)
		ri, found := actualByKey[key]
		if !found {
			result.Create = append(result.Create, d)
			continue
		}
		matchedKeys[key] = true

		if reason, drifted := vectorDrift(d, ri); drifted {
			result.Drift = append(result.Drift, IndexDrift{
				Desired: d,
				Actual:  ri,
				Reason:  reason,
			})
			continue
		}

		result.Match = append(result.Match, IndexMatch{
			Desired: d,
			Actual:  ri,
		})
	}

	// Remaining unmatched actual indexes are drops.
	for _, ri := range owned {
		if !matchedKeys[remoteIndexKey(ri)] {
			result.Drop = append(result.Drop, ri)
		}
	}

	return result
}

// desiredIndexKey builds a lookup key from a desired Index.
// Format: "label|prop1,prop2|type". Properties are NOT sorted: composite index
// order is significant, so a reordered composite is a distinct index.
func desiredIndexKey(idx Index) string {
	return idx.Label + "|" + strings.Join(idx.Properties, ",") + "|" + indexKindToRemoteType(idx.Kind)
}

// remoteIndexKey builds a lookup key from a RemoteIndex, preserving the reported
// property order (SHOW INDEXES reports properties in index-definition order).
// Both key builders skip the property sort so composite order remains
// significant — mirroring [desiredIndexKey], not [remoteSemanticKey].
func remoteIndexKey(ri RemoteIndex) string {
	label := ""
	if len(ri.LabelsOrTypes) > 0 {
		label = ri.LabelsOrTypes[0]
	}
	return label + "|" + strings.Join(ri.Properties, ",") + "|" + ri.Type
}

// indexKindToRemoteType maps an IndexKind to the corresponding Neo4j remote
// index type string.
func indexKindToRemoteType(kind IndexKind) string {
	switch kind {
	case IndexRange:
		return "RANGE"
	case IndexVector:
		return "VECTOR"
	default:
		return "UNKNOWN"
	}
}

// vectorDrift reports whether a matched VECTOR index differs in configuration.
// If either side is missing config (an older server that does not report
// options), no drift is claimed.
func vectorDrift(d Index, ri RemoteIndex) (string, bool) {
	if d.Kind != IndexVector {
		return "", false
	}
	remoteDim, dimOK := ri.VectorDimensions()
	remoteSim, simOK := ri.VectorSimilarity()
	if !dimOK || !simOK {
		return "", false // Remote config unavailable; do not claim drift.
	}
	if d.VectorDimensions != remoteDim {
		return fmt.Sprintf("vector dimension mismatch: schema %d, database %d",
			d.VectorDimensions, remoteDim), true
	}
	if d.VectorSimilarity != remoteSim {
		return fmt.Sprintf("vector similarity mismatch: schema %s, database %s",
			d.VectorSimilarity, remoteSim), true
	}
	return "", false
}
