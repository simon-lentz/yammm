// Package jschema generates a JSON Schema (draft 2020-12) document from a
// yammm schema, for editor-assisted authoring of the instance-data JSON that
// `yammm check` accepts. Call [Marshal] with a loaded, resolved schema; it
// returns a deterministic, self-contained document that editors (VS Code's
// JSON/YAML language servers, IntelliJ, Helix) can use for completion,
// hover documentation, and validation while data files are being written.
//
// # Schema-In, Bytes-Out
//
// Like adapter/gogen — and unlike the data adapters (adapter/json,
// adapter/csv, adapter/neo4j) — jschema has no instance-data path: it never
// parses, validates, or serializes instances, and it imports neither
// instance nor graph. It maps a completed schema to one JSON document,
// nothing more, and returns a plain error rather than the
// [github.com/simon-lentz/yammm/diag.Result] the rest of the library threads
// through, because its only failures are generator-internal (see Error
// Conditions), not data diagnostics with source locations.
//
// # The Target Contract
//
// The emitted document describes exactly the JSON object form the yammm
// instance layer accepts — the shape parsed by adapter/json's ParseObject
// and validated by [github.com/simon-lentz/yammm/instance.Validator]:
//
//   - The envelope is a single JSON object keyed by type name, each key
//     holding an array of instance objects. Entry-schema types are keyed by
//     their bare name; directly imported types by their alias-qualified name
//     (alias.Type — the only form the validator resolves for them). Types in
//     transitively imported schemas are structurally reachable through $defs
//     but have no addressable top-level key.
//   - An instance object holds properties by name, compositions and
//     associations by their lower_snake field name. Unknown fields are
//     rejected by the instance layer, so instance objects emit
//     additionalProperties false.
//   - An association renders as an edge object (to-one) or an array of edge
//     objects (to-many). An edge object carries one _target_<pk_name> field
//     per primary-key component of the target type — all required, each
//     validating against the target key's own constraint — plus the
//     association's edge properties. Association PRESENCE is deliberately
//     not required per-file: the instance layer defers it to graph assembly
//     (E_UNRESOLVED_REQUIRED), and a per-file required would flag
//     batch-authored files whose references resolve across files. The
//     generated description states the multiplicity and, for required
//     associations, where enforcement happens.
//   - A composition is ALWAYS an array of child instance objects, regardless
//     of multiplicity. A required composition is listed in required and gets
//     minItems 1 (absent or empty is an instance-layer error); a to-one
//     composition gets maxItems 1 (a second child is rejected at graph
//     assembly as a duplicate composed primary key).
//
// Schema doc-comments flow through: a type's documentation becomes its def's
// description, a property's (or edge property's, or DataType's) becomes its
// fragment's description, and a relation's is appended to the generated
// relation description, the JSON Schema keyword for human-readable
// documentation.
//
// # Constraint Mapping
//
// Each property constraint maps to a JSON Schema fragment:
//
//	String[min, max]      →  {"type":"string"} + minLength/maxLength
//	Integer[min, max]     →  {"type":"integer"} + minimum/maximum
//	Float[min, max]       →  {"type":"number"} + minimum/maximum
//	Boolean               →  {"type":"boolean"}
//	Enum[...]             →  {"type":"string","enum":[...]}   (inline, no def)
//	Pattern[p]            →  {"type":"string","pattern":p}
//	Pattern[p1, p2]       →  {"type":"string","allOf":[{"pattern":p1},{"pattern":p2}]}
//	UUID                  →  {"type":"string","format":"uuid"}
//	Date                  →  {"type":"string","format":"date"}
//	Timestamp             →  {"type":"string","format":"date-time"}
//	Timestamp["layout"]   →  {"type":"string"} + a description carrying the source form
//	Vector[N]             →  number array with minItems/maxItems N
//	List<T>[min, max]     →  {"type":"array","items":<T>} + minItems/maxItems
//	named DataType        →  {"$ref":"#/$defs/<Name>"}
//
// Named DataTypes are rendered faithfully as $refs in every position —
// scalar property, list element (List<FipsCode> emits items $ref), edge
// property, and _target_* foreign-key field — so the named constraint is
// stated once and hover shows its name and documentation. Inline enums are
// inlined (JSON Schema needs no synthesized names). A yammm value must match
// EVERY declared pattern, hence allOf, not anyOf.
//
// # Names, $defs, and Imports
//
// The full import closure — the entry schema plus every transitively
// imported schema — is flattened into one self-contained document. $defs
// keys are raw schema names: unqualified where unique across the closure,
// <schemaName>.<Name>-qualified on collision; a collision qualification
// cannot separate (a type and a DataType sharing one name in one schema) is
// a hard error instructing a rename. Each declared association gets one
// EDGE_<ownerKey>_<field>_<targetKey> entry, shared by every subtype that
// inherits it. Abstract types get no $defs entry — associations must target
// concrete non-part types and compositions must target part types, so
// nothing can ever $ref an abstract type; its members reach the document
// flattened into each subtype.
//
// # Fidelity Caveats
//
// Three places where the emitted schema and yammm validation intentionally
// diverge, always in the safe direction (the editor may under- or over-flag;
// `yammm check` remains the authority):
//
//   - Canonical-name authoring. The instance layer matches property names
//     case-insensitively by default, which JSON Schema cannot express. The
//     emitted schema targets canonical spellings: a case-variant file still
//     validates under yammm, and the editor nudges it toward the canonical
//     form.
//   - Pattern dialects. yammm compiles patterns as Go/RE2; JSON Schema
//     validators assume ECMA-262. Patterns pass through verbatim — the
//     shared subset covers common cases, and a divergent pattern degrades to
//     editor-side false results only.
//   - Custom Timestamp layouts. A Timestamp["layout"] constraint is not
//     expressible as a JSON Schema assertion; the emitted fragment is a
//     plain string whose description carries the source form. The editor
//     accepts what yammm may reject, never the reverse.
//
// The format keywords the generator emits (uuid, date, date-time) are
// annotations by default under draft 2020-12; validators assert them only
// when configured to (as this package's own contract tests do).
//
// # Editor Wiring
//
// Generate the document next to the data files it describes, then reference
// it. For YAML data files via yaml-language-server:
//
//	# yaml-language-server: $schema=./fleet.schema.json
//
// For JSON data files, either a "$schema" member in the file (when the
// editor supports it) or a json.schemas mapping in VS Code settings
// associating the document with a file glob.
//
// # Output Guarantees
//
// Output is deterministic — byte-identical across runs, machines, and
// checkouts (no absolute paths, all walks ordered) — so generated documents
// can be committed and drift-checked by regenerating and diffing. Before
// returning, [Marshal] verifies the bytes parse as JSON and that every $ref
// resolves to an emitted $defs entry; a failure there is a generator bug
// surfaced as an error, never emitted output.
//
// # Preconditions
//
// The schema must be completed (aliases resolved, inheritance linearized) —
// always true for a schema returned by
// [github.com/simon-lentz/yammm/schema.Load],
// [github.com/simon-lentz/yammm/schema.LoadString],
// [github.com/simon-lentz/yammm/schema.LoadSourcesWithEntry], or
// [github.com/simon-lentz/yammm/schema.Builder.Build]. jschema does not
// require a source-backed schema (nothing is embedded), so Builder-built
// schemas are accepted.
//
// # Configuration
//
//   - [WithSchemaID]: set the document's "$id". Omitted entirely when unset —
//     "$id" is optional in draft 2020-12 and there is no meaningful default
//     URI for an arbitrary schema.
//
// # Error Conditions
//
// [Marshal] returns an error (never partial or invalid output) when:
//
//   - a $defs key collision cannot be resolved by schema-qualification
//     (including a generated EDGE_ key colliding with a literal type name);
//   - the emitted document fails the self-check — invalid JSON or an
//     unresolvable $ref (each a generator bug).
//
// # Thread Safety
//
// [Marshal] is safe for concurrent use. It allocates fresh state per call
// and shares no mutable package state.
//
// # Annotations
//
// Schema annotations do not affect the emitted document. They describe
// store-level concerns — index shape, write-once behaviour — and this package
// emits a contract about which instance DATA is valid, which annotations
// deliberately do not constrain. A schema that gains its first annotation emits
// byte-identical JSON Schema.
//
// They do reach the generator indirectly: a property inherited from several
// ancestors carries the union of their annotations, so the merged view yields a
// synthesized copy rather than the declared *Property. The $defs lookup
// therefore keys through [github.com/simon-lentz/yammm/schema.Property.Origin].
//
// # Dependencies
//
//	adapter/jschema  ──imports──▶  schema, location
//
// jschema imports only public yammm packages and the standard library —
// no internal/* (the adapter-layer carve-out documented in adapter/doc.go
// stays gogen-only), no instance/graph, no diag, and no third-party
// modules. The contract-alignment test suite carries the package's one
// test-only dependency, a JSON Schema validator used to prove yammm and the
// emitted schema agree.
package jschema
