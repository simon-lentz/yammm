// Package json provides a JSON adapter for parsing instance data into
// [instance.RawInstance] values and for serializing [graph.Snapshot]
// snapshots back to JSON.
//
// # Serialization
//
// The adapter serializes a completed graph to JSON using [Adapter.MarshalObject]
// or [Adapter.WriteObject]. The output format groups instances by type name:
//
//	{
//	  "Person": [{"id": "p1", "name": "Alice"}, ...],
//	  "Company": [{"id": "c1", "name": "Acme"}, ...]
//	}
//
// Instances include:
//   - All validated properties
//   - Association targets as _target_-keyed objects carrying their edge
//     properties — the same shape [Adapter.ParseObject] accepts
//   - Composed children as arrays, (one) compositions included
//
// The output of a fully resolved snapshot round-trips: ParseObject plus the
// validator accept every shape this writer emits. Unresolved edges are not
// written; persist them in the .ys format when they must survive.
//
// Use [WithIndent] for pretty-printed output. Both entry points refuse a
// snapshot in which two type identities render the same output name (two
// same-named types from different schemas, or a transitively imported type
// rendering bare): the object shape keys instances by rendered name, so such
// a snapshot cannot be written without silently merging types.
//
// # Parsing
//
// [Adapter.ParseObject] parses a JSON object keyed by type name, each key
// holding an array of instances, and returns [instance.RawInstance] values.
// Input is preprocessed with [tidwall/jsonc], so comments and trailing commas
// are tolerated.
//
// # Type Tag Resolution
//
// Top-level keys are validated as type names. Unqualified type names resolve
// only to locally-defined types; imported types require alias-qualified form
// (alias.Type).
//
// # Thread Safety
//
// The Adapter type is safe for concurrent use after construction. No shared
// mutable state exists; all context flows through parameters.
//
// # Numeric Precision
//
// JSON numbers are parsed as int64 when possible, otherwise float64. This follows
// standard JSON semantics (RFC 8259). Large integers exceeding int64 range
// (> 9,223,372,036,854,775,807) will fall back to float64, which loses precision
// for values exceeding 2^53. This is inherent to JSON and not specific to this
// adapter.
//
// [tidwall/jsonc]: https://github.com/tidwall/jsonc
package json
