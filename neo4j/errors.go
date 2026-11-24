package neo4j

import (
	"errors"

	"github.com/simon-lentz/yammm/diag"
)

// E_NEO4J_INVALID_REMOTE_OBJECT indicates a remote row [Diff] cannot plan
// from: a constraint or an index with no name, two rows of one kind under one
// name, or a property type constraint that reports no property type. One
// [ShowConstraints] and [ShowIndexes] read holds none of them.
var E_NEO4J_INVALID_REMOTE_OBJECT = diag.NewCode("E_NEO4J_INVALID_REMOTE_OBJECT")

// Sentinel errors for write-path refusals.
var (
	// ErrUnsupportedListElem is the class of the refusal of a list whose
	// element Neo4j cannot store in a list, such as a nested collection.
	ErrUnsupportedListElem = errors.New("neo4j: unsupported list element type")

	// ErrUnrepresentable marks a value [NodeQueries],
	// [EdgeQueries] or [CoerceParams] refuses: one that is not in
	// the stored form of its kind, a nested collection's (beside
	// [ErrUnsupportedListElem]), an instant at a UTC offset past 18 hours, a map
	// at a property the schema declares, a list
	// or a map nested more than 256 levels deep, a number no driver number holds,
	// and, at [CoerceParams], a value the validator refuses at its constraint.
	// It is the class, matched with errors.Is; each message names the property,
	// the param or the key. It
	// separates a refusal of the data from a caller's own error, such as a
	// constraint [schema.CheckConstraint] refuses or a zero snapshot, which no
	// message text can.
	ErrUnrepresentable = errors.New("neo4j: the value cannot be stored as held")

	// ErrZeroSnapshot is the error [NodeQueries] and
	// [EdgeQueries] return for a zero
	// [github.com/simon-lentz/yammm/graph.Snapshot], which
	// carries no schema to derive a label or a key from.
	ErrZeroSnapshot = errors.New("neo4j: zero snapshot")
)
