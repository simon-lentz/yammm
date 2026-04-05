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
// # Type Coercion
//
// CSV values are strings. The adapter uses [*schema.Type] constraint metadata
// to coerce string values to typed Go values during parsing:
//
//   - Integer properties: [strconv.ParseInt]
//   - Float properties: [strconv.ParseFloat]
//   - Boolean properties: [strconv.ParseBool] ("true", "false", "1", "0")
//   - Date properties: validated as "2006-01-02" format, kept as string
//   - Timestamp properties: validated as RFC 3339 format, kept as string
//   - List properties: split by list separator (default "|"), elements coerced
//
// Date and Timestamp values remain as strings in [instance.RawInstance],
// matching the JSON adapter's behavior. Temporal coercion to driver types
// happens downstream in write adapters.
//
// # List Properties
//
// List values use a configurable separator (default "|"). A cell containing
// "a|b|c" for a List<String> property produces []any{"a", "b", "c"}.
// Configure via [WithListSeparator].
//
// # Null Handling
//
// The configured null string (default: empty string "") is treated as nil.
// Configure via [WithNullValue].
//
// # Foreign Keys
//
// FK columns appear as regular properties (e.g., "in_issuer" or
// "_target_issuer_id"). The instance validator handles edge resolution
// from the raw property map, identical to the JSON adapter path.
//
// During snapshot serialization ([Adapter.MarshalSnapshot], [Adapter.WriteSnapshot]),
// FK columns are included in the header but are empty because [graph.Instance]
// does not carry edge data (edges are stored externally in the graph). To
// populate FK columns, use [Adapter.MarshalTyped] or [Adapter.WriteTyped] with
// [instance.ValidInstance] values that have resolved edge data.
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
