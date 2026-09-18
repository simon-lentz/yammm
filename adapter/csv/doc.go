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
// # Column Mapping
//
// Column names map 1:1 to property names. A CSV header row
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
//   - List properties: split by the list separator, elements coerced
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
// [WithListSeparator]. Both sides escape through one shared helper pair
// (the backslash escapes itself and the separator), so an element
// containing the separator survives the round trip.
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
// still reported: as E_CSV_COERCE for a dotted column that names no
// association or edge property of the row's type, and by the validator for a
// plain column or a key component the target does not declare.
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
// These values cannot be written so that they read back unchanged:
//
//   - An optional property holding "" or an empty list writes the cell null
//     writes, so it reads back as null. This is a documented limitation: a
//     null-sentinel option would exist only to express a case the
//     specification calls a value, and the schema decides every other case.
//   - A list holding one empty element writes the cell an empty list writes.
//     A lone association target whose key components are all "" and whose
//     edge properties are all absent or "" writes the cell an absent edge
//     writes. The writer refuses both with an error naming the instance,
//     rather than let an element or an association vanish on the way back.
//   - A row whose only field is empty — a single-column type whose key is "" —
//     is written as a quoted empty field, because [encoding/csv.Writer] would
//     write a blank line, which [encoding/csv.Reader] skips.
//
// # Foreign Keys
//
// The writer emits each association as its dotted column group, and the
// parser assembles the group back into the _target_-keyed objects the
// instance validator accepts — identical to the JSON adapter path. Segment
// counts across a group must agree, or the row draws E_CSV_COERCE naming
// the relation.
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
// CSV is a flat format. Compositions are not supported in parsing and are
// silently omitted during serialization.
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
// carries no position at all. The two refusals that precede every record — a
// header that cannot be read, and a missing type column — carry line 1.
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
//	                           immutable, schema
package csv
