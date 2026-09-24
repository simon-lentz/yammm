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
//     associations by their lower-case field name. Unknown fields are
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
//     composition gets maxItems 1 (instance validation refuses a second
//     child, E_DUPLICATE_COMPOSED_PK).
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
//	Pattern[p]            →  {"type":"string","pattern":p'}   (p' rewritten; see below)
//	Pattern[p1, p2]       →  {"type":"string","allOf":[{"pattern":p1'},{"pattern":p2'}]}
//	UUID                  →  {"type":"string","format":"uuid"}
//	Date                  →  {"type":"string","format":"date"}
//	Timestamp             →  {"type":"string","format":"date-time"}
//	Timestamp["layout"]   →  {"type":"string"} + a description carrying the source form
//	Vector[N]             →  number array with minItems/maxItems N
//	List<T>[min, max]     →  {"type":"array","items":<T>} + minItems/maxItems
//	named DataType        →  {"$ref":"#/$defs/<Name>"}
//
// Named DataTypes are rendered faithfully as $refs in every position —
// scalar property, list element at any depth (List<List<FipsCode>> emits
// items of items $ref), edge property, _target_* foreign-key field, and
// inside another DataType's constraint (Codes = List<FipsCode>) — so the
// named constraint is stated once and hover shows its name and
// documentation. Inline enums are inlined (JSON Schema needs no synthesized
// names). A yammm value must match EVERY declared pattern, hence allOf, not
// anyOf.
//
// yammm compiles a pattern as Go's regexp (RE2) does. JSON Schema reads a
// pattern as ECMA-262, and a validator built on Go's regexp reads it as RE2,
// so each pattern is rewritten into syntax both read the same way, with the
// "u" flag ECMA-262 validators such as ajv apply: case folding becomes
// explicit classes, "." becomes [^\n], \s becomes its five ASCII characters,
// POSIX and Unicode classes become ranges, and \d and \w become their ASCII
// ranges. A string of code points matches the rewrite exactly when it matches
// the source. A line anchor under the "m" flag has
// no such form: that pattern is not asserted, and the fragment's description
// carries it in source form.
//
// # Names, $defs, and Imports
//
// The full import closure — the entry schema plus every transitively
// imported schema — is flattened into one self-contained document. $defs
// keys are raw schema names: unqualified where unique across the closure,
// <schemaName>.<Name>-qualified on collision; a collision qualification
// cannot separate (a type and a DataType sharing one name in one schema)
// gives the later claimant, types before DataTypes in declaration order, the
// first free numeric suffix (geo.Region2), as adapter/gogen does. A suffixed
// key can be another entity's natural spelling, which then takes the next
// suffix in turn (a type Region2 beside it becomes geo.Region22). Each declared association gets one
// EDGE_<ownerKey>_<field>_<targetKey> entry, shared by every subtype that
// inherits it; when that key is already taken — by a type so named, or by
// another association whose parts join to the same text — it takes the
// first free numeric suffix (EDGE_A_b_x_C2). A $ref percent-encodes every
// key character a URI fragment cannot hold. Abstract types get no $defs entry — associations must target
// concrete non-part types and compositions must target part types, so
// nothing can ever $ref an abstract type; its members reach the document
// flattened into each subtype.
//
// # Fidelity Caveats
//
// The emitted schema does not reproduce yammm's validation; it diverges in
// the classes below, and each divergence changes only what the editor flags.
// Every yammm data command — `yammm check`, `load`, `export` and
// `snapshot save` — runs all three stages the cases below name: the parse,
// instance validation and graph assembly.
//
// The emitted schema judges each value by its own shape, so no check that
// compares instances, counts depth or evaluates an expression reaches it. In
// each case below the editor accepts the file:
//
//   - Primary-key uniqueness. Graph assembly refuses the second of two
//     instances that share a primary key (E_DUPLICATE_PK). Instance
//     validation refuses two children of one (many) composition that share
//     one (E_DUPLICATE_COMPOSED_PK).
//   - Association resolution. Graph assembly refuses a required association
//     that is absent, that is an empty array, or whose _target_<pk_name>
//     fields name no instance (E_UNRESOLVED_REQUIRED).
//   - Invariants. A type's invariants are not emitted; instance validation
//     refuses an instance that fails one (E_INVARIANT_FAIL).
//   - Composition depth. Instance validation refuses composed children nested
//     deeper than [github.com/simon-lentz/yammm/instance.MaxComposedDepth]
//     (E_COMPOSITION_DEPTH_EXCEEDED); the emitted $refs nest without limit.
//
// The other divergences are in how one value or one name is read:
//
//   - Numbers. yammm's Integer is an integer literal — no '.', 'e' or 'E' —
//     that an int64 holds, and its Float is any literal read to its nearest
//     float64 that is finite. JSON Schema's "integer" is a number with no
//     fractional part, whatever its spelling. So the editor accepts on an
//     Integer a whole decimal or exponent literal such as 5.0 or 1e2, and an
//     integer outside int64 such as 9223372036854775808 or
//     -9223372036854775809, and on a Float a literal no float64 holds such as
//     1e400; yammm refuses each (E_TYPE_MISMATCH). A validator that reads a
//     number exactly judges a Float's bounds on the literal's own value, where
//     yammm judges its nearest float64, so it flags 1.00000000000000001 under
//     Float[_, 1.0], which yammm accepts. Validators differ among themselves:
//     vscode-json-languageservice reads "integer" by the absence of '.', so it
//     flags 5.0 and accepts 5e-1, whose value is fractional.
//   - Formats. The uuid, date and date-time keywords are annotations by
//     default under draft 2020-12, so an editor that does not assert formats
//     accepts a malformed UUID, Date or Timestamp that instance validation
//     refuses (E_CONSTRAINT_FAIL). A validator that asserts formats reads each
//     by its own grammar, which differs from yammm's parsers in both
//     directions: it flags a UUID without hyphens, which yammm accepts, and it
//     accepts a date-time with a lowercase t or z, or with a leap second
//     (23:59:60), which yammm refuses.
//   - Null for an optional property. yammm reads a null value as an absent
//     one. The emitted schema gives each property its value's type alone, so
//     the editor flags the null.
//   - Field-name case. The instance layer matches property names, relation
//     field names and _target_<pk_name> fields case-insensitively by default.
//     The emitted schema targets canonical spellings: a case-variant file
//     still validates under yammm, and the editor flags it toward the
//     canonical form.
//   - A repeated member name. adapter/json refuses an object — the envelope
//     or an instance — that names one member twice (E_ADAPTER_PARSE); a JSON
//     Schema validator reads one of the two values and accepts the object.
//   - An empty array under a key the envelope does not name. yammm accepts
//     an empty array under a part type's or an abstract type's name; the
//     envelope names neither, so the editor flags the key.
//   - Patterns. Each pattern is rewritten into syntax RE2 and ECMA-262 read
//     alike (see Constraint Mapping). A pattern holding a line anchor under
//     the "m" flag is not asserted, so the editor accepts what yammm may
//     reject, never the reverse. The rewrite needs the "u" flag, which a
//     validator such as ajv applies by default. A validator without it reads
//     every class by UTF-16 code unit: it refuses a pattern whose class holds
//     a range above U+FFFF (a Unicode class such as \p{L} rewrites to one),
//     and a class, [^\n] or [\s\S] matches half of a character above U+FFFF.
//     No form states such a range to RE2 and to both ECMA-262 modes, since
//     RE2 has no surrogate code units. A JSON string may also hold a lone
//     surrogate ("\ud800"): yammm's reader decodes it to U+FFFD and an
//     ECMA-262 validator keeps the surrogate, so the two can disagree on that
//     value.
//   - Custom Timestamp layouts. A Timestamp["layout"] constraint is not
//     expressible as a JSON Schema assertion; the emitted fragment is a
//     plain string whose description carries the source form. The editor
//     accepts what yammm may reject, never the reverse.
//
// # Editor Wiring
//
// Generate the document next to the data files it describes, then reference
// it. For YAML data files via yaml-language-server:
//
//	# yaml-language-server: $schema=./fleet.schema.json
//
// yammm reads no YAML data file — its data commands read JSON, JSONC and
// CSV — so a YAML file wired this way gets the editor's checks alone.
//
// For JSON data files, a json.schemas mapping in VS Code settings that
// associates the document with a file glob. Do not add a "$schema" member to
// a JSON data file: the envelope admits no member but a type name, and
// adapter/json refuses the key as a type tag (E_INVALID_TYPE_TAG).
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
//   - the schema is not completed as Preconditions requires: an association
//     target or a datatype reference does not resolve;
//   - a generator bug: a member with no registered $defs key, a constraint
//     the mapper cannot render, or an emitted document that fails the
//     self-check (invalid JSON or an unresolvable $ref).
//
// No name collision fails: every $defs key that is already taken takes a
// numeric suffix.
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
//	adapter/jschema  ──imports──▶  schema
//
// jschema imports only public yammm packages and the standard library —
// no internal/* (the adapter-layer carve-out documented in adapter/doc.go
// stays gogen-only), no instance/graph, no diag, and no third-party
// modules. The contract-alignment test suite carries the package's one
// test-only dependency, a JSON Schema validator used to prove yammm and the
// emitted schema agree.
package jschema
