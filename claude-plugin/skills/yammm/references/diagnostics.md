# Diagnostics Reference

Every yammm operation returns `(value, diag.Result)` with structured diagnostics. Each issue has a stable error code, severity, message, and source location.

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

- **All import failures are reported**, each at its own declaration; a broken import does not hide its siblings or the schema's own semantic errors. A shared broken import (diamond graphs) is compiled once: its own errors appear once, plus one `E_UPSTREAM_FAIL` per importer.
- **References through a failed import are deferred, not re-blamed.** The import failure is the single root-cause diagnostic; `extends`, relation targets, and property datatypes reached through that alias stay silent until the import is fixed. A qualifier that names no declared import at all is a genuine `E_UNKNOWN_TYPE`.
- **An alias binds once (keep-first).** A repeated alias is reported once (`E_DUPLICATE_IMPORT`) and the later declaration is inert; references resolve against the first binding.
- **`LoadString` / markdown blocks**: the imports-not-allowed rejection (`E_IMPORT_NOT_ALLOWED`) no longer suppresses the source's other diagnostics.
- **Truncation is visible**: past the issue limit (default 100), the CLI's text output appends a dropped-issues note, and the JSON output carries `limit` / `limitReached` / `droppedCount`.

The all-or-nothing contract is unchanged: any error still yields a nil schema.

---

## Decision Tree: "I Got an Error"

1. **Parse/syntax error** -- See `E_SYNTAX` in the Syntax section below.
2. **Import error** -- See the Import section below. Check paths, aliases, circular dependencies.
3. **Schema compilation error** -- See the Schema Compilation section. Type conflicts, missing types, invalid constraints.
4. **Validation error** -- See the Instance Validation section. `E_TYPE_MISMATCH`, `E_CONSTRAINT_FAIL`, `E_MISSING_REQUIRED`, etc.
5. **Invariant error** -- `E_INVARIANT_FAIL` (expression returned false) or `E_EVAL_ERROR` (expression bug).
6. **Graph error** -- `E_DUPLICATE_PK` (duplicate key) or `E_UNRESOLVED_REQUIRED` (missing required association).
7. **Snapshot error** -- See the Snapshot section. Corrupt, incompatible, or dangling references.
8. **Adapter error** -- `E_ADAPTER_PARSE`, `E_CSV_COERCE`, or Neo4j-specific codes in the Adapter section.

---

## Error Code Catalog

### Sentinel

| Code | Meaning |
| ---- | ------- |
| `E_INTERNAL` | Unexpected internal failure (likely a bug) |
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
| `E_INVALID_ALIAS` | Import alias is not a valid identifier |
| `E_PATH_ESCAPE` | Import path escapes allowed directory |
| `E_IMPORT_NOT_ALLOWED` | Imports not permitted in this context |
| `E_DUPLICATE_IMPORT` | Same schema imported under multiple aliases |
| `E_IMPORT_ALIAS_COLLISION` | Import alias collides with a local type name |

### Schema Compilation

| Code | Meaning |
| ---- | ------- |
| `E_INHERIT_CYCLE` | Inheritance chain contains a cycle |
| `E_UNKNOWN_PROPERTY` | Referenced property not found on its type |
| `E_DUPLICATE_PROPERTY` | Property defined more than once on a type |
| `E_DUPLICATE_RELATION` | Relation defined more than once on a type |
| `E_CASE_COLLISION` | Property/relation names differ only by case |
| `E_PROPERTY_RELATION_COLLISION` | Property and relation share the same name |
| `E_RELATION_NORMALIZATION_COLLISION` | Relation names collide after normalization |
| `E_RESERVED_PREFIX` | Name uses a reserved prefix |
| `E_INVALID_ASSOCIATION_TARGET` | Association targets an invalid type |
| `E_INVALID_COMPOSITION_TARGET` | Composition targets an invalid type |
| `E_INVALID_CONSTRAINT` | Constraint definition is invalid |
| `E_INVALID_INVARIANT` | Invariant expression is invalid |
| `E_INVALID_NAME` | Identifier has invalid format |
| `E_UPSTREAM_FAIL` | Imported schema failed to compile |
| `E_PROPERTY_CONFLICT` | Conflicting property definitions from inheritance |
| `E_UNKNOWN_TYPE` | Referenced type or datatype not found (`extends`, relation target, or property datatype) |
| `E_DUPLICATE_TYPE` | Type name defined multiple times |
| `E_RELATION_COLLISION` | Relations collide after normalization |
| `E_MISSING_SOURCE_ID` | Required SourceID is missing |
| `E_INVALID_SYNTHETIC_ID` | Synthetic SourceID has invalid format |
| `E_LIST_ON_EDGE` | List type used in relationship property |
| `E_INVALID_PRIMARY_KEY_TYPE` | Type not allowed as primary key |
| `E_NO_PRIMARY_KEY` | Concrete type declares or inherits no primary key |
| `E_LOAD_IO_FAILURE` | I/O error during schema loading |
| `E_UNKNOWN_ANNOTATION` | Annotation name not in the built-in registry for its placement |
| `E_UNKNOWN_ANNOTATION_TARGET` | Annotation property-reference argument names no property of the type |
| `E_INVALID_ANNOTATION` | Annotation placement, arity, argument-kind, keyword, or duplicate violation |
| `E_INVALID_ANNOTATION_TARGET` | Annotation attached to an ineligible property |
| `W_ANNOTATION_SHADOWED` | A re-declaration silently drops an inherited property's annotations (warning) |

### Instance Validation

| Code | Meaning |
| ---- | ------- |
| `E_INSTANCE_TYPE_NOT_FOUND` | Type referenced in instance data not found |
| `E_ABSTRACT_TYPE` | Attempt to instantiate an abstract type |
| `E_PART_TYPE_DIRECT` | Attempt to directly instantiate a part type |
| `E_TYPE_MISMATCH` | Value has wrong type for its property |
| `E_MISSING_REQUIRED` | Required property is missing |
| `E_MISSING_PRIMARY_KEY` | Primary key property is missing |
| `E_UNKNOWN_FIELD` | Unexpected field in instance data |
| `E_CONSTRAINT_FAIL` | Constraint check failed (bounds, pattern, enum) |
| `E_INVARIANT_FAIL` | Invariant expression evaluated to false |
| `E_EVAL_ERROR` | Error during expression evaluation |
| `E_MISSING_FK_TARGET` | Foreign key target is missing |
| `E_PARTIAL_COMPOSITE_FK` | Partial composite foreign key |
| `E_UNKNOWN_EDGE_FIELD` | Unknown field in edge data |
| `E_EDGE_SHAPE_MISMATCH` | Edge has wrong shape |
| `E_UNRESOLVED_REQUIRED_COMPOSITION` | Required composition is unresolved |
| `E_COMPOSITION_NOT_FOUND` | Referenced composition not found |
| `E_INVALID_TYPE_TAG` | `$type` tag has invalid format |
| `E_CASE_FOLD_COLLISION` | Input fields collide after case-folding |

### Graph

| Code | Meaning |
| ---- | ------- |
| `E_DUPLICATE_PK` | Duplicate primary key in graph |
| `E_DUPLICATE_COMPOSED_PK` | Duplicate composed child primary key |
| `E_UNRESOLVED_REQUIRED` | Required association is unresolved |
| `E_GRAPH_TYPE_NOT_FOUND` | Type not found in graph operations |
| `E_GRAPH_PARENT_NOT_FOUND` | Parent node not found for composed child |
| `E_GRAPH_INVALID_COMPOSITION` | Invalid composition in graph operations |
| `E_GRAPH_MISSING_PK` | Primary key missing in graph operations |

### Snapshot

| Code | Meaning |
| ---- | ------- |
| `E_SNAPSHOT_MALFORMED` | `.ys` file not valid JSON or wrong structure |
| `E_SNAPSHOT_UNSUPPORTED_VERSION` | Format version not recognized |
| `E_SNAPSHOT_UNSUPPORTED_FEATURE` | Unrecognized feature flag in header |
| `E_SNAPSHOT_INCOMPATIBLE_SCHEMA` | Schema structural hash mismatch |
| `E_SNAPSHOT_UNKNOWN_TYPE` | Type in `.ys` file not in schema |
| `E_SNAPSHOT_TYPE_MISMATCH` | Instances section inconsistent with the types table |
| `E_SNAPSHOT_DANGLING_REFERENCE` | Edge target references non-existent instance |
| `E_SNAPSHOT_INVALID_COMPOSED` | Composed child carries edges (invalid) |
| `E_SNAPSHOT_COMPOSED_ON_DUPLICATE` | Duplicate record has composed children |
| `E_SNAPSHOT_EDGES_ON_DUPLICATE` | Duplicate record has edges |
| `E_SNAPSHOT_DEPTH_EXCEEDED` | Composed nesting exceeds depth limit (32) |
| `E_SNAPSHOT_INTEGRITY_MISMATCH` | Integrity hash doesn't match content |
| `E_SNAPSHOT_UNSUPPORTED_HASH_ALGORITHM` | Schema hash algorithm not recognized (Warning) |
| `E_SNAPSHOT_PATH_FALLBACK` | Provenance path could not be parsed (Warning) |
| `E_SNAPSHOT_IO` | Per-file I/O failure during `snapshot.ScanDir` iteration (v0.3+) |
| `E_UPDATE_METADATA_BODY_OFFSET` | `snapshot.UpdateMetadata` body-offset tracker could not resolve the reused-body byte range (v0.3+) |
| `W_UPDATE_METADATA_FALLBACK` | `snapshot.UpdateMetadataOrReMarshal` fell back from the fast path to `Load + Marshal` (Warning, v0.3+) |

### Adapter

| Code | Adapter | Meaning |
| ---- | ------- | ------- |
| `E_ADAPTER_PARSE` | All | Format-specific parsing error |
| `E_CSV_COERCE` | CSV | Cell value could not be coerced to expected type |
| `E_NEO4J_LABEL_COLLISION` | Neo4j | Two types produce the same Neo4j label |
| `E_NEO4J_INVALID_IDENTIFIER` | Neo4j | Name not valid as Neo4j identifier |
| `E_NEO4J_UNSUPPORTED_TYPE` | Neo4j | Constraint kind has no Neo4j type mapping |
| `E_NEO4J_UNKNOWN_PROPERTY` | Neo4j | An index annotation names a property the type does not have |
| `E_NEO4J_INVALID_INDEX_TARGET` | Neo4j | An index annotation names a property whose type cannot carry it (`@index` on a non-scalar, `@vector` on a non-Vector or a non-positive dimension, `@fulltext` on a non-text property) |
| `W_NEO4J_NODE_KEY_UNSUPPORTED` | Neo4j | `WithNodeKeyConstraints(true)` combined with `WithEdition(Community)`; NODE KEY is Enterprise-only, so UNIQUE is emitted for primary keys instead (Warning, v0.9.1+) |
