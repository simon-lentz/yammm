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
// Use [WithIndent] for pretty-printed output. The object shape keys instances
// by the name the entry schema addresses each root type by, and two such names
// never collide: every snapshot root is a type the entry schema can name, so a
// local type renders bare and an imported one alias-qualified. The graph
// package doc's "Root type eligibility" section states the rule the
// constructors hold.
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
// # Byte Order Mark
//
// ParseObject skips one leading UTF-8 byte order mark, as the CSV adapter's
// parse methods do. A second mark is a parse error.
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
// # Dependencies
//
//	adapter/json  ──imports──▶  instance, diag, location, graph, immutable, schema,
//	                            adapter/json/internal/typetag, github.com/tidwall/jsonc
//
// [tidwall/jsonc]: https://github.com/tidwall/jsonc
package json
