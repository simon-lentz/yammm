// Package json provides a JSON adapter for parsing instance data into
// [instance.RawInstance] values with optional source location tracking,
// and for serializing [graph.Result] snapshots back to JSON.
//
// # Serialization
//
// The adapter can serialize a completed graph to JSON using [Adapter.MarshalObject]
// or [Adapter.WriteObject]. The output format groups instances by type name:
//
//	{
//	  "Person": [{"id": "p1", "name": "Alice"}, ...],
//	  "Company": [{"id": "c1", "name": "Acme"}, ...]
//	}
//
// Instances include:
//   - All validated properties
//   - Foreign key references for resolved associations (as inline key arrays)
//   - Composed children (nested inline)
//
// Use [WithIndent] for pretty-printed output and [WithDiagnostics] to include
// unresolved edges and duplicates in a "$diagnostics" section.
//
// # Parsing Modes
//
// The adapter supports two parsing modes controlled by the WithStrictJSON option:
//
//   - WithStrictJSON(true) — Uses encoding/json directly. Disables jsonc preprocessing.
//     Comments and trailing commas are parse errors.
//
//   - WithStrictJSON(false) (default) — Uses [tidwall/jsonc] as a preprocessor to
//     strip comments and trailing commas while preserving byte offsets. This
//     enables human-friendly JSON with accurate diagnostic locations.
//
// # Location Tracking
//
// When WithTrackLocations(true) is set, the adapter captures source positions for
// parsed elements using [json.Decoder.InputOffset]. These byte offsets are
// converted to line/column positions via the [location.PositionRegistry]
// interface (typically implemented by schema.SourceRegistry).
//
// The adapter is a read-only consumer of the registry's PositionAt method.
// To include JSON source excerpts in diagnostics, register the JSON content
// with the registry before parsing.
//
// # jsonc Offset Preservation
//
// The adapter relies on tidwall/jsonc preserving exact input length during
// preprocessing. This invariant is critical for accurate diagnostics:
//
//   - len(jsonc.ToJSON(input)) == len(input) — always true
//   - Newlines within block comments are preserved at the same byte offsets
//   - Byte offset N in preprocessed output maps to byte offset N in original
//
// These invariants are verified by the internal/jsonc_test.go test suite,
// which serves as a regression guard for jsonc dependency upgrades.
//
// # Type Tag Resolution
//
// The adapter recognizes $type fields for type routing. Unqualified type names
// resolve only to locally-defined types; imported types require alias-qualified
// form (alias.Type).
//
// # Thread Safety
//
// The Adapter type is safe for concurrent Parse* calls after construction.
// No shared mutable state exists; all context flows through parameters.
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
// This package imports github.com/tidwall/jsonc unconditionally. The WithStrictJSON
// option controls runtime behavior, not module dependencies:
//
//	WithStrictJSON(true)  — Uses encoding/json directly (jsonc not invoked)
//	WithStrictJSON(false) — Uses jsonc.ToJSON() preprocessor before stdlib parsing
//
// Consumers who import this package will have jsonc in their dependency graph
// regardless of WithStrictJSON setting.
//
// [tidwall/jsonc]: https://github.com/tidwall/jsonc
package json
