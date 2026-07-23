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

	// Unverified contains indexes that exist in both by semantic key but whose
	// full definition could not be compared because the database did not report
	// the configuration needed. They are neither confirmed in sync nor confirmed
	// drifted; folding them into Match would report an unchecked index as
	// verified.
	Unverified []IndexUnverified
}

// IndexUnverified pairs a desired index with an actual index whose configuration
// could not be read, so no in-sync claim is made about it.
type IndexUnverified struct {
	Desired Index
	Actual  RemoteIndex
	Reason  string // Human-readable description of what could not be verified
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

	// Filter actual indexes to those this schema both owns and could have
	// declared.
	var owned []RemoteIndex
	for _, ri := range actual {
		if isSchemaOwned(ri.LabelsOrTypes, labelPrefix) && declarableRemoteIndex(ri) {
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

		if reason, unverifiable := vectorConfigUnreadable(d, ri); unverifiable {
			result.Unverified = append(result.Unverified, IndexUnverified{
				Desired: d,
				Actual:  ri,
				Reason:  reason,
			})
			continue
		}

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

// declarableRemoteIndex reports whether a remote index is one the schema could
// have declared, and therefore one the diff owns.
//
// SHOW INDEXES reports every non-LOOKUP, non-constraint-backed index on a
// schema-owned label — including kinds the DSL has no vocabulary for (FULLTEXT,
// TEXT, POINT, and the Neo4j 4.x-historical BTREE) and relationship indexes,
// which the adapter never emits. Classifying one of those as an undeclared drop
// would report drift that no schema edit could resolve, leaving the
// all-or-nothing constraints-only mode as the only escape.
//
// An unreported entityType is treated as NODE: the field is absent only where a
// server or fixture does not report it, and every index the adapter emits is a
// node index.
func declarableRemoteIndex(ri RemoteIndex) bool {
	if ri.EntityType != "" && ri.EntityType != "NODE" {
		return false
	}
	for _, k := range allIndexKinds {
		if ri.Type == indexKindToRemoteType(k) {
			return true
		}
	}
	return false
}

// vectorConfigUnreadable reports whether a matched VECTOR index cannot be
// compared because the database reported no readable vector configuration — an
// older server that omits options, or a driver shape the options parser does not
// recognise.
//
// The semantic key matches on label, properties, and type alone, so a remote
// index of the wrong dimension matches a desired one by key; without this the
// pair would land in Match and report an unchecked index as in sync. It is a
// distinct outcome from drift (nothing was proven different) and from a failed
// introspection (there, nothing was compared at all).
func vectorConfigUnreadable(d Index, ri RemoteIndex) (string, bool) {
	if d.Kind != IndexVector {
		return "", false
	}
	_, dimOK := ri.VectorDimensions()
	_, simOK := ri.VectorSimilarity()
	if dimOK && simOK {
		return "", false
	}
	return "database reported no vector configuration; dimension and similarity not compared", true
}

// vectorDrift reports whether a matched VECTOR index differs in configuration.
// Callers must have established that the remote configuration is readable (see
// [vectorConfigUnreadable]); an unreadable one claims no drift here, because
// "unverifiable" is not "different".
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
	// Case-insensitive: the schema side is lowercase (validation forces it) while
	// Neo4j reports the similarity function uppercased ("COSINE"), so a
	// case-sensitive compare would flag every in-sync vector index as drift. The
	// two valid functions ("cosine", "euclidean") differ beyond case, so
	// EqualFold cannot conflate them.
	if !strings.EqualFold(d.VectorSimilarity, remoteSim) {
		return fmt.Sprintf("vector similarity mismatch: schema %s, database %s",
			d.VectorSimilarity, remoteSim), true
	}
	return "", false
}
