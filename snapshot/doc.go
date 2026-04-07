// Package snapshot provides serialization and deserialization of [graph.Snapshot]
// values to and from the yammm snapshot persistence format (.ys).
//
// The .ys format is a JSON-based persistence format that preserves full structural
// fidelity: instances with properties, primary keys, edges, compositions, provenance,
// duplicates, and unresolved edge records all survive a Marshal/Load round-trip.
//
// The format includes a schema structural hash for compatibility verification, an
// integrity hash for corruption detection, and a features array for forward
// compatibility.
//
// # Functions
//
// [Marshal] serializes a *graph.Snapshot to .ys bytes. Output is deterministic
// by default (no timestamp unless WithCreatedAt is used).
//
// [Load] deserializes .ys bytes back to a *graph.Snapshot, verifying structural
// integrity and schema compatibility.
//
// [Verify] validates a .ys file without materializing a Snapshot — useful for
// CI pipelines and pre-flight checks.
//
// [Info] reads summary metadata and statistics from a .ys file without loading
// the schema or materializing instance objects.
//
// # File extension
//
// The conventional file extension is .ys (yammm snapshot).
package snapshot
