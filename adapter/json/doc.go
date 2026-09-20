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
// [graph.RebuildSnapshot] reconstructs a document and does not validate one,
// so a .ys document can carry shapes this writer cannot render as an object its
// own parser and the validator accept: an edge whose target key has another
// arity than its target type's, a (one) association carrying several edges, an
// edge to a type its association does not declare, and an edge or composed
// children under a name the type declares no relation of that kind for. Each
// is refused with an error marked [ErrUnrepresentable], so a caller separates
// it from an encoding failure and from an I/O failure without matching the
// message text.
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
// Every type name whose value is an array is an entry of the result, an empty
// array included, unless it is read after a fault that stops the parse. Three
// shapes that a decoder would resolve silently are Error diagnostics instead.
// Each is reported and nothing the document states is dropped, because what a
// decoder keeps of them is one reader's convention and not the document's
// meaning:
//
//   - A type name repeated as a key of the root object. Decoding keeps the last
//     array and drops the first batch whole; here the repeat is reported at its
//     key and both arrays' instances are in the entry, each indexing its own
//     array.
//   - A member name repeated inside one object, at any depth of an instance.
//     Names compare as the decoder reads them: escapes are resolved, and each
//     byte of an invalid UTF-8 sequence reads as U+FFFD, which is the key the
//     decoder keeps. Each repeat is reported where it stands, and the instance
//     is produced holding the value the decoder kept, which is the value every
//     other JSON reader keeps.
//   - Anything but white space and closed comments after the root object,
//     reported where it starts, whether or not it reads as a JSON value. An
//     unterminated block comment is content.
//
// A fault the decoder cannot read past, such as a syntax error or a truncated
// document, is reported and parsing stops there: every instance read before it
// is returned, and nothing after it is read.
//
// # Cancellation
//
// Every entry point observes its context at each unit of work it has.
// [Adapter.ParseObject] checks once per
// top-level key and returns the types read so far beside a Fatal
// E_CONTEXT_CANCELLED, Fatal because [diag.Result.HasFatal] is documented to
// mean the run did not finish. [Adapter.MarshalObject] checks once per type
// group and returns an error wrapping the context's; [Adapter.WriteObject]
// checks again after the document is built and writes nothing if it is
// cancelled by then. The CSV adapter's writers are finer, checking per
// instance, so one very large type group stops sooner there than here.
//
// # Provenance
//
// Every instance carries a [location.Provenance]: the source the caller named,
// the instance's path in the document ($.Person[0]), and a point span at its
// opening brace. Every parse diagnostic carries a span in that source too.
//
// The path indexes the document's own array, so $.Person[2] addresses the
// document even where element 1 failed to decode. [adapter/csv] indexes by the
// instances it produced instead, because CSV has no path language.
//
// Columns count runes from the line start of the bytes passed in, never of the
// buffer jsonc returns: jsonc writes one space per comment BYTE, so a multibyte
// rune inside a comment would move every later column on its line. A malformed
// UTF-8 sequence decomposes byte by byte, as it does for a schema source.
//
// A leading byte order mark is trimmed before decoding and its length added back
// to every offset, so it occupies column 1 and every position after it sits one
// column further: positions are measured in the file the caller passed.
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
//	adapter/json  ──imports──▶  instance, diag, location, location/path, graph,
//	                            immutable, schema, adapter/internal/constraintof,
//	                            adapter/internal/refusal, adapter/internal/typetag,
//	                            github.com/tidwall/jsonc
//
// [tidwall/jsonc]: https://github.com/tidwall/jsonc
package json
