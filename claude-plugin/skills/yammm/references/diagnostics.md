# Diagnostics Reference

yammm reports problems as structured diagnostics. Every call that can fail returns an `error` last, after its value when it has one, or `false` last where the caller needs no reason: `schema.Load` returns `(Schema, error)`, and `Graph.Add` returns an `error` alone. `snapshot.ScanDir` yields an error only when the scan stops, holding a file's own failure in `ScanEntry.Err`. A call that judges its input returns its failures as a `*diag.ResultError`, whose `Result` holds them. Each issue has a stable error code, a severity and a message, and a source location where one applies. Some calls return a plain `error` for their own refusals, among them `gogen.Marshal`, `markdown.Marshal`, `neo4j.NodeQueries`, `neo4j.EdgeQueries` and `neo4j.ParseConstraints`, and a plain error can wrap a `*diag.ResultError`, as `NodeQueries`' does when `cypher.Config.CheckLabels` refuses the schema's labels; each call's doc names what it returns.

---

## How Diagnostics Work

### diag.ResultError and diag.Result

A failure is a `*diag.ResultError`: the Fatal and Error issues one call drew. Read it through any `fmt.Errorf` `%w` tag and through `errors.Join`:

```go
errors.Is(err, diag.E_CONSTRAINT_FAIL)   // the failure saw this code, stored or dropped
re, ok := errors.AsType[*diag.ResultError](err)
re.Result.HasCode(code)    // true if any retained issue carries the given code
re.Result.SeverityCounts() // counts by severity, over every issue seen
re.Result.Issues()         // iterator over the retained issues
re.Result.Errors()         // iterator over Fatal + Error issues only
re.Result.TruncationNote() // dropped-issues one-liner when the limit was hit
```

`slog.Any("diagnostic", re)` logs the failure through `ResultError.LogValue`: its first code, its counts, its truncation and one object per issue. Log `re`, not a tagged error, which `slog` writes as its text.

### Warnings

A Warning, Info or Hint is never in an error. A call that can draw one takes a `WithWarnings(func(diag.Issue))` option and calls it once per issue, in the order found, before it returns: every schema load (`schema.WithWarnings`), `snapshot.Unmarshal` and `snapshot.Verify` (`snapshot.WithWarnings`), `cypher.Config.Constraints` (`cypher.WithWarnings`) and `neo4j.Diff` (`neo4j.WithWarnings`). Without the option such issues are discarded.

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
| Error | Invalid input or state | Yes (in the call's error) |
| Warning | Likely problem, not blocking | No |
| Info | Informational note | No |
| Hint | Suggestion for improvement | No |

---

## Diagnostic Completeness

One load pass reports every *independent* error in a schema and its import closure -- each exactly once:

- **All import failures are reported**, each at its own declaration, save `E_IMPORT_NOT_ALLOWED`, one issue at the first import the parser keeps; a broken import does not hide its siblings, and the schema's own types and datatypes are still judged: a check of one is withheld only when it reads a reference through the failed import, whether the declaration's own, one in a member or a datatype it reads, or one in a type's `extends` chain. A shared broken import (diamond graphs) is compiled once: its own errors appear once, plus one `E_UPSTREAM_FAIL` per import declaration that names it.
- **References through a failed import are deferred, not re-blamed.** The import failure is the single root-cause diagnostic; `extends`, relation targets, property datatypes, and a datatype's own `List` element (`type Codes = List<lib.Code>`) reached through that alias stay silent until the import is fixed. A qualifier that names no declared import at all is a genuine `E_UNKNOWN_TYPE`.
- **An alias binds once (keep-first).** Each later declaration of an alias is reported (`E_DUPLICATE_IMPORT`) and is inert; references resolve against the first binding.
- **A name binds once (keep-first).** Types and datatypes share one namespace. Each later declaration of a name draws `E_DUPLICATE_TYPE` and is not completed: until the duplicate is gone its body draws no issue but the parser's. A reference that reads a declaration refused because the other kind holds the name stays silent.
- **The inheritance merge reports no conflict on a member of unknown type.** A property or edge property has an unknown type when the parser could not build the type or when the type reads a name that did not resolve; that cause is already reported, once. On each type a property reaches with such a definition, the property's definitions draw no `E_PROPERTY_CONFLICT`. On each type a relation reaches with an unresolved target or such an edge property, the definitions of that relation kind draw no `E_RELATION_COLLISION`. The answer is the same in any order of the `extends` clause.
- **`LoadString` / markdown blocks**: the imports-not-allowed rejection (`E_IMPORT_NOT_ALLOWED`) does not end the analysis. The rejected aliases are deferred as a failed import's is, and the source's other diagnostics are reported by the same rule.
- **Truncation is visible**: a load's error holds at most 100 of the failures the load collected, and a Fatal that ends the load joins them past that number; a warning takes no slot. The CLI keeps at most 100 issues of one load, warnings and failures together. Past a cap, the CLI's text output appends a dropped-issues note, and the JSON output carries `limitReached` / `droppedCount`.

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
8. **Neo4j error** -- Neo4j-specific codes in the Neo4j section.

---

## Error Code Catalog

### Sentinel

| Code | Meaning |
| ---- | ------- |
| `E_INTERNAL` | Unexpected internal failure (likely a bug) |
| `E_COMMAND_FAILED` | A command-line failure that is not itself a diagnostic, carried into a `--json` document; its `exit_code` detail is the process exit code |
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
| `E_INVALID_SUPERTYPE` | An `extends` clause names a type of a kind the subtype may not extend: a supertype is abstract or of the subtype's own kind |
| `E_UNKNOWN_PROPERTY` | Referenced property not found on its type |
| `E_DUPLICATE_PROPERTY` | Property defined more than once on a type, or an edge property defined more than once on a relation |
| `E_DUPLICATE_RELATION` | Relation defined more than once on a type |
| `E_CASE_COLLISION` | Two property names of a type, or two edge-property names of an association, differ only by case |
| `E_PROPERTY_RELATION_COLLISION` | A property's lower-cased name equals a relation's field name |
| `E_INVALID_ASSOCIATION_TARGET` | Association targets an invalid type, or a part type declares or inherits an association |
| `E_INVALID_COMPOSITION_TARGET` | Composition targets an invalid type |
| `E_COMPOSITION_CYCLE` | A cycle of compositions whose every link is required, which no finite instance satisfies; make one link optional |
| `E_INVALID_CONSTRAINT` | A constraint definition is invalid; at Warning, a `-_` bound whose minus sign has no effect |
| `E_INVALID_INVARIANT` | An invariant's expression or message is invalid |
| `E_DUPLICATE_INVARIANT` | One invariant message declared twice on a type |
| `E_INVARIANT_CONFLICT` | Two inherited definitions of one invariant message differ (a literal with no value matches any literal of its kind) |
| `E_INVALID_NAME` | A name the schema refuses: a relation name that is not UPPER_SNAKE, a schema name that is not an identifier (an ASCII letter, then letters, digits and underscores), a property named `self`, or an invariant reading a relation in a casing other than its declared name |
| `E_UPSTREAM_FAIL` | Imported schema failed to compile |
| `E_PROPERTY_CONFLICT` | A type's own declaration of a property does not narrow an inherited one, or, with none, no inherited declaration narrows every other |
| `E_UNKNOWN_TYPE` | Referenced type or datatype not found (`extends`, relation target, a property datatype, or a datatype declaration's reference such as a `List` element) |
| `E_DUPLICATE_TYPE` | A name declared more than once in one schema. Types and datatypes share one namespace, so a type and a datatype of one name draw it too. The issue stands at each later declaration and names the first |
| `E_DUPLICATE_SCHEMA` | Two schemas in one load declare one name, and both compile with no error. A schema with an error takes no name |
| `E_RELATION_COLLISION` | A type carries two relation definitions under one field name that do not merge: an inherited one that differs from another it carries, or an association and a composition (two relations one type declares under one exact name are `E_DUPLICATE_RELATION`) |
| `E_INVALID_EDGE_PROPERTY_TYPE` | A relationship property of a type an edge does not hold: a `List` or a `Vector`, written or through a datatype |
| `E_INVALID_PRIMARY_KEY_TYPE` | Type not allowed as primary key |
| `E_NO_PRIMARY_KEY` | Concrete type declares or inherits no primary key |
| `E_LOAD_IO_FAILURE` | I/O error during schema loading, or a load whose arguments or option values the loader refuses (an option a load does not take is refused with an error that carries no code) |
| `E_LOAD_MODULE_ROOT_MALFORMED` | A `yammm.mod` module-root marker holds content other than comment lines, is not a regular file, is larger than the size limit, or is a link that resolves to no file |
| `E_UNKNOWN_ANNOTATION` | Annotation name the built-in registry holds at neither placement (one it holds only at the other placement is `E_INVALID_ANNOTATION`) |
| `E_UNKNOWN_ANNOTATION_TARGET` | Annotation property-reference argument names no property of the type |
| `E_INVALID_ANNOTATION` | Annotation placement, arity, argument-kind, keyword, or duplicate violation, or conflicting annotations a type inherits for one property |
| `E_INVALID_ANNOTATION_TARGET` | Annotation attached to an ineligible property |
| `W_ANNOTATION_SHADOWED` | A re-declaration silently drops an inherited property's annotations (warning) |

### Instance Data and Validation

| Code | Meaning |
| ---- | ------- |
| `E_INSTANCE_TYPE_NOT_FOUND` | Type referenced in instance data not found |
| `E_ABSTRACT_TYPE` | Attempt to instantiate an abstract type, an empty batch under its name included |
| `E_PART_TYPE_DIRECT` | Attempt to directly instantiate a part type, an empty batch under its name included |
| `E_TYPE_MISMATCH` | Value has wrong type for its property |
| `E_MISSING_REQUIRED` | Required property is missing |
| `E_UNKNOWN_FIELD` | Unexpected field in instance data, a member's name spelled in another case included |
| `E_CONSTRAINT_FAIL` | A value fails its declared constraint |
| `E_INVARIANT_FAIL` | Invariant expression evaluated to false, at validation or at `Graph.Check` on a root `Graph.AddOrMerge` grew |
| `E_EVAL_ERROR` | Error during expression evaluation |
| `E_MISSING_FK_TARGET` | An edge object holds none of its target's `_target_` foreign-key fields |
| `E_PARTIAL_COMPOSITE_FK` | Partial composite foreign key |
| `E_UNKNOWN_EDGE_FIELD` | Unknown field in edge data, a foreign-key field or edge property spelled in another case included |
| `E_DUPLICATE_EDGE` | A `(many)` association lists one target key twice for one source, where no other check refused either listing |
| `E_EDGE_SHAPE_MISMATCH` | An edge's or a composition's value has a shape its relation does not take |
| `E_UNRESOLVED_REQUIRED_COMPOSITION` | Required composition is unresolved |
| `E_COMPOSITION_DEPTH_EXCEEDED` | Composed nesting deeper than the bound (32) |
| `E_INVALID_TYPE_TAG` | A JSON top-level key is not a type name |
| `E_DATA_PARSE` | A data file `yammm check` or `yammm snapshot save` reads is not well formed, is not one object, holds a type name whose value is not an array of objects, states one member name twice in one object, holds a string that is not valid UTF-8 or holds a `\u` escape that is half of no surrogate pair, or nests deeper than the decoder's 10,000-level limit |

### Graph

| Code | Meaning |
| ---- | ------- |
| `E_DUPLICATE_PK` | Duplicate primary key: two instances added to a graph, two roots whose stored keys the key rule both accepts, in a `.ys` document, or two roots of a data command's input, one of them refused. A read under a schema counts only roots of a type that can hold one |
| `E_DUPLICATE_COMPOSED_PK` | Two children of one `(many)` composition share a primary key, or a `(one)` composition holds several |
| `E_DUPLICATE_EDGE` | A `(many)` association lists one target key twice for one source, where no other check refused either listing; `snapshot.Unmarshal` and `snapshot.Verify` refuse two edges, or two `target_missing` records, of one source that name one target as `E_SNAPSHOT_MALFORMED` where no other check refused the repeating listing, one sound root (of a type that can hold a root, its stored key admitted by the key rule) holds the source's address alone, and an edge's target's, and no root stands at a record's target and no refused unit states it; elsewhere the issue at the listing, the source or the target stands for the repeat. `snapshot.Stat` reads no schema and does not judge it |
| `E_UNRESOLVED_REQUIRED` | Required association is unresolved |
| `E_GRAPH_INVALID_COMPOSITION` | Invalid composition in graph operations |
| `E_GRAPH_MISSING_PK` | An instance added at a root whose type declares no primary key |
| `E_GRAPH_CARDINALITY` | A `(one)` association holds more than one record: a merge at `Graph.AddOrMerge` that would add a second target (v0.21.1+), or several edges and unresolved records no check refused in a `.ys` document (unreleased; on the line after v0.21.0, not in v0.21.1). The code dates from v0.15, where `Graph.Add` drew it for a `(one)` association given several targets |
| `E_GRAPH_UNKNOWN_RELATION` | An association record or an empty edge list under a relation name the type does not declare as an association (v0.15+) |

### Snapshot

| Code | Meaning |
| ---- | ------- |
| `E_SNAPSHOT_MALFORMED` | `.ys` file not valid JSON, a wrong outer shape (the four top-level keys in order, each once and none null, nothing after them, and no object repeating a member name or spelling a declared one in another case), or content a structural rule refuses, such as an undeclared name, a stored key its key properties contradict, a key or target key component `graph.ParseKey` cannot read back, a `Timestamp`, `Date` or `UUID` key component spelled other than the text its kind stores (at a stored key, a target key, a record's source key, or a duplicate's key or conflict key), a target key of the wrong arity, an undocumented reason, a record contradicting itself, records `graph.Add` would not derive (two edges or `target_missing` records of one source naming one target under a `(many)` association among them), a duplicate with no conflict or another one, a duplicate stating a parent or a relation, a string holding invalid UTF-8 or half a surrogate pair, a `created_at` that is not an RFC 3339 time or that states a leap second or the zero instant, or a header past `MaxHeaderSize` where `snapshot.ReadHeader` reads; from `snapshot.Marshal`, an indent that is not JSON whitespace, a metadata key or value that is not valid UTF-8, a provenance source name or path that is not valid UTF-8, or a `WithCreatedAt` time with no RFC 3339 form; from `snapshot.UpdateMetadata`, such metadata too |
| `E_SNAPSHOT_UNSUPPORTED_VERSION` | Format version not recognized |
| `E_SNAPSHOT_UNSUPPORTED_FEATURE` | Unrecognized feature flag in header |
| `E_SNAPSHOT_INCOMPATIBLE_SCHEMA` | Schema structural hash mismatch |
| `E_SNAPSHOT_UNKNOWN_TYPE` | A `.ys` file's types table names a type the schema's import closure does not declare |
| `E_SNAPSHOT_TYPE_MISMATCH` | A type row that contradicts its position: a root's that is not its group's, a duplicate instance's that is not its record's, or a composed child's or association record's target row that is not the relation's declared target |
| `E_SNAPSHOT_DANGLING_REFERENCE` | An edge target, a duplicate's conflict, or an unresolved record's source names no root instance; none for an address a refused unit states |
| `E_SNAPSHOT_INVALID_COMPOSED` | Composed child carries edges, or composed children stand under a name the parent's type does not declare as a composition |
| `E_SNAPSHOT_INVALID_ROOT` | An instances group, empty or not, or a duplicate record names a type that cannot be a root: abstract, part, or no primary key |
| `E_SNAPSHOT_COMPOSED_ON_DUPLICATE` | Duplicate record has composed children |
| `E_SNAPSHOT_EDGES_ON_DUPLICATE` | Duplicate record has edges |
| `E_SNAPSHOT_DEPTH_EXCEEDED` | Composed nesting exceeds depth limit (32) |
| `E_SNAPSHOT_INTEGRITY_MISMATCH` | Integrity hash doesn't match content, or is empty, null or absent; `snapshot.UpdateMetadata` refuses such a document too, unless `WithIntegrityCheck(false)` is given |
| `E_SNAPSHOT_UNSUPPORTED_HASH_ALGORITHM` | Schema hash algorithm not recognized — Error from every surface that reads the body; `ReadHeader` and `ScanDir` accept the header and draw nothing, and the CLI reports it as a Warning (v0.15+), except beside another header failure |
| `E_SNAPSHOT_IO` | A file that fails to open during `snapshot.ScanDir`, a directory `yammm snapshot info --dir` fails to read (v0.3+), or a reader `snapshot.ReadHeader` reads failing with an error that is not a truncation, Fatal (unreleased; on the line after v0.21.0, not in v0.21.1) |
| `W_SNAPSHOT_UNRESOLVED_REQUIRED` | A loaded document carries an unresolved record, one no structural check refused, for an association the schema declares required (read from the schema, never the record); reported only under `snapshot.WithRevalidation`, at that option's severity (v0.15+) |
| `W_SNAPSHOT_PATH_EXTENSION` | A snapshot was written to a path that does not end in `.ys`; the write succeeded, and a reader that discovers snapshots by extension will not find it |

### Neo4j

`schema/cypher` registers these codes but `E_NEO4J_INVALID_REMOTE_OBJECT` when it is linked, and `neo4j`, which imports it, registers that one too.

| Code | Meaning |
| ---- | ------- |
| `E_NEO4J_INVALID_IDENTIFIER` | A label setting, schema name or label that breaks the label rule, or a property name that is no Neo4j identifier |
| `E_NEO4J_UNSUPPORTED_TYPE` | Constraint kind has no Neo4j type mapping |
| `E_NEO4J_UNKNOWN_PROPERTY` | An index annotation names a property the type does not have |
| `E_NEO4J_INVALID_INDEX_TARGET` | An index annotation names a property whose type cannot carry it (`@index` or `@@index` on a non-scalar, `@vector` on a non-Vector or a dimension outside 1 to 4096, `@fulltext` or `@@fulltext` on a non-text property) |
| `W_NEO4J_NODE_KEY_UNSUPPORTED` | `cypher.Config{NodeKeys: true, Edition: cypher.Community}` (`--node-keys --edition community`); NODE KEY is Enterprise-only, so UNIQUE is emitted for primary keys instead (Warning, v0.9.1+) |
| `W_NEO4J_EDITION_CONSTRAINT_OMITTED` | The Community edition dropped NOT NULL and PROPERTY_TYPE constraints the schema declares; reported once per call with a count per omitted kind (Warning, v0.14+) |
| `E_NEO4J_INVALID_REMOTE_OBJECT` | `neo4j.Diff` was given a remote row it cannot plan from: a constraint or an index with no name, two rows of one kind under one name, or a property type constraint that reports no `propertyType` |
