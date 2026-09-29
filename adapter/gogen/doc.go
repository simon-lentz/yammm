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
// A type's or a property's schema doc-comment, an edge property's included,
// becomes the Go declaration's doc-comment, one "//" line for each of its
// lines; a data type's, a relation's and the schema's own are not written.
// The text is the loaded schema's, whose continuation lines lost the
// indentation they all share, byte for byte, when the parser read the comment,
// so layout that indents every continuation line alike makes no code block;
// lines indented by different bytes, such as a tab on one and spaces on
// another, share none and keep theirs, which the Go doc-comment formatter
// reads as a code block. A line
// indented deeper than the rest keeps the difference and is rendered as a
// code block: gofmt puts a blank "//" line before it, and after it when text
// follows. A line Go would read as a build line — "+build ignore" as a "//"
// line, which gofmt moves into the file's build constraints — is written as a
// one-line /* */ comment in the same comment group, which neither gofmt nor go
// vet reads as one, so the doc reads unchanged; gofmt then leaves that group
// as written.
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
// speak the stored form. A per-layout type's bare spelling is "Timestamp" plus
// the layout's letters and digits (Timestamp20060102150405 for "2006-01-02
// 15:04:05"). When another entity claims that spelling, the layout takes its
// exact spelling (Timestamp_2006_2D_01_2D_02 for "2006-01-02"); see Names. A
// layout named only by a temporal DataType is no entity, since that DataType
// is its own type. A default-layout Timestamp stays time.Time, whose own codec
// already exchanges RFC 3339 with nanoseconds, the form the library stores. A
// DataType resolving to any temporal kind is emitted as
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
// vectors stay nil-able as-is, since a nil slice already encodes an absent
// field. A nil inner slice of a nested List field is not absent: encoding/json
// writes it as a null element, which instance validation refuses
// (E_TYPE_MISMATCH).
//
// Every field carries a json tag preserving the wire name verbatim. An optional
// pointer field adds ,omitempty. An optional slice field adds ,omitzero, which
// leaves out a nil slice and writes a present empty list as [], since the
// library keeps an empty list apart from an absent one; encoding/json reads
// omitzero from Go 1.24, and a consumer built below it writes a nil slice as
// null, which instance validation reads as an absent optional. Every relation field
// adds ,omitempty, required ones included: instance validation refuses a null
// relation, and a required relation left unset is refused where presence is
// checked.
//
// # Associations and the Graph Aggregate
//
// Each declared association is emitted as one struct, whose bare spelling is
// EDGE_<Owner>_<edge>_<Target> (see Names), carrying the target type's primary-key
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
// self-contained package, whose names the Names section states: a name unique in
// the closure stays bare, and two schemas' Region are Type_geo__Region and
// Type_common__Region. Inherited properties, associations, and compositions
// resolve against their declaring schema, so a member inherited from a
// cross-schema parent maps to the correct type.
//
// # Names
//
// Every top-level declaration shares one Go package-block namespace: each type,
// data type, per-layout type, EDGE_ struct, inline enum, List element enum and
// enum value constant, and the names gogen emits or once emitted (Graph,
// SchemaHash, SerializedSources, SerializedEntry, SerializedModel,
// SerializedModelEntry and Date), which are reserved. Each entity has a bare
// spelling, built from the names as the schema writes them:
//
//   - a type or a data type: its name as an exported identifier, under the
//     initialisms (URL for Url);
//   - a per-layout type: "Timestamp" and the layout's letters and digits;
//   - an EDGE_ struct: EDGE_<Owner>_<edge>_<Target>, its owner's and its
//     target's names as exported identifiers and its relation's field name
//     (the name in lower case), so Url's and URL's LINK associations share one;
//   - an inline enum: <Owner><Field>, where <Owner> is its type's bare
//     spelling or its EDGE_ struct's;
//   - the inline enum a List data type holds as its element: <DataType>Element;
//   - an enum value constant: its enum's bare spelling and the value as an
//     exported identifier ("a-b" gives AB).
//
// An entity takes its bare spelling when no other entity and no reserved name
// has that spelling. Otherwise every claimant takes its exact spelling: a word
// that names its family, "_", and the parts of its identity joined by "__",
// each part with every rune other than a letter or a digit written "_<hex>_",
// its code point in upper-case hexadecimal:
//
//	Type_<schema>__<Name>                                   a type
//	DataType_<schema>__<Name>                               a data type
//	Timestamp_<layout>                                      a per-layout type
//	Association_<schema>__<Owner>__<RELATION>               an EDGE_ struct
//	Enum_<schema>__<Owner>__<field>                         an inline enum on a type
//	AssociationEnum_<schema>__<Owner>__<RELATION>__<field>  an inline enum on an edge
//	Element_<schema>__<DataType>                            a List element enum
//	Const_<its enum's exact spelling>__<value>              an enum value constant
//
// So Url and URL in schema geo are Type_geo__Url and Type_geo__URL, a type and a
// data type Region are Type_geo__Region and DataType_geo__Region, and the values
// "a-b" and "a_b" of data type Level are Const_DataType_geo__Level__a_2D_b and
// Const_DataType_geo__Level__a_5F_b.
//
// A bare spelling either starts "EDGE_" or holds "_" only after a digit or an
// "X" and before a digit. An exact spelling holds "_" directly after its word,
// which ends in another letter and is never "EDGE", so no bare spelling is an
// exact one. Past that first "_", an exact spelling holds "_" only in a
// "_<hex>_" group, a "__" join, or, in a value constant's, after its enum's
// word, which fixes how many parts follow; so it reads back to one family and
// one identity, and no two entities share one. A name therefore depends on the set of claims, never on
// declaration or import order, and adding a declaration never gives a name to
// another entity: it can only move a claimant from its bare spelling to its
// exact one.
//
// The fields of one struct share a namespace of their own. A field's bare
// spelling is its property's name, or its relation's field name (the
// relation's name in lower case), as an exported identifier, and
// an EDGE_ struct's key field is "Target" and the key property's name. Two
// fields whose bare spellings meet each take "Field_" and their wire key, which
// their json tag also holds: Field_foo_1 and Field_foo1 for foo_1 and foo1.
//
// # Embedded Source
//
// Every generated file embeds the .yammm source it was generated from, so the
// schema can be re-loaded at runtime without the original files on disk. One
// surface carries it, emitted identically whatever the source count:
//
//   - func SerializedSources() map[string][]byte returns every source the
//     schema's load held, by its key, and const SerializedEntry names the
//     entry's key. The recommended re-load is
//     [github.com/simon-lentz/yammm/schema.LoadSourcesWithEntry] with an empty
//     module root, [github.com/simon-lentz/yammm/schema.WithSourcesOnly] and
//     [github.com/simon-lentz/yammm/schema.WithSyntheticRoot], which gives type
//     identities no working directory, checkout, or container mount point can move.
//
// The backing store is an unexported package-level map, so the identifiers a
// consumer sees do not vary with how many files a schema happens to span.
//
// A key is the name the re-load registers a source under, never an absolute
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
//     to "schema"); a keyword, a predeclared identifier such as nil, string or len,
//     "main" or "init" takes a "_" suffix, since a file importing a package named
//     for a predeclared identifier can no longer use that identifier, a file of
//     declarations alone cannot build as package main, and a package named init
//     cannot be imported without an alias. An explicit name, the empty one
//     included, must be a Go identifier that is not a keyword and not "_", and may
//     be "main", "init" or a predeclared identifier.
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
//     ([ErrInvalidPackageName]; [CheckPackageName] applies the same rule
//     before a schema is loaded);
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
// the identifier-casing transform that turns schema names into Go
// identifiers — and, unlike the data adapters, it imports neither instance/graph
// nor diag. The generated output depends only on the standard library, importing at
// most "time" and "encoding/json".
package gogen
