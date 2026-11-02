// Package snapparts carries a decoded .ys document from the snapshot package to
// the graph package, which alone can construct a graph.Snapshot. Neither the
// part types nor the constructor are public API: a consumer reaches a snapshot
// through graph.Graph.Add or snapshot.Load.
//
// # Dependencies
//
//	snapparts  ──imports──▶  immutable, location, schema
package snapparts

import (
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// Parts holds the pre-resolved data of one snapshot. Edges and records address
// instances by (type identity, key), never by record. Each root is filed under
// its own [Instance.TypeID], and Types may add a type no root holds.
type Parts struct {
	Types      []schema.TypeID
	Instances  []Instance
	Edges      []Edge
	Duplicates []Instance
	Unresolved []Unresolved

	// Attestation carries the header's claim verbatim, and is nil for a document
	// that carries none.
	Attestation *Attestation
}

// Attestation is the field set of graph.Attestation, which this package cannot
// import: graph imports this package.
type Attestation struct {
	Values       bool
	Associations bool
}

// Instance holds the data of one instance. Composed children keep the order
// given. The instance's name is rendered from TypeID under the schema.
type Instance struct {
	TypeID     schema.TypeID
	PrimaryKey immutable.Key
	Properties immutable.Properties
	Composed   map[string][]Instance
	Provenance location.Provenance
}

// Edge holds the data of one resolved edge. The target's type is the
// association's declared target.
type Edge struct {
	Relation   string
	SourceType schema.TypeID
	SourceKey  immutable.Key
	TargetKey  immutable.Key
	Properties immutable.Properties
}

// Unresolved holds the data of one unresolved edge record. The target's type
// and whether the association is required are read from the relation.
type Unresolved struct {
	SourceType schema.TypeID
	SourceKey  immutable.Key
	Relation   string
	TargetKey  immutable.Key
	Reason     string
	Properties immutable.Properties
}

// Hook is graph's constructor, set in graph's init. It returns a graph.Snapshot
// as any because this package cannot name it.
var Hook func(schema.Schema, Parts) any

// Rebuild calls [Hook]. S is graph.Snapshot at every call site, and importing
// graph is what guarantees Hook is set.
func Rebuild[S any](s schema.Schema, p Parts) S {
	return Hook(s, p).(S) //nolint:errcheck // graph's Hook returns the one S its caller names
}
