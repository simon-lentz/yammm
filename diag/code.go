package diag

// CodeCategory represents the semantic domain of an error code.
//
// Categories represent the semantic domain of an error, not necessarily the
// API layer that emits it. Most codes are emitted exclusively by their
// category's layer, but some codes represent cross-cutting concerns.
type CodeCategory uint8

const (
	// CategorySentinel is for sentinel codes like E_LIMIT_REACHED and E_INTERNAL.
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
	default:
		return "unknown"
	}
}

// Code is a stable programmatic identifier for an Issue.
//
// Error codes are stable identifiers that tools can match on, even when
// message text changes. The Code type uses unexported fields to enforce
// a closed set of valid codes—only codes defined in this package are valid.
//
// Code.String() values are globally unique across all categories. The
// CodeCategory is informational metadata for filtering and grouping.
type Code struct {
	value string
	cat   CodeCategory
}

// String returns the code's string representation (e.g., "E_TYPE_COLLISION").
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

// code is the unexported constructor—callers cannot create arbitrary codes.
func code(value string, cat CodeCategory) Code {
	return Code{value: value, cat: cat}
}

// Sentinel codes.
var (
	// E_LIMIT_REACHED is a sentinel code for explicit limit notification.
	// It does not automatically trigger Result.LimitReached(); use
	// Collector.LimitReached() to check limit status. Callers may inject
	// this code manually when desired.
	E_LIMIT_REACHED = code("E_LIMIT_REACHED", CategorySentinel)

	// E_INTERNAL indicates an unexpected invariant failure (internal bug indicator).
	// Use for conditions that should never occur in correct code.
	E_INTERNAL = code("E_INTERNAL", CategorySentinel)
)

// Schema codes.
var (
	// E_TYPE_COLLISION indicates a type name is already defined.
	E_TYPE_COLLISION = code("E_TYPE_COLLISION", CategorySchema)

	// E_INHERIT_CYCLE indicates an inheritance chain contains a cycle.
	E_INHERIT_CYCLE = code("E_INHERIT_CYCLE", CategorySchema)

	// E_SCHEMA_TYPE_NOT_FOUND indicates a referenced type cannot be found during schema compilation.
	E_SCHEMA_TYPE_NOT_FOUND = code("E_SCHEMA_TYPE_NOT_FOUND", CategorySchema)

	// E_UNKNOWN_PROPERTY indicates a referenced property cannot be found on its type.
	E_UNKNOWN_PROPERTY = code("E_UNKNOWN_PROPERTY", CategorySchema)

	// E_DUPLICATE_PROPERTY indicates a property is defined more than once on a type.
	E_DUPLICATE_PROPERTY = code("E_DUPLICATE_PROPERTY", CategorySchema)

	// E_DUPLICATE_RELATION indicates a relation is defined more than once on a type.
	E_DUPLICATE_RELATION = code("E_DUPLICATE_RELATION", CategorySchema)

	// E_CASE_COLLISION indicates property/relation names differ only by case.
	E_CASE_COLLISION = code("E_CASE_COLLISION", CategorySchema)

	// E_PROPERTY_RELATION_COLLISION indicates a property and relation have the same name.
	E_PROPERTY_RELATION_COLLISION = code("E_PROPERTY_RELATION_COLLISION", CategorySchema)

	// E_RELATION_NORMALIZATION_COLLISION indicates relation names collide after normalization.
	E_RELATION_NORMALIZATION_COLLISION = code("E_RELATION_NORMALIZATION_COLLISION", CategorySchema)

	// E_RESERVED_PREFIX indicates a name uses a reserved prefix.
	E_RESERVED_PREFIX = code("E_RESERVED_PREFIX", CategorySchema)

	// E_INVALID_RELATION indicates a relation definition is invalid.
	E_INVALID_RELATION = code("E_INVALID_RELATION", CategorySchema)

	// E_INVALID_ASSOCIATION_TARGET indicates an association targets an invalid type.
	E_INVALID_ASSOCIATION_TARGET = code("E_INVALID_ASSOCIATION_TARGET", CategorySchema)

	// E_INVALID_COMPOSITION_TARGET indicates a composition targets an invalid type.
	E_INVALID_COMPOSITION_TARGET = code("E_INVALID_COMPOSITION_TARGET", CategorySchema)

	// E_INVALID_CONSTRAINT indicates a constraint definition is invalid.
	E_INVALID_CONSTRAINT = code("E_INVALID_CONSTRAINT", CategorySchema)

	// E_INVALID_INVARIANT indicates an invariant expression is invalid.
	E_INVALID_INVARIANT = code("E_INVALID_INVARIANT", CategorySchema)

	// E_INVALID_NAME indicates an identifier has an invalid format.
	E_INVALID_NAME = code("E_INVALID_NAME", CategorySchema)

	// E_UPSTREAM_FAIL indicates an imported schema failed to compile.
	E_UPSTREAM_FAIL = code("E_UPSTREAM_FAIL", CategorySchema)

	// E_PROPERTY_CONFLICT indicates conflicting property definitions from inheritance.
	E_PROPERTY_CONFLICT = code("E_PROPERTY_CONFLICT", CategorySchema)

	// E_UNKNOWN_TYPE indicates a referenced type cannot be found.
	E_UNKNOWN_TYPE = code("E_UNKNOWN_TYPE", CategorySchema)

	// E_DUPLICATE_TYPE indicates a type name is defined multiple times.
	E_DUPLICATE_TYPE = code("E_DUPLICATE_TYPE", CategorySchema)

	// E_RELATION_COLLISION indicates relations collide after name normalization.
	E_RELATION_COLLISION = code("E_RELATION_COLLISION", CategorySchema)

	// E_MISSING_SOURCE_ID indicates a required SourceID is missing.
	E_MISSING_SOURCE_ID = code("E_MISSING_SOURCE_ID", CategorySchema)

	// E_INVALID_SYNTHETIC_ID indicates a synthetic SourceID has an invalid format.
	E_INVALID_SYNTHETIC_ID = code("E_INVALID_SYNTHETIC_ID", CategorySchema)
)

// Syntax codes.
var (
	// E_SYNTAX indicates a syntax error in the schema source.
	E_SYNTAX = code("E_SYNTAX", CategorySyntax)
)

// Import codes.
var (
	// E_IMPORT_RESOLVE indicates an import path could not be resolved.
	E_IMPORT_RESOLVE = code("E_IMPORT_RESOLVE", CategoryImport)

	// E_IMPORT_CYCLE indicates a cycle exists in the import dependency graph.
	E_IMPORT_CYCLE = code("E_IMPORT_CYCLE", CategoryImport)

	// E_INVALID_ALIAS indicates an import alias is not a valid identifier.
	E_INVALID_ALIAS = code("E_INVALID_ALIAS", CategoryImport)

	// E_PATH_ESCAPE indicates an import path escapes the allowed directory.
	E_PATH_ESCAPE = code("E_PATH_ESCAPE", CategoryImport)

	// E_IMPORT_NOT_ALLOWED indicates imports are not allowed in this context.
	E_IMPORT_NOT_ALLOWED = code("E_IMPORT_NOT_ALLOWED", CategoryImport)

	// E_DUPLICATE_IMPORT indicates the same schema is imported multiple times
	// under different aliases.
	E_DUPLICATE_IMPORT = code("E_DUPLICATE_IMPORT", CategoryImport)

	// E_IMPORT_ALIAS_COLLISION indicates an import alias collides with a local
	// type name.
	E_IMPORT_ALIAS_COLLISION = code("E_IMPORT_ALIAS_COLLISION", CategoryImport)
)

// Instance validation codes.
var (
	// E_INSTANCE_TYPE_NOT_FOUND indicates a type referenced in instance data cannot be found.
	E_INSTANCE_TYPE_NOT_FOUND = code("E_INSTANCE_TYPE_NOT_FOUND", CategoryInstance)

	// E_ABSTRACT_TYPE indicates an attempt to instantiate an abstract type.
	E_ABSTRACT_TYPE = code("E_ABSTRACT_TYPE", CategoryInstance)

	// E_PART_TYPE_DIRECT indicates an attempt to directly instantiate a part type.
	E_PART_TYPE_DIRECT = code("E_PART_TYPE_DIRECT", CategoryInstance)

	// E_TYPE_MISMATCH indicates a value has the wrong type.
	E_TYPE_MISMATCH = code("E_TYPE_MISMATCH", CategoryInstance)

	// E_MISSING_REQUIRED indicates a required property is missing.
	E_MISSING_REQUIRED = code("E_MISSING_REQUIRED", CategoryInstance)

	// E_MISSING_PRIMARY_KEY indicates a primary key property is missing.
	E_MISSING_PRIMARY_KEY = code("E_MISSING_PRIMARY_KEY", CategoryInstance)

	// E_UNKNOWN_FIELD indicates an unexpected field in instance data.
	E_UNKNOWN_FIELD = code("E_UNKNOWN_FIELD", CategoryInstance)

	// E_CONSTRAINT_FAIL indicates a constraint check failed.
	E_CONSTRAINT_FAIL = code("E_CONSTRAINT_FAIL", CategoryInstance)

	// E_INVARIANT_FAIL indicates an invariant check failed.
	E_INVARIANT_FAIL = code("E_INVARIANT_FAIL", CategoryInstance)

	// E_EVAL_ERROR indicates an error during expression evaluation.
	E_EVAL_ERROR = code("E_EVAL_ERROR", CategoryInstance)

	// E_UNKNOWN_BUILTIN indicates an unknown builtin function was referenced.
	E_UNKNOWN_BUILTIN = code("E_UNKNOWN_BUILTIN", CategoryInstance)

	// E_MISSING_FK_TARGET indicates a foreign key target is missing.
	E_MISSING_FK_TARGET = code("E_MISSING_FK_TARGET", CategoryInstance)

	// E_PARTIAL_COMPOSITE_FK indicates a partial composite foreign key.
	E_PARTIAL_COMPOSITE_FK = code("E_PARTIAL_COMPOSITE_FK", CategoryInstance)

	// E_UNKNOWN_EDGE_FIELD indicates an unknown field in edge data.
	E_UNKNOWN_EDGE_FIELD = code("E_UNKNOWN_EDGE_FIELD", CategoryInstance)

	// E_EDGE_SHAPE_MISMATCH indicates an edge has the wrong shape.
	E_EDGE_SHAPE_MISMATCH = code("E_EDGE_SHAPE_MISMATCH", CategoryInstance)

	// E_UNRESOLVED_REQUIRED_COMPOSITION indicates a required composition is unresolved.
	E_UNRESOLVED_REQUIRED_COMPOSITION = code("E_UNRESOLVED_REQUIRED_COMPOSITION", CategoryInstance)

	// E_COMPOSITION_NOT_FOUND indicates a referenced composition cannot be found.
	E_COMPOSITION_NOT_FOUND = code("E_COMPOSITION_NOT_FOUND", CategoryInstance)

	// E_MISSING_TYPE_TAG indicates a $type tag is missing.
	E_MISSING_TYPE_TAG = code("E_MISSING_TYPE_TAG", CategoryInstance)

	// E_INVALID_TYPE_TAG indicates a $type tag has an invalid format.
	E_INVALID_TYPE_TAG = code("E_INVALID_TYPE_TAG", CategoryInstance)

	// E_CASE_FOLD_COLLISION indicates multiple input fields collide after case-folding.
	// This occurs when non-strict mode is enabled and the input contains multiple
	// field names that differ only in case (e.g., "Name" and "name").
	E_CASE_FOLD_COLLISION = code("E_CASE_FOLD_COLLISION", CategoryInstance)
)

// Adapter codes.
var (
	// E_ADAPTER_PARSE indicates a format-specific parsing error.
	E_ADAPTER_PARSE = code("E_ADAPTER_PARSE", CategoryAdapter)
)

// Graph codes.
var (
	// E_DUPLICATE_PK indicates a duplicate primary key in the graph.
	E_DUPLICATE_PK = code("E_DUPLICATE_PK", CategoryGraph)

	// E_DUPLICATE_COMPOSED_PK indicates a duplicate composed child primary key.
	E_DUPLICATE_COMPOSED_PK = code("E_DUPLICATE_COMPOSED_PK", CategoryGraph)

	// E_UNRESOLVED_REQUIRED indicates a required association is unresolved.
	E_UNRESOLVED_REQUIRED = code("E_UNRESOLVED_REQUIRED", CategoryGraph)

	// E_GRAPH_TYPE_NOT_FOUND indicates a type referenced in graph operations cannot be found.
	E_GRAPH_TYPE_NOT_FOUND = code("E_GRAPH_TYPE_NOT_FOUND", CategoryGraph)

	// E_GRAPH_PARENT_NOT_FOUND indicates a parent node cannot be found.
	E_GRAPH_PARENT_NOT_FOUND = code("E_GRAPH_PARENT_NOT_FOUND", CategoryGraph)

	// E_GRAPH_INVALID_COMPOSITION indicates an invalid composition in graph operations.
	E_GRAPH_INVALID_COMPOSITION = code("E_GRAPH_INVALID_COMPOSITION", CategoryGraph)

	// E_GRAPH_MISSING_PK indicates a primary key is missing in graph operations.
	E_GRAPH_MISSING_PK = code("E_GRAPH_MISSING_PK", CategoryGraph)
)

// allCodes contains all defined codes for AllCodes() and uniqueness verification.
var allCodes = []Code{
	// Sentinel
	E_LIMIT_REACHED,
	E_INTERNAL,
	// Schema
	E_TYPE_COLLISION,
	E_INHERIT_CYCLE,
	E_SCHEMA_TYPE_NOT_FOUND,
	E_UNKNOWN_PROPERTY,
	E_DUPLICATE_PROPERTY,
	E_DUPLICATE_RELATION,
	E_CASE_COLLISION,
	E_PROPERTY_RELATION_COLLISION,
	E_RELATION_NORMALIZATION_COLLISION,
	E_RESERVED_PREFIX,
	E_INVALID_RELATION,
	E_INVALID_ASSOCIATION_TARGET,
	E_INVALID_COMPOSITION_TARGET,
	E_INVALID_CONSTRAINT,
	E_INVALID_INVARIANT,
	E_INVALID_NAME,
	E_UPSTREAM_FAIL,
	E_PROPERTY_CONFLICT,
	E_UNKNOWN_TYPE,
	E_DUPLICATE_TYPE,
	E_RELATION_COLLISION,
	E_MISSING_SOURCE_ID,
	E_INVALID_SYNTHETIC_ID,
	// Syntax
	E_SYNTAX,
	// Import
	E_IMPORT_RESOLVE,
	E_IMPORT_CYCLE,
	E_INVALID_ALIAS,
	E_PATH_ESCAPE,
	E_IMPORT_NOT_ALLOWED,
	E_DUPLICATE_IMPORT,
	E_IMPORT_ALIAS_COLLISION,
	// Instance
	E_INSTANCE_TYPE_NOT_FOUND,
	E_ABSTRACT_TYPE,
	E_PART_TYPE_DIRECT,
	E_TYPE_MISMATCH,
	E_MISSING_REQUIRED,
	E_MISSING_PRIMARY_KEY,
	E_UNKNOWN_FIELD,
	E_CONSTRAINT_FAIL,
	E_INVARIANT_FAIL,
	E_EVAL_ERROR,
	E_UNKNOWN_BUILTIN,
	E_MISSING_FK_TARGET,
	E_PARTIAL_COMPOSITE_FK,
	E_UNKNOWN_EDGE_FIELD,
	E_EDGE_SHAPE_MISMATCH,
	E_UNRESOLVED_REQUIRED_COMPOSITION,
	E_COMPOSITION_NOT_FOUND,
	E_MISSING_TYPE_TAG,
	E_INVALID_TYPE_TAG,
	E_CASE_FOLD_COLLISION,
	// Adapter
	E_ADAPTER_PARSE,
	// Graph
	E_DUPLICATE_PK,
	E_DUPLICATE_COMPOSED_PK,
	E_UNRESOLVED_REQUIRED,
	E_GRAPH_TYPE_NOT_FOUND,
	E_GRAPH_PARENT_NOT_FOUND,
	E_GRAPH_INVALID_COMPOSITION,
	E_GRAPH_MISSING_PK,
}

// AllCodes returns all defined codes.
//
// This function is useful for tooling and testing. The returned slice is a
// copy; modifications do not affect the original.
func AllCodes() []Code {
	result := make([]Code, len(allCodes))
	copy(result, allCodes)
	return result
}

// CodesByCategory returns codes in the given category.
//
// The returned slice is a new allocation; modifications do not affect
// internal state.
func CodesByCategory(cat CodeCategory) []Code {
	var result []Code
	for _, c := range allCodes {
		if c.cat == cat {
			result = append(result, c)
		}
	}
	return result
}
