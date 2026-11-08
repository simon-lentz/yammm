// Package jsondata reads the JSON instance documents the yammm CLI checks into
// [instance.RawInstance] values. A document is a JSON object keyed by type
// name, each key holding an array of instances:
//
//	{
//	  "Person": [{"id": "p1", "name": "Alice"}, ...],
//	  "Company": [{"id": "c1", "name": "Acme"}, ...]
//	}
//
// # Parsing
//
// [Parse] reads a document and returns its instances by type name.
// Input is preprocessed with [tidwall/jsonc], so comments and trailing commas
// are tolerated.
//
// Every valid type name whose value is an array is an entry of the result, an
// empty array included, unless a fault or a cancellation stops the parse
// before it is read. Each shape below, which a decoder resolves silently, is an
// Error diagnostic instead, because what a decoder keeps of it is one reader's
// convention and not the document's meaning. Each is reported where it stands:
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
//   - A null at a type name, which a decoder reads as a nil slice with no error.
//   - A null element of an instance array, which a decoder reads as a nil map
//     with no error.
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
// "[1,,]" is refused at its second comma. At a blank position, a "/" that no "/" or "*"
// follows is refused at the byte after it; inside a literal or a number, or
// after the root, it is refused at the "/" itself.
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
// [Parse] checks its context once per top-level key and returns the types read
// so far beside a Fatal E_CONTEXT_CANCELLED, which [diag.Result.HasFatal]
// reports.
//
// # Provenance
//
// Every instance carries a [location.Provenance]: the source the caller named,
// the instance's path in the document ($.Person[0]), and a point span at its
// opening brace. A parse diagnostic carries a span in that source wherever its
// offset lies in the data or at its end; the cancellation diagnostic carries none.
//
// The path indexes the document's own array, so $.Person[2] addresses the
// document even where element 1 failed to decode.
//
// Columns count runes from the line start of the bytes passed in, never of the
// buffer jsonc returns: jsonc writes one byte per comment BYTE, and one more
// after a "/*" that ends the input, so a multibyte rune inside a comment would
// move every later column on its line. A malformed
// UTF-8 sequence decomposes byte by byte, as it does for a schema source.
//
// A leading byte order mark is trimmed before decoding and its length added back
// to every byte offset, so it occupies column 1 of line 1 and every later
// position on line 1 sits one column further: positions are measured in the
// file the caller passed.
//
// # Type Tag Resolution
//
// Top-level keys are validated as type names. Unqualified type names resolve
// only to locally-defined types; imported types require alias-qualified form
// (alias.Type).
//
// # Byte Order Mark
//
// Parse skips one leading UTF-8 byte order mark and counts it in the
// positions it reports: one column on line 1, and three bytes in every byte
// offset. A second mark is a parse error, E_DATA_PARSE.
//
// # Thread Safety
//
// Parse keeps no state between calls, so concurrent calls are safe.
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
//	cmd/yammm/internal/jsondata  ──imports──▶  instance, diag, location, location/path,
//	                                           immutable, github.com/tidwall/jsonc
//
// [tidwall/jsonc]: https://github.com/tidwall/jsonc
package jsondata
