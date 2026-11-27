package diag

import "sync"

// Code is a stable programmatic identifier for an Issue.
//
// Error codes are stable identifiers that tools can match on, even when
// message text changes. Codes are created and registered via NewCode().
// Built-in codes are defined in this package; the Neo4j mapping and consumer
// packages define their own domain-specific codes using the same function.
//
// Code.String() values are globally unique across all registered codes.
type Code struct {
	value string
}

// String returns the code's string representation (e.g., "E_CASE_COLLISION").
func (c Code) String() string {
	return c.value
}

// Error returns the code's name, as [Code.String] does, so %v and %s print the
// name. A Code is the target of errors.Is, which reads a [ResultError] by the
// codes it saw; no call returns a Code as an error.
func (c Code) Error() string {
	return c.value
}

// IsZero reports whether the code is unset.
func (c Code) IsZero() bool {
	return c.value == ""
}

var (
	mu       sync.Mutex
	registry = make(map[string]Code)
)

// NewCode creates and registers a new diagnostic Code with the given
// identifier.
//
// Panics on an empty value, which no issue can carry ([Code.IsZero] reports it
// unset), and on an identifier already registered. Use a package-scoped prefix to avoid collisions (e.g., "E_NEO4J_*").
//
// Codes should be defined as package-level variables so that
// registration happens at program startup:
//
//	var E_MY_ERROR = diag.NewCode("E_MY_ERROR")
func NewCode(value string) Code {
	if value == "" {
		panic("diag.NewCode: empty code value")
	}
	mu.Lock()
	defer mu.Unlock()
	if _, dup := registry[value]; dup {
		panic("diag: duplicate code " + value)
	}
	c := Code{value: value}
	registry[value] = c
	return c
}

// Sentinel codes.
var (
	// E_INTERNAL indicates a broken invariant, which marks a library defect.
	E_INTERNAL = NewCode("E_INTERNAL")

	// E_COMMAND_FAILED carries a command-line failure that is not itself a
	// diagnostic — a bad flag, an unreadable path, a lost connection, an exit
	// code the document's issues do not earn, such as neo4j diff's drift — into a
	// --json document, so a machine consumer reads it by code rather
	// than as prose beside the document. Its exit_code detail is the code the
	// process exits with.
	E_COMMAND_FAILED = NewCode("E_COMMAND_FAILED")

	// E_CONTEXT_CANCELLED indicates the operation was cancelled via context.
	// An operation that checks its context and reports through a [Result]
	// raises it when ctx.Err() returns a non-nil error; one that returns another
	// Go error returns ctx.Err() or wraps it instead.
	E_CONTEXT_CANCELLED = NewCode("E_CONTEXT_CANCELLED")
)

// Schema codes.
var (
	// E_INHERIT_CYCLE indicates an inheritance chain contains a cycle.
	E_INHERIT_CYCLE = NewCode("E_INHERIT_CYCLE")

	// E_UNKNOWN_PROPERTY indicates a referenced property cannot be found on its type.
	E_UNKNOWN_PROPERTY = NewCode("E_UNKNOWN_PROPERTY")

	// E_DUPLICATE_PROPERTY indicates a property is defined more than once on a
	// type, or an edge property more than once on one relation.
	E_DUPLICATE_PROPERTY = NewCode("E_DUPLICATE_PROPERTY")

	// E_DUPLICATE_RELATION indicates a relation is defined more than once on a type.
	E_DUPLICATE_RELATION = NewCode("E_DUPLICATE_RELATION")

	// E_CASE_COLLISION indicates two property names of a type, or two edge property
	// names of an association, differ only by case.
	E_CASE_COLLISION = NewCode("E_CASE_COLLISION")

	// E_PROPERTY_RELATION_COLLISION indicates a property name and a relation's
	// field name that share one lower-case spelling, such as owner and OWNER.
	E_PROPERTY_RELATION_COLLISION = NewCode("E_PROPERTY_RELATION_COLLISION")

	// E_INVALID_ASSOCIATION_TARGET indicates an association targets an invalid type,
	// or a part type declares or inherits an association.
	E_INVALID_ASSOCIATION_TARGET = NewCode("E_INVALID_ASSOCIATION_TARGET")

	// E_INVALID_COMPOSITION_TARGET indicates a composition targets an invalid type.
	E_INVALID_COMPOSITION_TARGET = NewCode("E_INVALID_COMPOSITION_TARGET")

	// E_COMPOSITION_CYCLE indicates a cycle whose every composition is
	// required, so no instance on it is finite.
	E_COMPOSITION_CYCLE = NewCode("E_COMPOSITION_CYCLE")

	// E_INVALID_SUPERTYPE indicates an extends clause names a type of a kind the
	// subtype may not extend: a supertype is abstract or of the subtype's own
	// kind, so an abstract type extends abstract types alone.
	E_INVALID_SUPERTYPE = NewCode("E_INVALID_SUPERTYPE")

	// E_INVALID_CONSTRAINT indicates a constraint definition the load refuses, or,
	// at Warning, a minus sign before the unbounded marker, which has no effect.
	E_INVALID_CONSTRAINT = NewCode("E_INVALID_CONSTRAINT")

	// E_INVALID_INVARIANT indicates an invariant's expression or message is invalid.
	E_INVALID_INVARIANT = NewCode("E_INVALID_INVARIANT")

	// E_DUPLICATE_INVARIANT indicates a type declares one invariant message twice.
	E_DUPLICATE_INVARIANT = NewCode("E_DUPLICATE_INVARIANT")

	// E_INVARIANT_CONFLICT indicates a type inherits two definitions of one
	// invariant message with different expressions. A literal with no value
	// matches any literal of its kind.
	E_INVARIANT_CONFLICT = NewCode("E_INVARIANT_CONFLICT")

	// E_INVALID_NAME indicates a name the schema refuses: its format, a reserved
	// spelling, or an invariant's read of a relation in a casing other than its
	// declared name.
	E_INVALID_NAME = NewCode("E_INVALID_NAME")

	// E_UPSTREAM_FAIL indicates an imported schema failed to compile.
	E_UPSTREAM_FAIL = NewCode("E_UPSTREAM_FAIL")

	// E_PROPERTY_CONFLICT indicates conflicting property definitions from inheritance.
	E_PROPERTY_CONFLICT = NewCode("E_PROPERTY_CONFLICT")

	// E_UNKNOWN_TYPE indicates a referenced type cannot be found.
	E_UNKNOWN_TYPE = NewCode("E_UNKNOWN_TYPE")

	// E_DUPLICATE_TYPE indicates a schema declares one name more than once. A
	// schema's types and datatypes share one namespace.
	E_DUPLICATE_TYPE = NewCode("E_DUPLICATE_TYPE")

	// E_DUPLICATE_SCHEMA indicates two schemas of one load that declare one name.
	// Only a schema whose own compile drew no error is judged, because a schema
	// that failed takes no name.
	E_DUPLICATE_SCHEMA = NewCode("E_DUPLICATE_SCHEMA")

	// E_RELATION_COLLISION indicates a type carries conflicting relation
	// definitions under one name: an inherited definition that differs from the
	// type's own or from another inherited one, or an association and a
	// composition sharing a name.
	E_RELATION_COLLISION = NewCode("E_RELATION_COLLISION")

	// E_INVALID_EDGE_PROPERTY_TYPE indicates a relationship property of a type
	// an edge does not hold: a List or a Vector, written or through a datatype.
	E_INVALID_EDGE_PROPERTY_TYPE = NewCode("E_INVALID_EDGE_PROPERTY_TYPE")

	// E_INVALID_PRIMARY_KEY_TYPE indicates a type not allowed as a primary key.
	// Only String, UUID, Date, and Timestamp are permitted as primary key types.
	E_INVALID_PRIMARY_KEY_TYPE = NewCode("E_INVALID_PRIMARY_KEY_TYPE")

	// E_NO_PRIMARY_KEY indicates a concrete (non-abstract, non-part) type that
	// declares or inherits no primary key. A node needs identity to be added to a
	// graph or referenced by an association, so such a type is rejected at load
	// rather than at graph-construction time.
	E_NO_PRIMARY_KEY = NewCode("E_NO_PRIMARY_KEY")

	// E_LOAD_IO_FAILURE indicates an I/O error during schema loading, or a load
	// whose arguments or option values the loader refuses. It covers file read
	// failures, path resolution errors and other filesystem issues. An option
	// a load does not take is refused with an error that carries no code.
	E_LOAD_IO_FAILURE = NewCode("E_LOAD_IO_FAILURE")

	// E_LOAD_MODULE_ROOT_MALFORMED indicates a yammm.mod module-root marker
	// that is not a regular file, is larger than the marker bound, or holds
	// content that violates the marker rule: it must be empty or hold only
	// comment lines. Error severity: the marker is user content like a schema,
	// and the load reports it as it reports an invalid schema.
	E_LOAD_MODULE_ROOT_MALFORMED = NewCode("E_LOAD_MODULE_ROOT_MALFORMED")

	// E_UNKNOWN_ANNOTATION indicates an annotation name the built-in registry holds
	// at neither placement (@name on a property, @@name on a type). A name registered
	// at the other placement draws E_INVALID_ANNOTATION.
	E_UNKNOWN_ANNOTATION = NewCode("E_UNKNOWN_ANNOTATION")

	// E_UNKNOWN_ANNOTATION_TARGET indicates a property-reference argument of an
	// annotation (such as a @@index member) that names no property of the type.
	// It shares a failure class with E_UNKNOWN_PROPERTY on a different construct.
	E_UNKNOWN_ANNOTATION_TARGET = NewCode("E_UNKNOWN_ANNOTATION_TARGET")

	// E_INVALID_ANNOTATION indicates a structural annotation violation: wrong
	// placement, wrong arity, a bad argument kind or keyword, a duplicate, a
	// property annotation that begins a line, or two supertypes whose
	// annotations on one inherited property disagree.
	E_INVALID_ANNOTATION = NewCode("E_INVALID_ANNOTATION")

	// E_INVALID_ANNOTATION_TARGET indicates an annotation attached to an
	// ineligible property — e.g. @index on a non-scalar or sole primary key,
	// @vector on a non-Vector, or @writeOnce on a primary-key member.
	E_INVALID_ANNOTATION_TARGET = NewCode("E_INVALID_ANNOTATION_TARGET")

	// W_ANNOTATION_SHADOWED indicates that a type's own property re-declaration
	// (identical or narrowing) shadows an inherited property carrying
	// annotations without re-stating them, so those annotations silently drop
	// from the re-declaring type. Warning severity: it surfaces the resulting
	// write-shape / index-shape regression without blocking the load. A
	// re-declaration draws at most one warning per direct supertype, and the
	// warning names that supertype.
	W_ANNOTATION_SHADOWED = NewCode("W_ANNOTATION_SHADOWED")
)

// Syntax codes.
var (
	// E_SYNTAX indicates a syntax error in the schema source.
	E_SYNTAX = NewCode("E_SYNTAX")
)

// Import codes.
var (
	// E_IMPORT_RESOLVE indicates an import path could not be resolved.
	E_IMPORT_RESOLVE = NewCode("E_IMPORT_RESOLVE")

	// E_IMPORT_CYCLE indicates a cycle exists in the import dependency graph.
	E_IMPORT_CYCLE = NewCode("E_IMPORT_CYCLE")

	// E_INVALID_ALIAS indicates an import with no as clause whose path derives a
	// reserved keyword as its alias. A written reserved alias is E_SYNTAX.
	E_INVALID_ALIAS = NewCode("E_INVALID_ALIAS")

	// E_PATH_ESCAPE indicates an import path escapes the allowed directory.
	E_PATH_ESCAPE = NewCode("E_PATH_ESCAPE")

	// E_IMPORT_NOT_ALLOWED indicates imports are not allowed in this context.
	E_IMPORT_NOT_ALLOWED = NewCode("E_IMPORT_NOT_ALLOWED")

	// E_DUPLICATE_IMPORT indicates the same schema imported more than once, or
	// two imports that share one alias.
	E_DUPLICATE_IMPORT = NewCode("E_DUPLICATE_IMPORT")

	// E_IMPORT_ALIAS_COLLISION indicates an import alias collides with a local
	// type or datatype name.
	E_IMPORT_ALIAS_COLLISION = NewCode("E_IMPORT_ALIAS_COLLISION")
)

// Instance validation codes.
var (
	// E_INSTANCE_TYPE_NOT_FOUND indicates a type referenced in instance data cannot be found.
	E_INSTANCE_TYPE_NOT_FOUND = NewCode("E_INSTANCE_TYPE_NOT_FOUND")

	// E_ABSTRACT_TYPE indicates an attempt to instantiate an abstract type,
	// an empty batch under its name included.
	E_ABSTRACT_TYPE = NewCode("E_ABSTRACT_TYPE")

	// E_PART_TYPE_DIRECT indicates an attempt to directly instantiate a part
	// type, an empty batch under its name included.
	E_PART_TYPE_DIRECT = NewCode("E_PART_TYPE_DIRECT")

	// E_TYPE_MISMATCH indicates a value has the wrong type.
	E_TYPE_MISMATCH = NewCode("E_TYPE_MISMATCH")

	// E_MISSING_REQUIRED indicates a required property is missing.
	E_MISSING_REQUIRED = NewCode("E_MISSING_REQUIRED")

	// E_UNKNOWN_FIELD indicates an unexpected field in instance data.
	E_UNKNOWN_FIELD = NewCode("E_UNKNOWN_FIELD")

	// E_CONSTRAINT_FAIL indicates a constraint check failed.
	E_CONSTRAINT_FAIL = NewCode("E_CONSTRAINT_FAIL")

	// E_INVARIANT_FAIL indicates an invariant check failed.
	E_INVARIANT_FAIL = NewCode("E_INVARIANT_FAIL")

	// E_EVAL_ERROR indicates an error during expression evaluation.
	E_EVAL_ERROR = NewCode("E_EVAL_ERROR")

	// E_MISSING_FK_TARGET indicates an edge object that holds none of its
	// target's `_target_` foreign-key fields.
	E_MISSING_FK_TARGET = NewCode("E_MISSING_FK_TARGET")

	// E_PARTIAL_COMPOSITE_FK indicates a partial composite foreign key.
	E_PARTIAL_COMPOSITE_FK = NewCode("E_PARTIAL_COMPOSITE_FK")

	// E_UNKNOWN_EDGE_FIELD indicates an unknown field in edge data.
	E_UNKNOWN_EDGE_FIELD = NewCode("E_UNKNOWN_EDGE_FIELD")

	// E_EDGE_SHAPE_MISMATCH indicates an edge's or a composition's value has a
	// shape its relation does not take.
	E_EDGE_SHAPE_MISMATCH = NewCode("E_EDGE_SHAPE_MISMATCH")

	// E_UNRESOLVED_REQUIRED_COMPOSITION indicates a required composition is unresolved.
	E_UNRESOLVED_REQUIRED_COMPOSITION = NewCode("E_UNRESOLVED_REQUIRED_COMPOSITION")

	// E_COMPOSITION_DEPTH_EXCEEDED indicates composed nesting deeper than
	// [github.com/simon-lentz/yammm/instance.MaxComposedDepth], the bound the
	// .ys wire enforces. The validator refuses it first, so no validated instance
	// is nested past that bound.
	E_COMPOSITION_DEPTH_EXCEEDED = NewCode("E_COMPOSITION_DEPTH_EXCEEDED")

	// E_INVALID_TYPE_TAG indicates a type tag that is not a type name by the
	// grammar's rule: a JSON document's top-level key.
	E_INVALID_TYPE_TAG = NewCode("E_INVALID_TYPE_TAG")

	// E_DATA_PARSE indicates an instance document that is not well formed, is
	// not one object, holds a type name whose value is not an array of objects,
	// states one member name twice in one object, holds a string that is not
	// valid UTF-8 or a \u escape that is half of no surrogate pair, or nests
	// deeper than the decoder's 10,000-level limit.
	E_DATA_PARSE = NewCode("E_DATA_PARSE")
)

// Graph codes.
var (
	// E_DUPLICATE_PK indicates a primary key stated twice for one type: by two
	// instances added to a graph, by two root instances whose stored keys the key
	// rule both accepts, in a snapshot that snapshot.Stat reads or, of a type that
	// can hold one, that snapshot.Unmarshal or Verify reads, or by two root instances a yammm data command reads from its
	// data files and --into file, one of them refused.
	E_DUPLICATE_PK = NewCode("E_DUPLICATE_PK")

	// E_DUPLICATE_COMPOSED_PK indicates a composition slot that cannot hold a
	// child: two children of one (many) slot share a primary key, or a (one)
	// slot is given several. The validator raises it. snapshot.Unmarshal
	// and Verify raise it for a (one) slot a document fills more than once and
	// for two children of a keyed (many) slot at one key, compared as written.
	// Stat reads without a schema, which the checks need, so it makes no such claim.
	E_DUPLICATE_COMPOSED_PK = NewCode("E_DUPLICATE_COMPOSED_PK")

	// E_DUPLICATE_EDGE indicates a (many) association that names one target
	// twice for one source: a graph holds one edge per source, association and
	// target. The validator raises it for a target key one listing repeats where
	// no other check refused either listing. snapshot.Unmarshal and Verify refuse such
	// a document with E_SNAPSHOT_MALFORMED where no other check refused the
	// repeating listing, one sound root (of a type that can hold a root, its stored key admitted by
	// the key rule) holds the source's address alone, and an edge's target's,
	// and no root stands at a record's target and no refused unit states it.
	// Elsewhere the issue at the listing, the source or the target stands for
	// the repeat.
	E_DUPLICATE_EDGE = NewCode("E_DUPLICATE_EDGE")

	// E_UNRESOLVED_REQUIRED indicates a required association is unresolved.
	E_UNRESOLVED_REQUIRED = NewCode("E_UNRESOLVED_REQUIRED")

	// E_GRAPH_INVALID_COMPOSITION indicates an invalid composition in graph operations.
	E_GRAPH_INVALID_COMPOSITION = NewCode("E_GRAPH_INVALID_COMPOSITION")

	// E_GRAPH_MISSING_PK indicates an instance added at a root whose type
	// declares no primary key.
	E_GRAPH_MISSING_PK = NewCode("E_GRAPH_MISSING_PK")

	// E_GRAPH_CARDINALITY indicates a (one) association holding more than one
	// record: a merge at graph.Graph.AddOrMerge that would add a second target,
	// or several edges and unresolved records no check refused, together in a
	// .ys document.
	E_GRAPH_CARDINALITY = NewCode("E_GRAPH_CARDINALITY")

	// E_GRAPH_UNKNOWN_RELATION indicates an association record, or an empty
	// edge list, under a relation name the type does not declare as an
	// association. The .ys reader reports it.
	E_GRAPH_UNKNOWN_RELATION = NewCode("E_GRAPH_UNKNOWN_RELATION")
)

// Snapshot persistence codes.
var (
	// E_SNAPSHOT_MALFORMED indicates the .ys file is not valid JSON, has a
	// header past snapshot.MaxHeaderSize where only the header is read, has the
	// wrong outer shape (the four top-level keys, in order, each once and none
	// null, with nothing after them, and no object repeating a member name or
	// spelling a declared one in another case), or states content a structural
	// rule refuses, such as an undeclared name, a stored key its key properties
	// contradict, a key or target key component graph.ParseKey cannot read back,
	// a target key of the wrong arity, an undocumented reason, a record
	// contradicting itself, records graph.Add would not derive, a duplicate with
	// no conflict or another one, a duplicate stating a parent or a relation, a
	// string holding invalid UTF-8 or half a surrogate pair, which encoding/json
	// would silently read as U+FFFD, or a created_at that is not an RFC 3339
	// time or that states a leap second or the zero instant.
	// snapshot.Marshal raises it for input it refuses to write: an indent that is
	// not JSON whitespace, a created_at time with no RFC 3339 form, or a metadata
	// key or value, a source name or a path that is not valid UTF-8;
	// snapshot.UpdateMetadata raises it for such metadata too.
	E_SNAPSHOT_MALFORMED = NewCode("E_SNAPSHOT_MALFORMED")

	// E_SNAPSHOT_UNSUPPORTED_VERSION indicates the format version is not recognized.
	E_SNAPSHOT_UNSUPPORTED_VERSION = NewCode("E_SNAPSHOT_UNSUPPORTED_VERSION")

	// E_SNAPSHOT_UNSUPPORTED_FEATURE indicates an unrecognized feature flag in the header.
	// One diagnostic is emitted per unrecognized feature.
	E_SNAPSHOT_UNSUPPORTED_FEATURE = NewCode("E_SNAPSHOT_UNSUPPORTED_FEATURE")

	// E_SNAPSHOT_INCOMPATIBLE_SCHEMA indicates the schema structural hash does not
	// match between the .ys file and the provided schema.
	E_SNAPSHOT_INCOMPATIBLE_SCHEMA = NewCode("E_SNAPSHOT_INCOMPATIBLE_SCHEMA")

	// E_SNAPSHOT_UNKNOWN_TYPE indicates a type name in the .ys file does not exist
	// in the provided schema.
	E_SNAPSHOT_UNKNOWN_TYPE = NewCode("E_SNAPSHOT_UNKNOWN_TYPE")

	// E_SNAPSHOT_TYPE_MISMATCH indicates a type row that contradicts its
	// position: a root's row that is not its group's, a duplicate instance's
	// that is not its record's, or a composed child's or an association record's
	// target row that is not the relation's declared target.
	E_SNAPSHOT_TYPE_MISMATCH = NewCode("E_SNAPSHOT_TYPE_MISMATCH")

	// E_SNAPSHOT_DANGLING_REFERENCE indicates an edge target, a duplicate's
	// conflict, or an unresolved record's source names no root instance.
	E_SNAPSHOT_DANGLING_REFERENCE = NewCode("E_SNAPSHOT_DANGLING_REFERENCE")

	// E_SNAPSHOT_INVALID_COMPOSED indicates a composed child instance carries edges,
	// which violates the composed children invariant (edges are only on root
	// instances), or composed children under a name the parent's type does not
	// declare as a composition.
	E_SNAPSHOT_INVALID_COMPOSED = NewCode("E_SNAPSHOT_INVALID_COMPOSED")

	// E_SNAPSHOT_INVALID_ROOT indicates an instances group, empty or not, or a
	// duplicate record names a type that cannot hold a root instance: an
	// abstract type, a part type, or one declaring no primary key. No instance of
	// any of the three installs at
	// [github.com/simon-lentz/yammm/graph.Graph.Add], so a document stating one
	// describes a graph that cannot be built. The message names which rule the
	// type fails. One type draws one issue, however many positions name it.
	E_SNAPSHOT_INVALID_ROOT = NewCode("E_SNAPSHOT_INVALID_ROOT")

	// E_SNAPSHOT_COMPOSED_ON_DUPLICATE indicates a duplicate record's instance contains
	// composed children, violating the duplicate structural constraint.
	E_SNAPSHOT_COMPOSED_ON_DUPLICATE = NewCode("E_SNAPSHOT_COMPOSED_ON_DUPLICATE")

	// E_SNAPSHOT_EDGES_ON_DUPLICATE indicates a duplicate record's instance contains
	// edges, violating the duplicate structural constraint.
	E_SNAPSHOT_EDGES_ON_DUPLICATE = NewCode("E_SNAPSHOT_EDGES_ON_DUPLICATE")

	// E_SNAPSHOT_DEPTH_EXCEEDED indicates composed nesting deeper than
	// [github.com/simon-lentz/yammm/instance.MaxComposedDepth].
	E_SNAPSHOT_DEPTH_EXCEEDED = NewCode("E_SNAPSHOT_DEPTH_EXCEEDED")

	// E_SNAPSHOT_INTEGRITY_MISMATCH indicates the integrity hash does not match the
	// document content, or the header holds none: an empty, null or absent
	// integrity_hash. The file may be corrupted, truncated, or modified.
	E_SNAPSHOT_INTEGRITY_MISMATCH = NewCode("E_SNAPSHOT_INTEGRITY_MISMATCH")

	// E_SNAPSHOT_UNSUPPORTED_HASH_ALGORITHM indicates a schema hash
	// algorithm version this library does not implement. Error on the
	// body-reading surfaces (Unmarshal, Verify, Stat, UpdateMetadata).
	// ReadHeader and ScanDir accept such a header and draw nothing, so a
	// document stays classifiable for dispatch; the CLI reports it as a Warning,
	// save beside another header failure, which returns no header to read.
	E_SNAPSHOT_UNSUPPORTED_HASH_ALGORITHM = NewCode("E_SNAPSHOT_UNSUPPORTED_HASH_ALGORITHM")

	// E_SNAPSHOT_IO indicates an I/O failure reading a snapshot: a file that
	// fails to open (os.Open, on snapshot.ScanDir's per-file path), a reader
	// error that stops snapshot.ReadHeader's decode and is not and does not
	// wrap io.EOF or io.ErrUnexpectedEOF (an error that arrives with or after
	// the types table's last byte is not read), or, at yammm snapshot info
	// --dir, a directory that fails to
	// read. A header that ends early is ReadHeader's
	// Error-severity E_SNAPSHOT_MALFORMED, never this code.
	// Per-file emissions land on ScanEntry.Err so the iterator
	// continues to the next file rather than aborting.
	// The underlying os error is preserved as a detail entry so the
	// operator can recover the concrete cause. Named E_SNAPSHOT_IO, not a
	// bare I/O name, to match the E_SNAPSHOT_* convention, as E_LOAD_IO_FAILURE
	// names the schema load's I/O failure.
	E_SNAPSHOT_IO = NewCode("E_SNAPSHOT_IO")

	// W_SNAPSHOT_UNRESOLVED_REQUIRED indicates a loaded document carries an
	// unresolved record for an association the schema declares required, which
	// is read from the schema, never from the record. A record a structural
	// check refused draws that check's issue instead. Reported only when the
	// caller passes snapshot.WithRevalidation, at that option's severity —
	// the walk that finds the record runs on every Unmarshal and Verify, but a
	// document holding the record is well-formed, so without the option the
	// record stays data (the snapshot's unresolved records) rather than a
	// diagnostic.
	W_SNAPSHOT_UNRESOLVED_REQUIRED = NewCode("W_SNAPSHOT_UNRESOLVED_REQUIRED")

	// W_SNAPSHOT_PATH_EXTENSION indicates a snapshot was written where a scan
	// for .ys files does not find it: neither the path nor a link it follows
	// has a name that ends in .ys and names a regular file. The document is
	// correct and the write succeeded; the warning exists because every reader
	// that discovers snapshots by extension — a directory scan among them — will
	// not find this one. Warning severity: the caller named the path
	// deliberately, and refusing it would make a documented workflow fail
	// rather than work.
	W_SNAPSHOT_PATH_EXTENSION = NewCode("W_SNAPSHOT_PATH_EXTENSION")
)
