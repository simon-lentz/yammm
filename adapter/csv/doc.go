// Package csv provides a CSV adapter for parsing delimited data into
// [instance.RawInstance] values and serializing validated instances to CSV.
//
// # Delimiter and Header
//
// Fields split on ',' unless [WithDelimiter] names another delimiter; a TSV
// file takes '\t'. The parse side and the write side read one delimiter, so an
// adapter reads back the file it writes. [encoding/csv]'s quoting holds under
// every delimiter: a cell that starts with '"' is a quoted field, and the writer
// quotes a cell holding the delimiter, a quote or a line break, or starting
// with white space. A TSV file is
// therefore CSV with tabs, not the quote-free IANA text/tab-separated-values
// format. The first row is always the header,
// because its column names are what map a cell to a property: the package has
// no headerless mode.
//
// # Diagnostic Codes
//
// Three codes divide every fault this package reports, by what the caller must
// do about it. A cell whose text does not coerce to the type its member
// declares draws [E_CSV_COERCE]: the data needs cleaning, the cell keeps its
// text and the row is still produced. A setting the adapter cannot use draws
// [E_CSV_CONFIG] — a list separator the parser could not find again, a
// delimiter [encoding/csv] refuses, [Adapter.ParseWithTypeColumn] with no
// [WithTypeColumn] — and no record is read. Everything else is the input not
// being well formed, or the input failing to arrive at all, and draws
// [diag.E_ADAPTER_PARSE] — the code the JSON adapter reports a malformed
// document under, so one code answers that question for both data parsers. A
// failing [io.Reader] takes it too, at Fatal, since severity is what says the
// run did not finish. A type-column value that is not a type name draws
// E_INVALID_TYPE_TAG and a cancelled parse E_CONTEXT_CANCELLED, as they do
// there.
//
// The line falls on what the caller must change, not on who is at fault. A
// type column the header does not name is [diag.E_ADAPTER_PARSE] and not
// [E_CSV_CONFIG], although a caller chose the name: the file can be given the
// column, so the input is what does not match. [E_CSV_CONFIG] is for a setting
// no file could satisfy.
//
// # Header and Records
//
// The header must name every column, and name each one once: a header with an
// unnamed column or a name that repeats is refused at the header's line, with
// one diagnostic for each unnamed column and each repeated name, and no record
// is read. A name is matched as written; it is never trimmed.
//
// A record the reader refuses — one whose field count differs from the
// header's, a bare quote in an unquoted field, a character after a quoted
// field's closing quote — is reported as an Error at the line the fault is on,
// and the parse goes on with the next record. A quoted field that is never
// closed runs to the end of the input, so its record is the last one
// reported. [encoding/csv]'s LazyQuotes is off, so a malformed quoted field is
// a diagnostic, never a value the file did not state.
//
// Any other error the reader returns is the [io.Reader] failing, an error that
// only wraps [io.EOF] included. It stops the parse with a Fatal diagnostic, as
// the diag package documents for an I/O failure, and the records read before it
// are kept. A header the reader cannot read is an Error for a fault in the
// input or a delimiter [encoding/csv] refuses, and a Fatal for the reader
// failing.
//
// # Column Mapping
//
// A column maps to the property whose name it spells. A CSV header row
//
//	id,name,age,created_at
//
// produces property maps with keys "id", "name", "age", "created_at".
//
// Association edges use dotted columns: one per foreign-key component
// (<field>._target_<pk>) and one per edge property (<field>.<prop>). A
// (many) association zips its targets across the group on the list
// separator. No declared name can contain a dot or a leading underscore,
// so the grammar is unambiguous; with [WithTypeColumn], the type column is
// extracted before dotted classification.
//
// The parser owns the CSV — its quoting, its records, its header and the
// coercion of each cell — and the validator owns every name, as it does for
// the JSON adapter. So a row parses as the JSON object that holds the same keys
// and values does, and the validator answers the two alike.
//
// A column name resolves by the rule [instance.Validator] applies to a JSON
// object's keys, applied to the object a row makes: each row, and each target
// of an edge group, is its own object, and a key it holds is one whose cell
// the parser writes. The rule holds at four places: a property, an
// association's field, an edge property and a key component. A name that
// spells a member exactly claims it when the object holds it. Otherwise a name
// whose [instance.FoldKey] matches the member resolves to it, when it is the
// member's one such name the object holds, or the member's one candidate in the
// header. The resolved member decides the cell's coercion, what an empty cell
// holds and how an edge group assembles. Every key keeps the header's spelling.
//
// [WithStrictPropertyNames] matches names exactly instead, as
// [instance.WithStrictPropertyNames] makes the validator match them. Give the
// parser the validator's setting: [instance.RecommendedOptions] sets it. A
// parser that folds for a strict validator writes a mis-cased column's empty
// value under the header's spelling, so the row draws the validator's unknown
// field where the file means no value.
//
// A name the object holds that resolves to no member is carried as its text, as
// a JSON key is, for the validator to report as unknown, shadowed or colliding,
// or to ignore under [instance.WithAllowUnknownFields]; its empty cell is
// skipped. That holds at every position: a column naming no property, a suffix
// naming neither a key component nor an edge property, a key component the
// target does not declare, and a dotted field naming no association, whose
// cells are carried as one object under the field's spelling.
//
// Two values for one key are refused, as a JSON object that repeats a member
// is: where a plain column and the dotted columns of the same field both write
// a value in one row, the row draws [diag.E_ADAPTER_PARSE] naming both — the
// code that adapter reports its repeated member under — and the group is kept,
// since it is the field's only valid form.
//
// Each row type of a [WithTypeColumn] file reads the header against its own
// members, once per type. A type-column value must be a type name by the
// grammar's rule, which the JSON adapter applies to a document's top-level
// keys; one that is not, the empty one included, draws E_INVALID_TYPE_TAG, and
// its record is skipped.
//
// # Type Coercion
//
// CSV values are strings. The adapter uses [*schema.Type] constraint metadata
// to coerce string values to typed Go values during parsing:
//
//   - Integer properties: [strconv.ParseInt]
//   - Float properties: [strconv.ParseFloat]
//   - Boolean properties: [strconv.ParseBool], every spelling it accepts
//   - Date properties: validated as "2006-01-02" format, kept as string
//   - Timestamp properties: validated against the declared layout — the
//     validator's own rule — or RFC 3339 for the default layout; kept as
//     string
//   - List properties: split by the list separator, each element coerced
//     by the element constraint, a nested list by this same rule
//
// Date and Timestamp values remain as strings in [instance.RawInstance],
// matching the JSON adapter's behavior. Temporal coercion to driver types
// happens downstream in write adapters.
//
// The write side renders Timestamp, Date and UUID through their constraint, so
// a cell carries the text the validator stores — including foreign-key columns,
// whose components render through the TARGET type's primary-key constraints,
// and list elements, which render through the element constraint. A value the
// constraint cannot render is written as it arrived: an export returns an error
// rather than a diag.Result, so one malformed cell must not fail the file.
//
// # List Properties
//
// List values use the list separator — "|" by default, configurable with
// [WithListSeparator]. Both sides escape through one shared helper pair: the
// backslash escapes itself and every occurrence of the separator's first
// byte, so the separator is found only between elements, and an element
// holding any part of it splits back unchanged. A separator the parser
// cannot find again is refused: one that begins with the backslash, and one
// holding a CR LF, which [encoding/csv]'s reader turns into LF. A parse reports
// it as [E_CSV_CONFIG] and a write returns it marked [ErrConfig], because the
// separator is the adapter's own setting and no snapshot can satisfy it.
//
// A nested collection — a List of Lists, a List of Vectors — renders by the
// same rule at every depth: an inner list renders as a list cell does, and the
// outer list escapes that text as one element, so its escapes are escaped
// again. The parser splits and unescapes one depth at a time, so it reads every
// depth back.
//
// Every scalar renders one way wherever it sits — a cell, a list element, a
// Vector element or an edge segment — so a Float is written in positional
// notation ([strconv.FormatFloat] with 'f' and the shortest precision)
// everywhere, never with an exponent. A Vector's elements render as Floats, as
// the validator coerces them, so a float32, which only an instance nothing
// validated can hold, is widened to float64 in a Float, a List<Float> and a
// Vector alike.
//
// # Empty Cells
//
// The schema, not the wire, decides what an empty cell holds. [encoding/csv]
// writes the empty string and a missing value as the same empty field, and
// reads a quoted empty field as a bare one, so no spelling of a cell can say
// which one it carries.
//
// For a property the row's type declares, an empty cell is nil where the
// property is optional. Where it is required it is the empty value of the
// property's kind — "" for a String, a UUID, an enum or a pattern, an empty
// list for a List or a Vector — which the validator then checks like any other
// value, as it checks the JSON adapter's "" and []: an empty required UUID
// draws E_CONSTRAINT_FAIL, and a pattern that admits "" accepts it. Where the
// kind has no empty value (an Integer, a Float, a Boolean, a Date, a
// Timestamp) the cell is nil, and the validator reports the property missing.
//
// An empty cell in a column the row's type does not declare is skipped, so a
// header that unions several types' columns — the only shape a file read with
// [WithTypeColumn] can take — parses every row. A value in such a column is
// still reported, by the validator, for a plain column and a dotted one alike.
//
// An edge group is absent only when every cell in it is empty. Inside a present
// group an empty cell stands for an empty segment on every target. An empty
// foreign-key segment is the key's empty value where [WithSchema] supplies the
// target's keys and the key's kind has one (a String or a UUID); otherwise it
// is absent, and the validator reports the missing component. An empty
// edge-property segment follows the property rule above, except that an edge
// property is never null, so an optional one, or a required one whose kind has
// no empty value, is absent on that target.
//
// These values cannot be written so that they read back unchanged. The writer
// refuses the two that would lose data, with an error marked
// [ErrUnrepresentable] so a caller separates a refusal of the data from an I/O
// failure and from [ErrConfig] without matching the message text; it writes the
// other two, which are documented limitations rather than losses the writer can
// prevent:
//
//   - An optional property holding "" or an empty list writes the cell null
//     writes, so it reads back as null. This is a documented limitation: a
//     null-sentinel option would exist only to express a case the
//     specification calls a value, and the schema decides every other case.
//   - A list holding one empty element writes the cell an empty list writes,
//     and an inner list holding one empty element writes the text an empty
//     inner list writes.
//     A lone association target whose key components are all "" and whose
//     edge properties are all absent or "" writes the cell an absent edge
//     writes. The writer refuses both with an error naming the instance,
//     rather than let an element or an association vanish on the way back.
//   - A cell whose text holds a CR LF: [encoding/csv]'s reader turns every CR
//     LF into LF, inside a quoted field too. The writer refuses it with an
//     error naming the column and the instance. A lone CR is written. A file
//     another program wrote with a CR LF inside a quoted field reads as LF,
//     since the parser never sees the CR.
//   - A row whose only field is empty — a single-column type whose key is "" —
//     is written as a quoted empty field, because [encoding/csv.Writer] would
//     write a blank line, which [encoding/csv.Reader] skips.
//
// # Foreign Keys
//
// The writer emits each association as its dotted column group, and the
// parser assembles the group back into the _target_-keyed objects the
// instance validator accepts — identical to the JSON adapter path. Segment
// counts across a group must agree, or the row draws [diag.E_ADAPTER_PARSE]
// naming the relation.
//
// [WithSchema] gives the parser each association's target type. A non-empty
// key component keeps its text either way: every kind a primary key may take
// (String, UUID, Date, Timestamp) reads as the text it was written as. A Date
// or Timestamp component that does not parse draws E_CSV_COERCE with the
// option and the validator's error without it. What the option decides is an
// EMPTY segment, under Empty Cells above.
//
// During snapshot serialization ([Adapter.MarshalSnapshot], [Adapter.WriteSnapshot]),
// edge columns are populated from the snapshot's edge index via [graph.Snapshot]
// edge lookup. An association whose target type does not resolve is refused:
// the writer will not emit columns its own parser cannot name.
//
// # Compositions
//
// CSV is a flat format, so a row has no column for a composed child. The parser
// reads no composition. Both writers refuse a snapshot in which any instance
// holds a composed child with an [ErrUnrepresentable] error naming the type,
// the instance and the composition, before they produce any output. A
// composition with no children loses nothing and is written. The JSON adapter
// carries compositions.
//
// # Provenance
//
// Every instance carries a [location.Provenance]: the source the caller named,
// the instance's path under its own type ($.Entity[0]), and a point span at the
// start of its record. Every row diagnostic carries that span.
//
// The path indexes the instances of that type the parser PRODUCED, not the
// records it read. CSV has no path language and no stable record address — the
// ordinal this package no longer reports is not one — and under
// [Adapter.ParseWithTypeColumn] no other index is well defined: a record the
// reader refuses carries no type, so it can consume no type's index.
//
// No diagnostic names a record ordinal. A record's line comes from the reader,
// so a quoted newline moves it as the file reads, which an ordinal cannot see.
// A record the reader refuses is located from the parse error instead, at the
// start of the line the fault is on; a reader error that is not a parse error
// carries no position, unless the header could not be read at all, which is
// reported at line 1. A header refusal and a missing type column carry the
// header's own line: blank lines before the header are skipped.
//
// A span's column is always 1. [encoding/csv] counts columns in BYTES and
// [location.Position] counts them in runes, and this package parses an
// [io.Reader], so it never holds the line it would need to convert one.
//
// # BOM Handling
//
// Parse methods strip a UTF-8 BOM if present, handling Windows-generated CSVs.
//
// # Thread Safety
//
// An [Adapter] is safe for concurrent use after construction.
// Configuration is immutable after [New] returns.
//
// # Dependencies
//
//	adapter/csv  ──imports──▶  instance, diag, location, location/path, graph,
//	                           immutable, schema, adapter/internal/typetag
package csv
