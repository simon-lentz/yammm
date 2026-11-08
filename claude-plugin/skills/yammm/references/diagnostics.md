# Diagnostics Reference

yammm reports problems as structured diagnostics in a `diag.Result`. An exported function that returns one returns it last, after its value when it has one: `schema.Load` returns `(Schema, diag.Result)`, and `Graph.Add` returns a `diag.Result` alone. Each issue has a stable error code, a severity and a message, and a source location where one applies. Some entry points return a Go `error` instead, among them `gogen.Marshal` and `markdown.Marshal`. `graph.BatchAssembler`'s `Add`, `AddValid` and `AddValidOrMerge` return an `error` too; when the cause is a failed result, the error carries that `diag.Result`, and `diag.AsContextualError` recovers it.

---

## How Diagnostics Work

### diag.Result

The primary container for diagnostic output. Check results with:

```go
result.OK()             // true if no Fatal or Error issues
result.HasErrors()      // true if any Fatal or Error issues
result.HasWarnings()    // true if any Warning issues
result.HasCode(code)    // true if any issue carries the given code
result.Err()            // nil if OK, or *diag.ResultError
result.Issues()         // iterator over all issues
result.Errors()         // iterator over Fatal + Error issues only
result.TruncationNote() // dropped-issues one-liner when the limit was hit
```

### Contextual Wrap (ContextualError)

`result.WithContext(tag)` converts a failed Result into an `error` — a `*diag.ContextualError` carrying the tag plus the full Result — and returns `nil` when the Result is OK, so it slots directly into `return result.WithContext("load users")`. Recover the diagnostics downstream with `errors.As` or `diag.AsContextualError(err, fallbackTag)`. `diag.IsImportDeclarationCode(code)` classifies import-*declaration* codes (the import line itself is wrong) apart from import-*resolution* codes.

### diag.Issue

Each issue contains:

| Field | Description |
| ----- | ----------- |
| `Code()` | Stable identifier (e.g., `E_CONSTRAINT_FAIL`) |
| `Severity()` | Fatal, Error, Warning, Info, or Hint |
| `Message()` | Human-readable description |
| `Span()` | Source location in `.yammm` file (if applicable) |
| `Path()` | Instance data path (JSONPath-like, if applicable) |
| `Hint()` | Optional resolution suggestion |

### Severity Levels

| Level | Meaning | Stops pipeline? |
| ----- | ------- | --------------- |
| Fatal | Unrecoverable failure | Yes |
| Error | Invalid input or state | Yes (via `HasErrors()`) |
| Warning | Likely problem, not blocking | No |
| Info | Informational note | No |
| Hint | Suggestion for improvement | No |

---

## Diagnostic Completeness

One load pass reports every *independent* error in a schema and its import closure -- each exactly once:

- **All import failures are reported**, each at its own declaration, save `E_IMPORT_NOT_ALLOWED`, one issue at the first import; a broken import does not hide its siblings or the schema's own semantic errors. A shared broken import (diamond graphs) is compiled once: its own errors appear once, plus one `E_UPSTREAM_FAIL` per import declaration that names it.
- **References through a failed import are deferred, not re-blamed.** The import failure is the single root-cause diagnostic; `extends`, relation targets, and property datatypes reached through that alias stay silent until the import is fixed. A qualifier that names no declared import at all is a genuine `E_UNKNOWN_TYPE`.
- **An alias binds once (keep-first).** A repeated alias is reported once (`E_DUPLICATE_IMPORT`) and the later declaration is inert; references resolve against the first binding.
- **`LoadString` / markdown blocks**: the imports-not-allowed rejection (`E_IMPORT_NOT_ALLOWED`) does not suppress the source's other diagnostics.
- **Truncation is visible**: one load stores at most 100 issues. Past that cap, the CLI's text output appends a dropped-issues note, and the JSON output carries `limitReached` / `droppedCount`.

The all-or-nothing contract is unchanged: any error still yields a zero schema.

---

## Decision Tree: "I Got an Error"

1. **Parse/syntax error** -- A schema that does not parse draws `E_SYNTAX`: see the Syntax section below. A data file that does not parse draws `E_DATA_PARSE`: see the Instance Data and Validation section.
2. **Import error** -- See the Import section below. Check paths, aliases, circular dependencies.
3. **Schema compilation error** -- See the Schema Compilation section. Type conflicts, missing types, invalid constraints.
4. **Validation error** -- See the Instance Data and Validation section. `E_TYPE_MISMATCH`, `E_CONSTRAINT_FAIL`, `E_MISSING_REQUIRED`, etc.
5. **Invariant error** -- `E_INVARIANT_FAIL` (expression returned false) or `E_EVAL_ERROR` (expression bug).
6. **Graph error** -- a code of the Graph table below, such as `E_DUPLICATE_PK` (duplicate key) or `E_UNRESOLVED_REQUIRED` (missing required association).
7. **Snapshot error** -- See the Snapshot section. Corrupt, incompatible, or dangling references.
8. **Adapter error** -- Neo4j-specific codes in the Adapter section.

---

## Error Code Catalog

### Sentinel

| Code | Meaning |
| ---- | ------- |
| `E_INTERNAL` | Unexpected internal failure (likely a bug) |
| `E_COMMAND_FAILED` | A command-line failure that is not itself a diagnostic, carried into a `--format json` document; its `exit_code` detail is the process exit code |
| `E_CONTEXT_CANCELLED` | Operation cancelled via context |

### Syntax

| Code | Meaning |
| ---- | ------- |
| `E_SYNTAX` | Syntax error in `.yammm` source |

### Import

| Code | Meaning |
| ---- | ------- |
| `E_IMPORT_RESOLVE` | Import path could not be resolved |
| `E_IMPORT_CYCLE` | Circular import dependency |
| `E_INVALID_ALIAS` | An import with no `as` derives a reserved keyword from its path as its alias. An `as` alias the grammar refuses, a reserved keyword included, is `E_SYNTAX` |
| `E_PATH_ESCAPE` | Import path escapes allowed directory |
| `E_IMPORT_NOT_ALLOWED` | Imports not permitted in this context |
| `E_DUPLICATE_IMPORT` | One schema imported more than once, or two imports that share one alias |
| `E_IMPORT_ALIAS_COLLISION` | Import alias collides with a local type or datatype name |

### Schema Compilation

| Code | Meaning |
| ---- | ------- |
| `E_INHERIT_CYCLE` | Inheritance chain contains a cycle |
| `E_UNKNOWN_PROPERTY` | Referenced property not found on its type |
| `E_DUPLICATE_PROPERTY` | Property defined more than once on a type, or an edge property defined more than once on a relation |
| `E_DUPLICATE_RELATION` | Relation defined more than once on a type |
| `E_CASE_COLLISION` | Two property names of a type, or two edge-property names of an association, differ only by case |
| `E_PROPERTY_RELATION_COLLISION` | A property's lower-cased name equals a relation's field name |
| `E_INVALID_ASSOCIATION_TARGET` | Association targets an invalid type, or a part type declares or inherits an association |
| `E_INVALID_COMPOSITION_TARGET` | Composition targets an invalid type |
| `E_INVALID_CONSTRAINT` | Constraint definition is invalid |
| `E_INVALID_INVARIANT` | An invariant's expression or message is invalid |
| `E_DUPLICATE_INVARIANT` | One invariant message declared twice on a type |
| `E_INVARIANT_CONFLICT` | Two inherited definitions of one invariant message differ (a literal with no value matches any literal of its kind) |
| `E_REVERSE_CLAUSE_REMOVED` | Schema carries the reverse clause removed in v0.15.0 (v0.15+) |
| `E_INVALID_NAME` | A name the schema refuses: a relation name that is not UPPER_SNAKE, an empty schema name, a property or relation field named `self`, or an invariant reading a relation in a casing other than its name or field name |
| `E_UPSTREAM_FAIL` | Imported schema failed to compile |
| `E_PROPERTY_CONFLICT` | Conflicting property definitions from inheritance |
| `E_UNKNOWN_TYPE` | Referenced type or datatype not found (`extends`, relation target, a property datatype, or a datatype declaration's reference such as a `List` element) |
| `E_DUPLICATE_TYPE` | Type name defined multiple times |
| `E_DUPLICATE_SCHEMA` | Two schemas in one load declare one name, and both compile with no error. A schema with an error takes no name |
| `E_RELATION_COLLISION` | A type carries two relation definitions under one field name that do not merge: an inherited one that differs from another it carries, or an association and a composition (two relations one type declares under one exact name are `E_DUPLICATE_RELATION`) |
| `E_LIST_ON_EDGE` | List type used in relationship property |
| `E_INVALID_PRIMARY_KEY_TYPE` | Type not allowed as primary key |
| `E_NO_PRIMARY_KEY` | Concrete type declares or inherits no primary key |
| `E_LOAD_IO_FAILURE` | I/O error during schema loading, or a load whose arguments or options the loader refuses |
| `E_LOAD_MODULE_ROOT_MALFORMED` | A `yammm.mod` module-root marker holds content other than comment lines, is not a regular file, is larger than the size limit, or is a link that resolves to no file |
| `E_UNKNOWN_ANNOTATION` | Annotation name the built-in registry holds at neither placement (one it holds only at the other placement is `E_INVALID_ANNOTATION`) |
| `E_UNKNOWN_ANNOTATION_TARGET` | Annotation property-reference argument names no property of the type |
| `E_INVALID_ANNOTATION` | Annotation placement, arity, argument-kind, keyword, or duplicate violation, or conflicting annotations a type inherits for one property |
| `E_INVALID_ANNOTATION_TARGET` | Annotation attached to an ineligible property |
| `W_ANNOTATION_SHADOWED` | A re-declaration silently drops an inherited property's annotations (warning) |
| `W_TIMESTAMP_LOSSY_FORMAT` | `Timestamp["layout"]` declares a layout that cannot reproduce an instant, so a value stored through it can read back as another instant (Warning, v0.13+) |

### Instance Data and Validation

| Code | Meaning |
| ---- | ------- |
| `E_INSTANCE_TYPE_NOT_FOUND` | Type referenced in instance data not found |
| `E_ABSTRACT_TYPE` | Attempt to instantiate an abstract type, an empty batch under its name included |
| `E_PART_TYPE_DIRECT` | Attempt to directly instantiate a part type, an empty batch under its name included |
| `E_TYPE_MISMATCH` | Value has wrong type for its property |
| `E_MISSING_REQUIRED` | Required property is missing |
| `E_UNKNOWN_FIELD` | Unexpected field in instance data |
| `E_CONSTRAINT_FAIL` | A value fails its declared constraint |
| `E_INVARIANT_FAIL` | Invariant expression evaluated to false |
| `E_EVAL_ERROR` | Error during expression evaluation |
| `E_MISSING_FK_TARGET` | Foreign key target is missing |
| `E_PARTIAL_COMPOSITE_FK` | Partial composite foreign key |
| `E_UNKNOWN_EDGE_FIELD` | Unknown field in edge data |
| `E_EDGE_SHAPE_MISMATCH` | An edge's or a composition's value has a shape its relation does not take; at `graph.NewFromSnapshot` under another schema, an `"empty"` unresolved record under a `(one)` association |
| `E_UNRESOLVED_REQUIRED_COMPOSITION` | Required composition is unresolved |
| `E_COMPOSITION_DEPTH_EXCEEDED` | Composed nesting deeper than the bound (32) |
| `E_INVALID_TYPE_TAG` | A JSON top-level key is not a type name |
| `E_DATA_PARSE` | A data file `yammm check` or `yammm snapshot save` reads is not well formed, is not one object, holds a type name whose value is not an array of objects, states one member name twice in one object, or nests deeper than the decoder's 10,000-level limit |
| `E_CASE_FOLD_COLLISION` | Input fields collide after case-folding |

### Graph

| Code | Meaning |
| ---- | ------- |
| `E_DUPLICATE_PK` | Duplicate primary key: two instances added to a graph, two roots whose stored keys the key rule both accepts, in a snapshot or a `.ys` document, or two roots of a data command's input, one of them refused. A read under a schema counts only roots of a type that can hold one |
| `E_DUPLICATE_COMPOSED_PK` | Two children of one `(many)` composition share a primary key, or a `(one)` composition holds several |
| `E_UNRESOLVED_REQUIRED` | Required association is unresolved |
| `E_GRAPH_TYPE_NOT_FOUND` | A type identity an imported snapshot states that the schema does not resolve |
| `E_GRAPH_INVALID_COMPOSITION` | Invalid composition in graph operations |
| `E_GRAPH_MISSING_PK` | A root, or an imported snapshot's types entry, whose type declares no primary key |
| `E_GRAPH_CARDINALITY` | A `(one)` association holds more than one record: a merge at `BatchAssembler.AddValidOrMerge` that would add a second target, several edges and unresolved records no check refused, in a snapshot or `.ys` document (v0.15+) |
| `E_GRAPH_UNKNOWN_RELATION` | Instance data or an association record under a relation name the type does not declare in that slot, or an association record naming a target type the schema resolves, other than the declared one (v0.15+) |
| `E_GRAPH_ABSTRACT_TYPE` | An imported snapshot's root or types entry naming a type the importing schema makes abstract (v0.15+) |
| `E_GRAPH_INVALID_PK` | A keyed type's primary key empty, of the wrong arity, with a key property absent or null, or disagreeing with its own key properties; an association target key of the wrong arity; or a key stored on a composed child whose type declares no primary key (v0.15+) |

### Snapshot

| Code | Meaning |
| ---- | ------- |
| `E_SNAPSHOT_MALFORMED` | `.ys` file not valid JSON, a wrong outer shape (the four top-level keys in order, each once and none null, nothing after them, and no object repeating a member name or spelling a declared one in another case), or content a structural rule refuses, such as an undeclared name, a stored key its key properties contradict, a key or target key component `graph.ParseKey` cannot read back, a target key of the wrong arity, an undocumented reason, a record contradicting itself, records `graph.Add` would not derive, a duplicate with no conflict or another one, a duplicate stating a parent or a relation, a string holding invalid UTF-8 or half a surrogate pair, or a header past `MaxHeaderSize`; from `snapshot.Marshal`, an indent that is not JSON whitespace, a metadata key or value that is not valid UTF-8, or a provenance source name or raw path that is not valid UTF-8; from `snapshot.UpdateMetadata`, such metadata too, or a document whose first byte is not `{` |
| `E_SNAPSHOT_UNSUPPORTED_VERSION` | Format version not recognized |
| `E_SNAPSHOT_UNSUPPORTED_FEATURE` | Unrecognized feature flag in header |
| `E_SNAPSHOT_INCOMPATIBLE_SCHEMA` | Schema structural hash mismatch |
| `E_SNAPSHOT_UNKNOWN_TYPE` | A `.ys` file's types table names a type the schema's import closure does not declare |
| `E_SNAPSHOT_TYPE_MISMATCH` | A type row that contradicts its position: a root's that is not its group's, a duplicate instance's that is not its record's, or a composed child's or association record's target row that is not the relation's declared target |
| `E_SNAPSHOT_DANGLING_REFERENCE` | An edge target, a duplicate's conflict, or an unresolved record's source names no root instance; none for an address a refused unit states |
| `E_SNAPSHOT_INVALID_COMPOSED` | Composed child carries edges, or composed children stand under a name the parent's type does not declare as a composition |
| `E_SNAPSHOT_INVALID_ROOT` | An instances group, empty or not, or a duplicate record names a type that cannot be a root: abstract, part, or no primary key |
| `W_SNAPSHOT_VALUE_DROPPED` | A write path held a value it could not write at that position and left it out |
| `E_SNAPSHOT_COMPOSED_ON_DUPLICATE` | Duplicate record has composed children |
| `E_SNAPSHOT_EDGES_ON_DUPLICATE` | Duplicate record has edges |
| `E_SNAPSHOT_DEPTH_EXCEEDED` | Composed nesting exceeds depth limit (32) |
| `E_SNAPSHOT_INTEGRITY_MISMATCH` | Integrity hash doesn't match content |
| `E_SNAPSHOT_UNSUPPORTED_HASH_ALGORITHM` | Schema hash algorithm not recognized — Error on body-reading surfaces, Warning on header-only reads (v0.15+) |
| `W_SNAPSHOT_PATH_FALLBACK` | Provenance path could not be parsed (Warning) |
| `E_SNAPSHOT_IO` | A file that fails to open during `snapshot.ScanDir`, a directory `ScanDirSlice` fails to read (v0.3+), or a reader `snapshot.HeaderOnlyRead` reads failing with an error that is not a truncation, Fatal (unreleased; on the line after v0.21.0, not in v0.21.1) |
| `E_UPDATE_METADATA_BODY_OFFSET` | `snapshot.UpdateMetadata` body-offset tracker could not resolve the reused-body byte range (v0.3+) |
| `W_UPDATE_METADATA_FALLBACK` | `snapshot.UpdateMetadataOrReMarshal` fell back from the fast path to `Load + Marshal` (Warning, v0.3+) |
| `W_SNAPSHOT_VALUE_NONCONFORMING` | A root's or a composed child's stored `Timestamp`, `Date` or `UUID` value, or a list of them at any depth, does not conform to its schema constraint. Reported only under `snapshot.WithValueConformance`, and not a full re-validation (Warning, v0.13+) |
| `W_SNAPSHOT_UNRESOLVED_REQUIRED` | A loaded document carries an unresolved record, one no structural check refused, for an association the schema declares required (read from the schema, never the record); reported only under `snapshot.WithRevalidation`, at that option's severity (v0.15+) |
| `W_SNAPSHOT_PATH_EXTENSION` | A snapshot was written to a path that does not end in `.ys`; the write succeeded, and a reader that discovers snapshots by extension will not find it |

### Adapter

| Code | Adapter | Meaning |
| ---- | ------- | ------- |
| `E_NEO4J_LABEL_COLLISION` | Neo4j | Two types produce the same Neo4j label |
| `E_NEO4J_INVALID_IDENTIFIER` | Neo4j | Name not valid as Neo4j identifier |
| `E_NEO4J_UNSUPPORTED_TYPE` | Neo4j | Constraint kind has no Neo4j type mapping |
| `E_NEO4J_UNKNOWN_PROPERTY` | Neo4j | An index annotation names a property the type does not have |
| `E_NEO4J_INVALID_INDEX_TARGET` | Neo4j | An index annotation names a property whose type cannot carry it (`@index` or `@@index` on a non-scalar, `@vector` on a non-Vector or a non-positive dimension, `@fulltext` or `@@fulltext` on a non-text property) |
| `W_NEO4J_NODE_KEY_UNSUPPORTED` | Neo4j | `WithNodeKeyConstraints(true)` combined with `WithEdition(Community)`; NODE KEY is Enterprise-only, so UNIQUE is emitted for primary keys instead (Warning, v0.9.1+) |
| `W_NEO4J_EDITION_CONSTRAINT_OMITTED` | Neo4j | `WithEdition(Community)` dropped NOT NULL and PROPERTY_TYPE constraints the schema declares; reported once per call with a count per omitted kind (Warning, v0.14+) |
| `W_NEO4J_INDEXES_UNREADABLE` | Neo4j | A comparison could not read the database's indexes, so the half needing them did not run; the constraint half is complete, but a declaration whose name an index already holds reads as a create that will not take effect (Warning) |
