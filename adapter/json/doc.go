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
// Every constructor of a snapshot holds its structure to what [graph.Graph.Add]
// builds, so every relation this writer meets renders in a shape its own
// parser and the validator accept. A value can still have no JSON form: a
// non-finite float at any depth, which a validated Float never is, or a Go
// value encoding/json refuses, which only a bypass-built snapshot holds, is
// refused with an error marked [ErrUnrepresentable], so a caller separates it
// from an I/O failure without matching the message text.
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
// Every valid type name whose value is an array is an entry of the result, an
// empty array included, unless a fault or a cancellation stops the parse
// before it is read. Three
// shapes that a decoder would resolve silently are Error diagnostics instead,
// because what a decoder keeps of them is one reader's convention and not the
// document's meaning. Each is reported where it stands:
//
//   - A type name repeated as a key of the root object. Decoding keeps the last
//     array and drops the first batch whole; here the repeat is reported at its
//     key and both arrays' instances are in the entry, each indexing its own
//     array.
//   - A member name repeated inside one object, at any depth of an instance.
//     Names compare as the decoder reads them: escapes are resolved, and each
//     byte of an invalid UTF-8 sequence reads as U+FFFD, which is the key the
//     decoder keeps. Each repeat is reported where it stands, and the instance
//     is produced holding the value the decoder kept, the last one the object
//     states.
//   - Anything but white space and closed comments after the root object,
//     reported where it starts, whether or not it reads as a JSON value. An
//     unterminated block comment is content.
//
// A fault the decoder cannot read past, such as a syntax error or a truncated
// document, is reported and parsing stops there: every instance read before it
// is returned, and nothing after it is read. "Where a fault is placed" below
// names the byte the diagnostic points at.
//
// # Where a fault is placed
//
// A fault the decoder cannot read past is placed at the first byte of the
// document from which no continuation gives a document the parser reads
// without such a fault. A document that ends before any such byte, inside a
// comment included, is placed at its end.
//
// The bytes are read as jsonc and the decoder read them together: JSON as
// encoding/json reads it, where a string may hold any byte of an invalid UTF-8
// sequence, and jsonc's comments wherever white space may stand. jsonc blanks
// a comma that a closing delimiter follows, so "[," can still close and
// "[1,,]" is refused at its second comma. A "/" that no "/" or "*" follows is
// refused at the byte after it.
//
// The nesting limit is 10,000 levels, counted as the running implementation of
// encoding/json counts it. Its own implementation counts within each value it
// decodes whole: an element of a type's array, or the value of a refused type
// name. Its v2 implementation, the default from Go 1.27, counts from the root.
//
// The diagnostic's detail keeps the decoder's own message, which can name
// another byte.
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
// opening brace. A parse diagnostic carries a span in that source wherever its
// offset lies inside the data; the cancellation diagnostic carries none.
//
// The path indexes the document's own array, so $.Person[2] addresses the
// document even where element 1 failed to decode. [adapter/csv] indexes by the
// instances it produced instead, because CSV has no path language.
//
// Columns count runes from the line start of the bytes passed in, never of the
// buffer jsonc returns: jsonc writes one byte per comment BYTE, and one more
// after a "/*" that ends the input, so a multibyte rune inside a comment would
// move every later column on its line. A malformed
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
// An [Adapter] holds no state, so one value serves concurrent calls.
//
// # Numeric Precision
//
// JSON numbers are read by lexical form: a literal with '.', 'e' or 'E' is a
// float64 when a finite float64 holds it, and an integer literal is an int64.
// An Integer property takes an integer literal alone: 5.0 and 1e2 are floats,
// which validation refuses there (E_TYPE_MISMATCH). A Float property takes any
// number, read as its nearest float64. A literal no finite float64 holds, such
// as 1e400, stays a json.Number that validation refuses at either kind. An
// integer literal outside the int64 range keeps its exact text as a
// json.Number: validation refuses it at an Integer property and reads it as its
// nearest float64 at a Float property, refusing it there only when no finite
// float64 holds it. The literal -0 stays a json.Number too, so a Float reads
// it as the float64 -0. A float64 holds integers exactly only up to 2^53.
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
