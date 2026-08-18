package diag

import "sync"

// CodeCategory represents the semantic domain of an error code.
//
// Categories represent the semantic domain of an error, not necessarily the
// API layer that emits it. Most codes are emitted exclusively by their
// category's layer, but some codes represent cross-cutting concerns.
type CodeCategory uint8

const (
	// CategorySentinel is for sentinel codes like E_INTERNAL and E_CONTEXT_CANCELLED.
	CategorySentinel CodeCategory = iota

	// CategorySchema is for schema compilation errors.
	CategorySchema

	// CategorySyntax is for parse/lexer errors.
	CategorySyntax

	// CategoryImport is for import resolution errors.
	CategoryImport

	// CategoryInstance is for instance validation errors.
	CategoryInstance

	// CategoryGraph is for graph-layer errors.
	CategoryGraph

	// CategoryAdapter is for format adapter parsing errors.
	CategoryAdapter

	// CategorySnapshot is for snapshot persistence errors.
	CategorySnapshot
)

// String returns a human-readable label for the category.
func (c CodeCategory) String() string {
	switch c {
	case CategorySentinel:
		return "sentinel"
	case CategorySchema:
		return "schema"
	case CategorySyntax:
		return "syntax"
	case CategoryImport:
		return "import"
	case CategoryInstance:
		return "instance"
	case CategoryGraph:
		return "graph"
	case CategoryAdapter:
		return "adapter"
	case CategorySnapshot:
		return "snapshot"
	default:
		return "unknown"
	}
}

// Code is a stable programmatic identifier for an Issue.
//
// Error codes are stable identifiers that tools can match on, even when
// message text changes. Codes are created and registered via NewCode().
// Built-in codes are defined in this package; adapter and consumer
// packages define their own domain-specific codes using the same function.
//
// Code.String() values are globally unique across all registered codes.
// The CodeCategory is informational metadata for filtering and grouping.
type Code struct {
	value string
	cat   CodeCategory
}

// String returns the code's string representation (e.g., "E_CASE_COLLISION").
func (c Code) String() string {
	return c.value
}

// Category returns the programmatic category for this code.
func (c Code) Category() CodeCategory {
	return c.cat
}

// IsZero reports whether the code is unset.
func (c Code) IsZero() bool {
	return c.value == ""
}

var (
	mu       sync.Mutex
	registry []Code
	seen     = make(map[string]bool)
)

// NewCode creates and registers a new diagnostic Code with the given
// identifier and category. Registered codes appear in [AllCodes] and
// [CodesByCategory].
//
// Panics if a code with the same identifier has already been registered.
// Use a package-scoped prefix to avoid collisions (e.g., "E_NEO4J_*").
//
// Codes should be defined as package-level variables so that
// registration happens at program startup:
//
//	var E_MY_ERROR = diag.NewCode("E_MY_ERROR", diag.CategoryAdapter)
func NewCode(value string, cat CodeCategory) Code {
	mu.Lock()
	defer mu.Unlock()
	if seen[value] {
		panic("diag: duplicate code " + value)
	}
	seen[value] = true
	c := Code{value: value, cat: cat}
	registry = append(registry, c)
	return c
}

// Sentinel codes.
var (
	// E_INTERNAL indicates an unexpected invariant failure (internal bug indicator).
	// Use for conditions that should never occur in correct code.
	E_INTERNAL = NewCode("E_INTERNAL", CategorySentinel)

	// E_CONTEXT_CANCELLED indicates the operation was cancelled via context.
	// Used across all packages when ctx.Err() returns a non-nil error.
	E_CONTEXT_CANCELLED = NewCode("E_CONTEXT_CANCELLED", CategorySentinel)
)

// Schema codes.
var (
	// E_INHERIT_CYCLE indicates an inheritance chain contains a cycle.
	E_INHERIT_CYCLE = NewCode("E_INHERIT_CYCLE", CategorySchema)

	// E_UNKNOWN_PROPERTY indicates a referenced property cannot be found on its type.
	E_UNKNOWN_PROPERTY = NewCode("E_UNKNOWN_PROPERTY", CategorySchema)

	// E_DUPLICATE_PROPERTY indicates a property is defined more than once on a type.
	E_DUPLICATE_PROPERTY = NewCode("E_DUPLICATE_PROPERTY", CategorySchema)

	// E_DUPLICATE_RELATION indicates a relation is defined more than once on a type.
	E_DUPLICATE_RELATION = NewCode("E_DUPLICATE_RELATION", CategorySchema)

	// E_CASE_COLLISION indicates property/relation names differ only by case.
	E_CASE_COLLISION = NewCode("E_CASE_COLLISION", CategorySchema)

	// E_PROPERTY_RELATION_COLLISION indicates a property and relation have the same name.
	E_PROPERTY_RELATION_COLLISION = NewCode("E_PROPERTY_RELATION_COLLISION", CategorySchema)

	// E_RELATION_NORMALIZATION_COLLISION indicates relation names collide after normalization.
	E_RELATION_NORMALIZATION_COLLISION = NewCode("E_RELATION_NORMALIZATION_COLLISION", CategorySchema)

	// E_RESERVED_PREFIX indicates a name uses a reserved prefix.
	E_RESERVED_PREFIX = NewCode("E_RESERVED_PREFIX", CategorySchema)

	// E_INVALID_ASSOCIATION_TARGET indicates an association targets an invalid type.
	E_INVALID_ASSOCIATION_TARGET = NewCode("E_INVALID_ASSOCIATION_TARGET", CategorySchema)

	// E_INVALID_COMPOSITION_TARGET indicates a composition targets an invalid type.
	E_INVALID_COMPOSITION_TARGET = NewCode("E_INVALID_COMPOSITION_TARGET", CategorySchema)

	// E_INVALID_CONSTRAINT indicates a constraint definition is invalid.
	E_INVALID_CONSTRAINT = NewCode("E_INVALID_CONSTRAINT", CategorySchema)

	// E_INVALID_INVARIANT indicates an invariant expression is invalid.
	E_INVALID_INVARIANT = NewCode("E_INVALID_INVARIANT", CategorySchema)

	// E_INVALID_NAME indicates an identifier has an invalid format.
	E_INVALID_NAME = NewCode("E_INVALID_NAME", CategorySchema)

	// E_UPSTREAM_FAIL indicates an imported schema failed to compile.
	E_UPSTREAM_FAIL = NewCode("E_UPSTREAM_FAIL", CategorySchema)

	// E_PROPERTY_CONFLICT indicates conflicting property definitions from inheritance.
	E_PROPERTY_CONFLICT = NewCode("E_PROPERTY_CONFLICT", CategorySchema)

	// E_UNKNOWN_TYPE indicates a referenced type cannot be found.
	E_UNKNOWN_TYPE = NewCode("E_UNKNOWN_TYPE", CategorySchema)

	// E_DUPLICATE_TYPE indicates a type name is defined multiple times.
	E_DUPLICATE_TYPE = NewCode("E_DUPLICATE_TYPE", CategorySchema)

	// E_RELATION_COLLISION indicates relations collide after name normalization.
	E_RELATION_COLLISION = NewCode("E_RELATION_COLLISION", CategorySchema)

	// E_MISSING_SOURCE_ID indicates a required SourceID is missing.
	E_MISSING_SOURCE_ID = NewCode("E_MISSING_SOURCE_ID", CategorySchema)

	// E_INVALID_SYNTHETIC_ID indicates a synthetic SourceID has an invalid format.
	E_INVALID_SYNTHETIC_ID = NewCode("E_INVALID_SYNTHETIC_ID", CategorySchema)

	// E_LIST_ON_EDGE indicates a List type was used in a relationship property.
	E_LIST_ON_EDGE = NewCode("E_LIST_ON_EDGE", CategorySchema)

	// E_INVALID_PRIMARY_KEY_TYPE indicates a type not allowed as a primary key.
	// Only String, UUID, Date, and Timestamp are permitted as primary key types.
	E_INVALID_PRIMARY_KEY_TYPE = NewCode("E_INVALID_PRIMARY_KEY_TYPE", CategorySchema)

	// E_NO_PRIMARY_KEY indicates a concrete (non-abstract, non-part) type that
	// declares or inherits no primary key. A node needs identity to be added to a
	// graph or referenced by an association, so such a type is rejected at load
	// rather than at graph-construction time.
	E_NO_PRIMARY_KEY = NewCode("E_NO_PRIMARY_KEY", CategorySchema)

	// E_LOAD_IO_FAILURE indicates an I/O error during schema loading.
	// Covers file read failures, path resolution errors, and other filesystem issues.
	E_LOAD_IO_FAILURE = NewCode("E_LOAD_IO_FAILURE", CategorySchema)

	// E_UNKNOWN_ANNOTATION indicates an annotation name absent from the built-in
	// registry for its placement (@name on a property, @@name on a type).
	E_UNKNOWN_ANNOTATION = NewCode("E_UNKNOWN_ANNOTATION", CategorySchema)

	// E_UNKNOWN_ANNOTATION_TARGET indicates a property-reference argument of an
	// annotation (such as a @@index member) that names no property of the type.
	// It shares a failure class with E_UNKNOWN_PROPERTY on a different construct.
	E_UNKNOWN_ANNOTATION_TARGET = NewCode("E_UNKNOWN_ANNOTATION_TARGET", CategorySchema)

	// E_INVALID_ANNOTATION indicates a structural annotation violation: wrong
	// placement, wrong arity, a bad argument kind or keyword, or a duplicate.
	E_INVALID_ANNOTATION = NewCode("E_INVALID_ANNOTATION", CategorySchema)

	// E_INVALID_ANNOTATION_TARGET indicates an annotation attached to an
	// ineligible property — e.g. @index on a non-scalar or sole primary key,
	// @vector on a non-Vector, or @writeOnce on a primary-key member.
	E_INVALID_ANNOTATION_TARGET = NewCode("E_INVALID_ANNOTATION_TARGET", CategorySchema)

	// W_ANNOTATION_SHADOWED indicates that a type's own property re-declaration
	// (identical or narrowing) shadows an inherited property carrying
	// annotations without re-stating them, so those annotations silently drop
	// from the re-declaring type. Warning severity: it surfaces the resulting
	// write-shape / index-shape regression without blocking the load. One
	// warning is emitted per shadowed ancestor.
	W_ANNOTATION_SHADOWED = NewCode("W_ANNOTATION_SHADOWED", CategorySchema)
)

// Syntax codes.
var (
	// E_SYNTAX indicates a syntax error in the schema source.
	E_SYNTAX = NewCode("E_SYNTAX", CategorySyntax)
)

// Import codes.
var (
	// E_IMPORT_RESOLVE indicates an import path could not be resolved.
	E_IMPORT_RESOLVE = NewCode("E_IMPORT_RESOLVE", CategoryImport)

	// E_IMPORT_CYCLE indicates a cycle exists in the import dependency graph.
	E_IMPORT_CYCLE = NewCode("E_IMPORT_CYCLE", CategoryImport)

	// E_INVALID_ALIAS indicates an import alias is not a valid identifier.
	E_INVALID_ALIAS = NewCode("E_INVALID_ALIAS", CategoryImport)

	// E_PATH_ESCAPE indicates an import path escapes the allowed directory.
	E_PATH_ESCAPE = NewCode("E_PATH_ESCAPE", CategoryImport)

	// E_IMPORT_NOT_ALLOWED indicates imports are not allowed in this context.
	E_IMPORT_NOT_ALLOWED = NewCode("E_IMPORT_NOT_ALLOWED", CategoryImport)

	// E_DUPLICATE_IMPORT indicates the same schema is imported multiple times
	// under different aliases.
	E_DUPLICATE_IMPORT = NewCode("E_DUPLICATE_IMPORT", CategoryImport)

	// E_IMPORT_ALIAS_COLLISION indicates an import alias collides with a local
	// type name.
	E_IMPORT_ALIAS_COLLISION = NewCode("E_IMPORT_ALIAS_COLLISION", CategoryImport)
)

// Instance validation codes.
var (
	// E_INSTANCE_TYPE_NOT_FOUND indicates a type referenced in instance data cannot be found.
	E_INSTANCE_TYPE_NOT_FOUND = NewCode("E_INSTANCE_TYPE_NOT_FOUND", CategoryInstance)

	// E_ABSTRACT_TYPE indicates an attempt to instantiate an abstract type.
	E_ABSTRACT_TYPE = NewCode("E_ABSTRACT_TYPE", CategoryInstance)

	// E_PART_TYPE_DIRECT indicates an attempt to directly instantiate a part type.
	E_PART_TYPE_DIRECT = NewCode("E_PART_TYPE_DIRECT", CategoryInstance)

	// E_TYPE_MISMATCH indicates a value has the wrong type.
	E_TYPE_MISMATCH = NewCode("E_TYPE_MISMATCH", CategoryInstance)

	// E_MISSING_REQUIRED indicates a required property is missing.
	E_MISSING_REQUIRED = NewCode("E_MISSING_REQUIRED", CategoryInstance)

	// E_MISSING_PRIMARY_KEY indicates a primary key property is missing.
	E_MISSING_PRIMARY_KEY = NewCode("E_MISSING_PRIMARY_KEY", CategoryInstance)

	// E_UNKNOWN_FIELD indicates an unexpected field in instance data.
	E_UNKNOWN_FIELD = NewCode("E_UNKNOWN_FIELD", CategoryInstance)

	// E_CONSTRAINT_FAIL indicates a constraint check failed.
	E_CONSTRAINT_FAIL = NewCode("E_CONSTRAINT_FAIL", CategoryInstance)

	// E_INVARIANT_FAIL indicates an invariant check failed.
	E_INVARIANT_FAIL = NewCode("E_INVARIANT_FAIL", CategoryInstance)

	// E_EVAL_ERROR indicates an error during expression evaluation.
	E_EVAL_ERROR = NewCode("E_EVAL_ERROR", CategoryInstance)

	// E_MISSING_FK_TARGET indicates a foreign key target is missing.
	E_MISSING_FK_TARGET = NewCode("E_MISSING_FK_TARGET", CategoryInstance)

	// E_PARTIAL_COMPOSITE_FK indicates a partial composite foreign key.
	E_PARTIAL_COMPOSITE_FK = NewCode("E_PARTIAL_COMPOSITE_FK", CategoryInstance)

	// E_UNKNOWN_EDGE_FIELD indicates an unknown field in edge data.
	E_UNKNOWN_EDGE_FIELD = NewCode("E_UNKNOWN_EDGE_FIELD", CategoryInstance)

	// E_EDGE_SHAPE_MISMATCH indicates an edge has the wrong shape.
	E_EDGE_SHAPE_MISMATCH = NewCode("E_EDGE_SHAPE_MISMATCH", CategoryInstance)

	// E_UNRESOLVED_REQUIRED_COMPOSITION indicates a required composition is unresolved.
	E_UNRESOLVED_REQUIRED_COMPOSITION = NewCode("E_UNRESOLVED_REQUIRED_COMPOSITION", CategoryInstance)

	// E_COMPOSITION_NOT_FOUND indicates a referenced composition cannot be found.
	E_COMPOSITION_NOT_FOUND = NewCode("E_COMPOSITION_NOT_FOUND", CategoryInstance)

	// E_INVALID_TYPE_TAG indicates a $type tag has an invalid format.
	E_INVALID_TYPE_TAG = NewCode("E_INVALID_TYPE_TAG", CategoryInstance)

	// E_CASE_FOLD_COLLISION indicates multiple input fields collide after case-folding.
	// This occurs when non-strict mode is enabled and the input contains multiple
	// field names that differ only in case (e.g., "Name" and "name").
	E_CASE_FOLD_COLLISION = NewCode("E_CASE_FOLD_COLLISION", CategoryInstance)
)

// Adapter codes.
var (
	// E_ADAPTER_PARSE indicates a format-specific parsing error.
	E_ADAPTER_PARSE = NewCode("E_ADAPTER_PARSE", CategoryAdapter)
)

// Graph codes.
var (
	// E_DUPLICATE_PK indicates a duplicate primary key in the graph.
	E_DUPLICATE_PK = NewCode("E_DUPLICATE_PK", CategoryGraph)

	// E_DUPLICATE_COMPOSED_PK indicates a duplicate composed child primary key.
	E_DUPLICATE_COMPOSED_PK = NewCode("E_DUPLICATE_COMPOSED_PK", CategoryGraph)

	// E_UNRESOLVED_REQUIRED indicates a required association is unresolved.
	E_UNRESOLVED_REQUIRED = NewCode("E_UNRESOLVED_REQUIRED", CategoryGraph)

	// E_GRAPH_TYPE_NOT_FOUND indicates a type referenced in graph operations cannot be found.
	E_GRAPH_TYPE_NOT_FOUND = NewCode("E_GRAPH_TYPE_NOT_FOUND", CategoryGraph)

	// E_GRAPH_PARENT_NOT_FOUND indicates a parent node cannot be found.
	E_GRAPH_PARENT_NOT_FOUND = NewCode("E_GRAPH_PARENT_NOT_FOUND", CategoryGraph)

	// E_GRAPH_INVALID_COMPOSITION indicates an invalid composition in graph operations.
	E_GRAPH_INVALID_COMPOSITION = NewCode("E_GRAPH_INVALID_COMPOSITION", CategoryGraph)

	// E_GRAPH_MISSING_PK indicates a primary key is missing in graph operations.
	E_GRAPH_MISSING_PK = NewCode("E_GRAPH_MISSING_PK", CategoryGraph)
)

// Snapshot persistence codes.
var (
	// E_SNAPSHOT_MALFORMED indicates the .ys file is not valid JSON or has
	// wrong top-level structure (e.g., missing yammm_snapshot header as first key).
	E_SNAPSHOT_MALFORMED = NewCode("E_SNAPSHOT_MALFORMED", CategorySnapshot)

	// E_SNAPSHOT_UNSUPPORTED_VERSION indicates the format version is not recognized.
	E_SNAPSHOT_UNSUPPORTED_VERSION = NewCode("E_SNAPSHOT_UNSUPPORTED_VERSION", CategorySnapshot)

	// E_SNAPSHOT_UNSUPPORTED_FEATURE indicates an unrecognized feature flag in the header.
	// One diagnostic is emitted per unrecognized feature.
	E_SNAPSHOT_UNSUPPORTED_FEATURE = NewCode("E_SNAPSHOT_UNSUPPORTED_FEATURE", CategorySnapshot)

	// E_SNAPSHOT_INCOMPATIBLE_SCHEMA indicates the schema structural hash does not
	// match between the .ys file and the provided schema.
	E_SNAPSHOT_INCOMPATIBLE_SCHEMA = NewCode("E_SNAPSHOT_INCOMPATIBLE_SCHEMA", CategorySnapshot)

	// E_SNAPSHOT_UNKNOWN_TYPE indicates a type name in the .ys file does not exist
	// in the provided schema.
	E_SNAPSHOT_UNKNOWN_TYPE = NewCode("E_SNAPSHOT_UNKNOWN_TYPE", CategorySnapshot)

	// E_SNAPSHOT_TYPE_MISMATCH indicates the instances section is inconsistent
	// with the types table (structural malformation).
	E_SNAPSHOT_TYPE_MISMATCH = NewCode("E_SNAPSHOT_TYPE_MISMATCH", CategorySnapshot)

	// E_SNAPSHOT_DANGLING_REFERENCE indicates an edge target or duplicate conflict
	// references an instance that does not exist in the snapshot.
	E_SNAPSHOT_DANGLING_REFERENCE = NewCode("E_SNAPSHOT_DANGLING_REFERENCE", CategorySnapshot)

	// E_SNAPSHOT_INVALID_COMPOSED indicates a composed child instance carries edges,
	// which violates the composed children invariant (edges are only on root instances).
	E_SNAPSHOT_INVALID_COMPOSED = NewCode("E_SNAPSHOT_INVALID_COMPOSED", CategorySnapshot)

	// E_SNAPSHOT_COMPOSED_ON_DUPLICATE indicates a duplicate record's instance contains
	// composed children, violating the duplicate structural constraint.
	E_SNAPSHOT_COMPOSED_ON_DUPLICATE = NewCode("E_SNAPSHOT_COMPOSED_ON_DUPLICATE", CategorySnapshot)

	// E_SNAPSHOT_EDGES_ON_DUPLICATE indicates a duplicate record's instance contains
	// edges, violating the duplicate structural constraint.
	E_SNAPSHOT_EDGES_ON_DUPLICATE = NewCode("E_SNAPSHOT_EDGES_ON_DUPLICATE", CategorySnapshot)

	// E_SNAPSHOT_DEPTH_EXCEEDED indicates composed nesting exceeds the depth limit (32).
	E_SNAPSHOT_DEPTH_EXCEEDED = NewCode("E_SNAPSHOT_DEPTH_EXCEEDED", CategorySnapshot)

	// E_SNAPSHOT_INTEGRITY_MISMATCH indicates the integrity hash does not match the
	// document content. The file may be corrupted, truncated, or modified.
	E_SNAPSHOT_INTEGRITY_MISMATCH = NewCode("E_SNAPSHOT_INTEGRITY_MISMATCH", CategorySnapshot)

	// E_SNAPSHOT_UNSUPPORTED_HASH_ALGORITHM (Warning) indicates the schema hash
	// algorithm version in the .ys header is not recognized. Schema hash verification
	// is skipped; load proceeds without the schema compatibility check.
	E_SNAPSHOT_UNSUPPORTED_HASH_ALGORITHM = NewCode("E_SNAPSHOT_UNSUPPORTED_HASH_ALGORITHM", CategorySnapshot)

	// E_SNAPSHOT_PATH_FALLBACK (Warning) indicates a provenance path string could
	// not be parsed and fell back to the root path. The original path string is
	// preserved for round-trip fidelity via Provenance.RawPath().
	E_SNAPSHOT_PATH_FALLBACK = NewCode("E_SNAPSHOT_PATH_FALLBACK", CategorySnapshot)

	// --- v0.3.0 additions ---

	// E_SNAPSHOT_IO indicates a filesystem I/O failure encountered during
	// a directory scan — either a dir-level failure (os.ReadDir on
	// snapshot.ScanDir / snapshot.ScanDirSlice) or a per-file failure
	// (os.Open or the underlying file Read on ScanDir's per-file path).
	// Per-file emissions land on ScanEntry.Result so the iterator
	// continues to the next file rather than aborting; dir-level
	// emissions surface on the outer Result returned by ScanDirSlice.
	// The underlying os error is preserved as a detail entry so the
	// operator can recover the concrete cause. Named E_SNAPSHOT_IO (not
	// E_IO) to match the E_SNAPSHOT_* convention under CategorySnapshot;
	// the precedent for a per-category I/O code is E_LOAD_IO_FAILURE
	// under CategorySchema. No new CategoryIO constant is introduced.
	E_SNAPSHOT_IO = NewCode("E_SNAPSHOT_IO", CategorySnapshot)

	// E_UPDATE_METADATA_BODY_OFFSET indicates the header parsed cleanly
	// but snapshot.UpdateMetadata's body-boundary offset tracker could
	// not resolve a valid byte range for the reused body. The input does
	// not match the shape snapshot.Marshal produces; byte-identical
	// recovery via UpdateMetadata is not possible. Consumers using the
	// primitive UpdateMetadata directly fall back to Load + Marshal;
	// consumers using snapshot.UpdateMetadataOrReMarshal get that
	// fallback automatically with a paired W_UPDATE_METADATA_FALLBACK
	// warning on the returned Result.
	E_UPDATE_METADATA_BODY_OFFSET = NewCode("E_UPDATE_METADATA_BODY_OFFSET", CategorySnapshot)

	// W_UPDATE_METADATA_FALLBACK (Warning) indicates that
	// snapshot.UpdateMetadataOrReMarshal fell back from the UpdateMetadata
	// fast path to Load + Marshal because the input triggered a
	// recoverable Fatal code (E_SNAPSHOT_MALFORMED,
	// E_UPDATE_METADATA_BODY_OFFSET, or another non-cancellation Fatal
	// issue). The output bytes are byte-identical to what Marshal would
	// produce; the warning surfaces the path transition so operators can
	// observe fallback frequency and triage persistent cases. Details
	// include a "triggering_codes" entry listing the original Fatal
	// code(s) that caused the fallback.
	//
	// Uses the W_ prefix, inaugurating the convention for
	// Warning-severity codes added from v0.3.0 onward; existing
	// Warning-severity codes (E_SNAPSHOT_UNSUPPORTED_HASH_ALGORITHM,
	// E_SNAPSHOT_PATH_FALLBACK) retain their E_ identifiers for
	// backwards compatibility — severity is carried on the Issue, not
	// the Code, so the prefix is a naming convention rather than a
	// type-enforced property.
	W_UPDATE_METADATA_FALLBACK = NewCode("W_UPDATE_METADATA_FALLBACK", CategorySnapshot)
)

// AllCodes returns all registered diagnostic codes in registration order.
// Built-in codes appear first (registered during diag package initialization),
// followed by consumer-defined codes in package initialization order.
//
// This function is useful for tooling and testing. The returned slice is a
// copy; modifications do not affect the original.
func AllCodes() []Code {
	mu.Lock()
	defer mu.Unlock()
	result := make([]Code, len(registry))
	copy(result, registry)
	return result
}

// CodesByCategory returns registered codes in the given category.
//
// The returned slice is a new allocation; modifications do not affect
// internal state.
func CodesByCategory(cat CodeCategory) []Code {
	mu.Lock()
	defer mu.Unlock()
	var result []Code
	for _, c := range registry {
		if c.cat == cat {
			result = append(result, c)
		}
	}
	return result
}

// IsImportDeclarationCode reports whether code (a diagnostic code's String())
// names an import-DECLARATION diagnostic — a rejected, duplicated, colliding, or
// invalid import alias — as distinct from an import-RESOLUTION diagnostic
// (E_IMPORT_RESOLVE / E_IMPORT_CYCLE / E_PATH_ESCAPE). Both families share
// [CategoryImport], which is too coarse to tell them apart, so this is the
// canonical declaration subset — co-located with the code definitions so a new
// declaration code is added here too.
//
// The LSP markdown surface downgrades the declaration family to Hint in code
// blocks (imports are categorically not processed there); the resolution family
// keeps its severity. Keyed by the code's String() so callers holding only the
// rendered code string can consult the list.
func IsImportDeclarationCode(code string) bool {
	switch code {
	case E_IMPORT_NOT_ALLOWED.String(), E_DUPLICATE_IMPORT.String(),
		E_IMPORT_ALIAS_COLLISION.String(), E_INVALID_ALIAS.String():
		return true
	default:
		return false
	}
}
