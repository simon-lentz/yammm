// Package gogen generates Go source from a yammm schema: one struct per type,
// named Enum/DataType types, EDGE_ association structs, a Graph aggregate, and an
// embedded SerializedModel. Output is stdlib-only — it imports at most "time". Call
// [Marshal] with a loaded, resolved schema; it returns formatted, type-checked
// bytes.
//
// # Schema-In, Bytes-Out
//
// Unlike the data adapters (adapter/json, adapter/csv, adapter/neo4j), gogen has no
// instance-data path: it never parses, validates, or serializes instances, and it
// imports neither instance nor graph. It maps a completed schema to Go source,
// nothing more. It also returns a plain error rather than the
// [github.com/simon-lentz/yammm/diag.Result] the rest of the library threads
// through, because its only failures are generator-internal (see Error Conditions),
// not data diagnostics with source locations.
//
// # Generated Declarations
//
// [Marshal] emits, in deterministic order:
//
//   - A named Go type for every DataType (type <Name> <base>), plus value constants
//     when the DataType resolves to an Enum.
//   - A per-owner named type for every inline-enum property
//     (type <Owner><Field> string), plus its value constants.
//   - One struct per type in the closure. Concrete, abstract, and part types are all
//     emitted — part types because compositions reference them, abstract types
//     because they document the schema even though inheritance is flattened into
//     each concrete struct.
//   - One EDGE_ struct per declared association (see Associations and the Graph
//     Aggregate).
//   - A Graph aggregate (see Associations and the Graph Aggregate).
//   - The embedded SerializedModel and SchemaHash (see SerializedModel).
//
// A type's or property's schema doc-comment is carried through verbatim as the Go
// declaration's doc-comment.
//
// # Type Mapping
//
// Each property's constraint maps to a Go type:
//
//	String, UUID, Pattern, Enum   →  string
//	Integer                       →  int64
//	Float                         →  float64
//	Boolean                       →  bool
//	Date, Timestamp               →  time.Time
//	Vector                        →  []float64
//	List<T>                       →  []T
//
// Named types are rendered faithfully rather than collapsed to their primitive: a
// field typed by a named DataType keeps that Go type, a List of a named DataType
// renders []<Name> (not []string), and an enum declared inline on a property
// becomes that property's own <Owner><Field> string type. An optional non-slice
// field becomes a pointer (*T); slices and vectors stay nil-able as-is, since a nil
// slice already encodes absence. Every field carries a json tag preserving the wire
// name, with ,omitempty added when the field is optional.
//
// # Associations and the Graph Aggregate
//
// Each declared association is emitted as one struct named
// EDGE_<Owner>_<edge>_<Target>, carrying the association's own edge properties plus
// a Where block holding the target type's primary-key fields (wire key "where"):
//
//	type EDGE_Person_employer_Company struct {
//		Since time.Time `json:"since"`   // an edge property
//		Where struct {
//			ID string `json:"id"`        // the target Company's primary key
//		} `json:"where"`
//	}
//
// The owning type references it as a field — *EDGE_… for a to-one association,
// []*EDGE_… for a to-many — and an association inherited by several subtypes is
// emitted once, by its declaring type, with every subtype's field pointing at that
// same struct. A completed schema guarantees every association targets a concrete
// type (E_INVALID_ASSOCIATION_TARGET) that carries a primary key
// (E_NO_PRIMARY_KEY), so a Where block always has at least one field. Compositions,
// by contrast, are inlined as ownership fields (*Child or []*Child) on the owning
// struct.
//
// The Graph aggregate is the top-level envelope: one slice field per concrete type
// in the closure (abstract and part types are excluded), each keyed by its
// lower_snake JSON name.
//
//	type Graph struct {
//		Person  []*Person  `json:"person"`
//		Company []*Company `json:"company"`
//	}
//
// # Imports and Cross-Schema Schemas
//
// gogen handles the full range of yammm schemas, including schemas with imports.
// The entire import closure — the entry schema plus every transitively imported
// schema, walked deterministically and deduped by source — is flattened into one
// self-contained package. Go names are unqualified where they are unique across the
// closure and schema-qualified (<Schema><Name>) on collision; inherited properties,
// associations, and compositions resolve against their declaring schema, so a
// member inherited from a cross-schema parent maps to the correct type. A collision
// that qualification cannot resolve — two entities in one schema mapping to a single
// Go name — is a hard error, mirroring the label-collision handling in
// adapter/neo4j.
//
// # SerializedModel
//
// Every generated file embeds the .yammm source it was generated from, so the
// schema can be re-loaded at runtime without the original files on disk:
//
//   - A single-source schema embeds var SerializedModel string, re-loadable via
//     [github.com/simon-lentz/yammm/schema.LoadString].
//   - A multi-source (imported) schema embeds var SerializedModel map[string]string
//     keyed by module-root-relative path, plus const SerializedModelEntry naming the
//     entry point, re-loadable via
//     [github.com/simon-lentz/yammm/schema.LoadSourcesWithEntry] with module root "."
//     (add [github.com/simon-lentz/yammm/schema.WithSourcesOnly] to guarantee the
//     re-load never touches the filesystem).
//
// Keys are relative to the load's recorded module root
// ([github.com/simon-lentz/yammm/schema.Schema.ModuleRoot] — the WithModuleRoot
// value when given, else the entry's directory), never absolute generation-machine
// paths, so the output is byte-reproducible across checkouts and CI and the keys
// match the module-style import statements inside the sources on re-load. const
// SchemaHash carries the schema's
// [github.com/simon-lentz/yammm/schema.StructuralHash]. Before returning, [Marshal]
// re-loads the embedded model and confirms its StructuralHash matches the input's —
// hermetically, under [github.com/simon-lentz/yammm/schema.WithSourcesOnly], so a
// mis-keyed source fails generation rather than being silently satisfied by an
// on-disk file — making the embedded provenance a guaranteed re-loadable model
// rather than an unverified claim.
//
// # Output Guarantees
//
// Generated source is gofmt-formatted and then type-checked with go/types before
// [Marshal] returns. Type-checking is hermetic: because the output imports at most
// "time" (and only as the time.Time field type), a synthetic importer satisfies it
// with no Go toolchain, GOROOT, or build cache — so [Marshal] behaves identically
// inside the distributed yammm CLI binary, a CI container, or a scratch image, while
// still catching duplicate declarations, unused imports, and undefined references. A
// format or type-check failure is treated as a generator bug and surfaced as an
// error rather than emitted as broken Go. All closure walks and name assignments are
// ordered, so output is deterministic across runs.
//
// # Preconditions
//
// The schema must be completed (aliases resolved, inheritance linearized) — always
// true for a schema returned by [github.com/simon-lentz/yammm/schema.Load] or
// [github.com/simon-lentz/yammm/schema.Builder.Build] — and it must be
// source-backed: loaded via [github.com/simon-lentz/yammm/schema.Load],
// [github.com/simon-lentz/yammm/schema.LoadString],
// [github.com/simon-lentz/yammm/schema.LoadSources], or
// [github.com/simon-lentz/yammm/schema.LoadSourcesWithEntry]. A schema assembled
// programmatically through [github.com/simon-lentz/yammm/schema.NewBuilder] without
// retained source content has no source for the SerializedModel and its round-trip
// check, so [Marshal] returns an error for it.
//
// # Configuration
//
//   - [WithPackageName]: override the generated package name. The default is derived
//     from the schema name, sanitized to a valid lowercase identifier (falling back
//     to "schema").
//   - [WithInitialisms]: register extra acronyms (e.g. "GUID", "JWT") the name mapper
//     upper-cases wholesale in exported identifiers. They merge with gogen's default
//     golint acronym set and are matched case-insensitively. This is how a downstream
//     generator injects its own domain vocabulary without that vocabulary ever living
//     in yammm.
//
// # Error Conditions
//
// [Marshal] returns an error (never partial or broken Go) when:
//
//   - the schema is not source-backed (e.g. built via
//     [github.com/simon-lentz/yammm/schema.NewBuilder] without retained source);
//   - a Go name collision cannot be resolved by schema-qualification;
//   - the generated source fails to format, fails to type-check, or its embedded
//     SerializedModel fails the round-trip hash check (each a generator bug).
//
// # Thread Safety
//
// [Marshal] is safe for concurrent use. It allocates a fresh generator per call and
// shares no mutable state; the package-level acronym set is cloned, never mutated.
//
// # Dependencies
//
//	adapter/gogen  ──imports──▶  schema, location, internal/ident
//
// gogen is the one adapter that imports an internal package — internal/ident, for
// the canonical identifier-casing transform the library uses elsewhere (e.g. JSON
// field names) — and, unlike the data adapters, it imports neither instance/graph
// nor diag. The generated output depends only on the standard library, importing at
// most "time".
package gogen
