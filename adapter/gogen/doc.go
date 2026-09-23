// Package gogen generates Go source from a yammm schema: one struct per type,
// named Enum/DataType types, generated temporal types, EDGE_ association
// structs, a Graph aggregate, and the embedded schema source. Output is
// stdlib-only — it imports at most "time" and "encoding/json". Call [Marshal]
// with a loaded, resolved schema; it returns formatted, type-checked bytes.
//
// # Schema-In, Bytes-Out
//
// Unlike the data adapters (adapter/json, adapter/csv, adapter/neo4j), gogen has no
// instance-data path: it never parses, validates, or serializes instances, and it
// imports neither instance nor graph. It maps a completed schema to Go source,
// nothing more. It also returns a plain error rather than the
// [github.com/simon-lentz/yammm/diag.Result] the rest of the library threads
// through, because its failures are refusals of its inputs and generator bugs (see
// Error Conditions), not data diagnostics with source locations.
//
// # Generated Declarations
//
// [Marshal] emits, in deterministic order:
//
//   - A named Go type for every DataType (type <Name> <base>), plus value constants
//     when the DataType resolves to an Enum.
//   - A per-owner named type for every inline-enum property
//     (type <Owner><Field> string), plus its value constants.
//   - A Date type when a position emits Date, and one type per distinct
//     custom Timestamp layout a position emits, each a struct embedding
//     time.Time with a JSON codec that exchanges the value in the string form
//     the library stores (see Type Mapping).
//   - One struct per type in the closure. Concrete, abstract, and part types are all
//     emitted — part types because compositions reference them, abstract types
//     because they document the schema even though inheritance is flattened into
//     each concrete struct.
//   - One EDGE_ struct per declared association (see Associations and the Graph
//     Aggregate).
//   - A Graph aggregate (see Associations and the Graph Aggregate).
//   - The embedded schema source, reachable through the SerializedSources /
//     SerializedEntry pair, and SchemaHash (see the Embedded Source section
//     below).
//
// A type's or property's schema doc-comment becomes the Go declaration's
// doc-comment, one "//" line for each of its lines. The indentation its
// continuation lines share is removed, because the Go doc-comment formatter
// reads an indented line as a code block. A line indented deeper than the rest
// keeps the difference and is rendered as a code block, which gofmt sets apart
// with a blank "//" line before and after it.
//
// # Type Mapping
//
// Each property's constraint maps to a Go type:
//
//	String, UUID, Pattern, Enum   →  string
//	Integer                       →  int64
//	Float                         →  float64
//	Boolean                       →  bool
//	Timestamp                     →  time.Time
//	Timestamp["<layout>"]         →  Timestamp<layout>  (generated, one per layout)
//	Date                          →  Date               (generated)
//	Vector                        →  []float64
//	List<T>                       →  []T
//
// The library stores a Date as "2006-01-02" and a custom-layout Timestamp
// through its declared layout, and adapter/json writes those strings, which a
// bare time.Time cannot decode. The generated Date and per-layout types are
// structs embedding time.Time — so every time.Time method is promoted and a
// value is built as Date{Time: t} — carrying MarshalJSON and UnmarshalJSON that
// speak the stored form. A per-layout type is named "Timestamp" plus the
// layout's letters and digits (Timestamp20060102150405 for "2006-01-02
// 15:04:05") when nothing else claims that name. It is claimed when another
// layout that a position renders as a generated type has the same letters and
// digits, or when a declared type or data type, a reserved name or an EDGE_
// struct holds it; an inline enum is named later and yields to the layout. A
// layout named only by a temporal DataType claims nothing, since that DataType
// is its own type. A layout whose name is claimed takes "Timestamp_" plus its
// layout with every other rune written as its hexadecimal code point between
// underscores, and an invalid UTF-8 byte as "_x" and its hexadecimal value
// (Timestamp_2006_2D_01_2D_02 for "2006-01-02"), a name nothing else can hold.
// A layout's name therefore never passes to another layout: a schema edit that
// claims a bare name moves its layout to the exact name, which no other layout
// can take. A default-layout Timestamp stays time.Time,
// whose own codec already exchanges RFC 3339 with nanoseconds, the form the
// library stores. A DataType resolving to any temporal kind is emitted as
// struct{ time.Time } too, with the codec its layout needs.
//
// Named types are rendered faithfully rather than collapsed to their primitive: a
// field typed by a named DataType keeps that Go type; a List whose innermost
// element is a named DataType renders it at every depth ([]<Name>,
// [][]<Name>), in a property and in a List DataType's own declaration; and an
// enum declared inline on a property becomes that property's own
// <Owner><Field> string type, at any List depth ([]<Owner><Field>), and an
// edge property's owner is its EDGE_ struct; the inline enum a List
// DataType holds as its innermost element becomes <DataType>Element, so
// type Tags = List<Enum[...]> emits type TagsElement and type Tags
// []TagsElement. An optional non-slice field becomes a pointer (*T); slices and
// vectors stay nil-able as-is, since a nil slice already encodes absence.
//
// Every field carries a json tag preserving the wire name verbatim. An optional
// pointer field adds ,omitempty. An optional slice field adds ,omitzero, which
// leaves out a nil slice and writes a present empty list as [], since the
// library keeps an empty list apart from an absent one; encoding/json reads
// omitzero from Go 1.24, and a consumer built below it writes a nil slice as
// null, which adapter/json reads as an absent optional. Every relation field
// adds ,omitempty, required ones included: the parser refuses a null relation,
// and a required relation left unset is refused where presence is checked.
//
// # Associations and the Graph Aggregate
//
// Each declared association is emitted as one struct named
// EDGE_<Owner>_<edge>_<Target>, carrying the target type's primary-key
// fields under the parser's reserved "_target_" keys, with the association's
// own edge properties beside them — the shape adapter/json's parser and
// writer exchange:
//
//	type EDGE_Person_employer_Company struct {
//		TargetID string    `json:"_target_id"` // the target Company's primary key
//		Since    time.Time `json:"since"`      // an edge property
//	}
//
// The owning type references it as a field — *EDGE_… for a to-one association,
// []*EDGE_… for a to-many — and an association inherited by several subtypes is
// emitted once, by its declaring type, with every subtype's field pointing at that
// same struct. A completed schema guarantees every association targets a concrete
// type (E_INVALID_ASSOCIATION_TARGET) that carries a primary key
// (E_NO_PRIMARY_KEY), so the "_target_" fields always exist. Compositions, by
// contrast, are inlined as ownership slices ([]*Child for every
// multiplicity — the parser exchanges an array for (one) too) on the owning
// struct.
//
// The Graph aggregate is the top-level envelope: one slice field per concrete type
// the entry schema can name (abstract and part types are excluded), each keyed by
// its [github.com/simon-lentz/yammm/schema.AddressableTag], the name adapter/json
// keys its object by — the bare type name for the entry schema's own types and
// the alias-qualified name for a directly imported one — so a document that
// adapter writes decodes into the aggregate. A type the entry schema reaches only
// through another import has no such name, so no document can hold it at top
// level, and it takes no Graph field. Its struct is still emitted, as every type
// in the closure is. The keys are the envelope keys adapter/jschema emits.
//
//	type Graph struct {
//		Person []*Person `json:"Person,omitempty"`
//		Region []*Region `json:"common.Region,omitempty"`
//	}
//
// # Imports and Cross-Schema Schemas
//
// gogen handles the full range of yammm schemas, including schemas with imports.
// The entire import closure — the entry schema plus every transitively imported
// schema, walked deterministically and deduped by source — is flattened into one
// self-contained package. Go names are unqualified where they are unique across the
// closure and schema-qualified (<Schema><Name>) on collision. Every unique name is
// assigned before any qualified one, so a qualified name already taken — by another
// type or data type, or by a reserved name such as SchemaHash — takes a numeric
// suffix (AFoo2) rather than the other declaration's name.
// Two entities of one schema that map to one Go name — a type and a data type
// both named Region, or Url and URL under the initialisms — both qualify, and
// the numeric suffix separates them in declaration order, types before data
// types (GeoRegion, GeoRegion2). Inherited properties, associations, and
// compositions resolve against their declaring schema, so a member inherited
// from a cross-schema parent maps to the correct type.
//
// # Embedded Source
//
// Every generated file embeds the .yammm source it was generated from, so the
// schema can be re-loaded at runtime without the original files on disk. One
// surface carries it, emitted identically whatever the source count:
//
//   - func SerializedSources() map[string][]byte returns every source in the
//     closure by its key, and const SerializedEntry names the entry's key. The
//     recommended re-load is
//     [github.com/simon-lentz/yammm/schema.LoadSourcesWithEntry] with an empty
//     module root, [github.com/simon-lentz/yammm/schema.WithSourcesOnly] and
//     [github.com/simon-lentz/yammm/schema.WithSyntheticRoot], which gives type
//     identities no working directory, checkout, or container mount point can move.
//
// The backing store is an unexported package-level map, so the identifiers a
// consumer sees do not vary with how many files a schema happens to span.
//
// A key is the name the re-load looks a source up by, never an absolute
// generation-machine path, so the output is byte-reproducible across checkouts
// and CI. The entry keys by its path under the load's recorded module root
// ([github.com/simon-lentz/yammm/schema.Schema.ModuleRoot] — supplied by the
// caller, discovered from a yammm.mod marker, or a synthetic root), compared as
// an identity, with ".." segments when the entry lies outside it. A file entry
// loaded with no root keys against its own directory. A
// [github.com/simon-lentz/yammm/schema.LoadString] source has no root and keys
// by the base name it was loaded under, split at either separator; a name with
// no base, such as ".", names no file and is refused.
// Every imported source keys by the text that imports it, resolved against its
// importer's key by [github.com/simon-lentz/yammm/schema.SyntheticImportKey],
// the loader's own rule, so an import through a symlinked directory keys by
// the path the import names, not by where the link resolves.
//
// Two layouts a load accepts have no store the re-load can read, and [Marshal]
// refuses both, naming the paths: one source imported by two paths under two
// keys (a store holding both fails to re-load as one schema name registered
// twice), and two sources
// taking one key (a relative import through a symlink reads the link target's
// neighbour on load and resolves by the key's text on re-load).
// A source with no path relative to the root, such as one on another drive, is
// refused too, because a key is never a generation-machine path, and so is a
// LoadString name with no base, which names no file. So is a key the re-load's
// key rule, [github.com/simon-lentz/yammm/location.NormalizeSyntheticKey],
// refuses: one holding a backslash, which a Unix file name may hold and another
// host reads as a separator, and one that looks absolute, such as the key of a
// source in a directory named "C:" directly under the root. The entry and a
// source nothing imports key from their identities, so a symlinked path to one
// is judged by where the link resolves. const SchemaHash carries the schema's
// [github.com/simon-lentz/yammm/schema.StructuralHash]. Before returning,
// [Marshal] re-loads the store exactly as emitted, through the recipe the
// generated file prints, and confirms it produces the input's StructuralHash —
// hermetically, under [github.com/simon-lentz/yammm/schema.WithSourcesOnly], so
// a mis-keyed source fails generation rather than being silently satisfied by
// an on-disk file. [Marshal] returns only bytes that check passed.
//
// # Output Guarantees
//
// Generated source is gofmt-formatted and then type-checked with go/types before
// [Marshal] returns. Type-checking is hermetic: the output imports at most "time"
// and "encoding/json" and calls only time.Time.Format, time.Parse, json.Marshal
// and json.Unmarshal, so a synthetic importer declaring exactly those satisfies
// it with no Go toolchain, GOROOT, or build cache — [Marshal] behaves identically
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
// [github.com/simon-lentz/yammm/schema.LoadString], or
// [github.com/simon-lentz/yammm/schema.LoadSourcesWithEntry]. A schema assembled
// programmatically through [github.com/simon-lentz/yammm/schema.NewBuilder] without
// retained source content has nothing to embed and nothing for the round-trip
// check to re-load, so [Marshal] returns an error for it.
//
// # Configuration
//
//   - [WithPackageName]: override the generated package name. The default is derived
//     from the schema name, sanitized to a valid lowercase identifier (falling back
//     to "schema"); a keyword, "main" or "init" takes a "_" suffix, since a file of
//     declarations alone cannot build as package main and a package named init
//     cannot be imported without an alias. An explicit name must be a Go identifier
//     that is not a keyword and not "_", and may be "main" or "init".
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
//   - the [WithPackageName] value cannot head a package clause
//     ([ErrInvalidPackageName]);
//   - a source is imported by two paths under two keys, two sources take one
//     key, a source has no path relative to the root, a LoadString name has
//     no base, or a key is one the re-load's key rule refuses (see Embedded
//     Source);
//   - the generated source fails to format, fails to type-check, or the
//     embedded store fails its round-trip hash check (each a generator bug).
//
// # Thread Safety
//
// [Marshal] is safe for concurrent use. It allocates a fresh generator per call and
// shares no mutable state; the package-level acronym set is cloned, never mutated.
//
// # Annotations
//
// Schema annotations do not shape the generated Go: no struct field, tag, enum,
// or method reflects them. They describe store-level concerns — index shape,
// write-once behaviour — that a Go type does not express, and
// [github.com/simon-lentz/yammm/adapter/neo4j] is where they are read.
//
// They do survive in the embedded source, which is held verbatim and reachable
// through SerializedSources. A schema re-loaded from it carries its annotations,
// and the store DDL derived from the re-loaded schema matches the DDL derived
// from the original files — the whole point of embedding the source rather than
// a reduction of it. Adding an annotation to a schema changes the embedded text
// and nothing else in the emitted file.
//
// They do reach the generator indirectly: a property inherited from several
// ancestors carries the union of their annotations, so the merged view yields a
// synthesized copy rather than the declared *Property. Every pointer-keyed
// lookup here therefore goes through [github.com/simon-lentz/yammm/schema.Property.Origin].
//
// # Dependencies
//
//	adapter/gogen  ──imports──▶  schema, location, internal/ident
//
// gogen is the one adapter that imports a core internal package — internal/ident, for
// the canonical identifier-casing transform the library uses elsewhere (e.g. JSON
// field names) — and, unlike the data adapters, it imports neither instance/graph
// nor diag. The generated output depends only on the standard library, importing at
// most "time" and "encoding/json".
package gogen
