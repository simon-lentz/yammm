// Package csv provides a CSV adapter for parsing delimited data into
// [instance.RawInstance] values and serializing validated instances to CSV.
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
// # Null Handling
//
// An empty cell in a property column is treated as nil. An edge column is
// different: an all-empty group means the edge is absent, and an empty
// segment means the optional edge property is absent on that target —
// never null, which the validator rejects for edges.
//
// # Foreign Keys
//
// The writer emits each association as its dotted column group, and the
// parser assembles the group back into the _target_-keyed objects the
// instance validator accepts — identical to the JSON adapter path. Segment
// counts across a group must agree, or the row draws E_CSV_COERCE naming
// the relation.
//
// FK components coerce against the target type's primary-key constraints
// when the adapter is constructed with [WithSchema]; without it they stay
// strings, which string-keyed targets accept and the validator reports for
// the rest.
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
//	adapter/csv  ──imports──▶  instance, diag, location, graph, immutable, schema
package csv
