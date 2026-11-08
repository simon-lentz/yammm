// Package schema provides the public API for loading and querying YAMMM schemas.
//
// # Overview
//
// This package implements the schema layer, which is responsible for:
//   - Loading and parsing schema definitions from *.yammm files
//   - Providing an immutable, thread-safe schema representation
//   - Supporting cross-schema imports and inheritance
//
// # Key Types
//
// The schema object model's key types:
//
//   - [Schema]: Top-level container with types, data types, and imports
//   - [Type]: Named type with properties, relations, invariants, and inheritance
//   - [Property]: Typed property with optionality, primary key, and constraint metadata
//   - [Relation]: Association (reference) or composition (ownership) between types
//   - [Invariant]: Named boolean constraint expression evaluated at validation time
//   - [DataType]: Named constraint alias (e.g., `type Email = Pattern["^.+@.+$"]`)
//   - [Import]: Cross-schema import declaration with alias
//   - [TypeID]: Canonical type identity tuple (SourceID, name) for cross-schema resolution
//   - [TypeRef]: Syntactic type reference preserving user's original syntax
//   - [Annotation]: Validated @name / @@name decorator on a property or type,
//     carrying [AnnotationArg] values whose semantic [AnnotationArgKind] is
//     stamped during completion
//
// Editor tooling reads the curated annotation registry through
// [AnnotationSpecs] and [VectorSimilarityFunctions] rather than hard-coding
// names, so it offers only names and keywords the loader knows. It filters names by
// placement and keywords by annotation name, so @vector's keywords are offered at
// @@vector( too. It can still offer an annotation a property's type refuses, such as
// @vector on a String.
//
// # Loading Schemas
//
// Schemas are loaded using the Load family of functions:
//
//	// Load from file
//	s, result := schema.Load(ctx, "schema.yammm")
//
//	// Load from string (imports disallowed)
//	s, result := schema.LoadString(ctx, source, "schema.yammm")
//
//	// Load from in-memory sources with an explicit entry point (useful for
//	// LSP, and for a generated package's embedded sources)
//	s, result := schema.LoadSourcesWithEntry(ctx, sources, entryPath, moduleRoot)
//
// A non-OK result with a zero Schema indicates failure. result.HasFatal() reports
// a load that could not start or finish for one of these reasons: an I/O failure, a cancellation, or
// input a load cannot begin from, such as no sources, a source key the loader
// refuses, two keys that name one source, or a source name that is not valid
// UTF-8 or holds a character that does not print, or a source whose header cannot be parsed, which the parser reports
// as a Fatal E_SYNTAX. A schema name with no value is an Error, E_SYNTAX, an
// empty one an Error, E_INVALID_NAME, and a malformed yammm.mod marker an
// Error, E_LOAD_MODULE_ROOT_MALFORMED, each with a zero Schema. Check result.HasErrors() to determine semantic success.
//
// # Load Options
//
// Load functions accept [LoadOption] values to configure behavior, among them:
//
//   - [WithModuleRoot]: root directory for module-style imports
//   - [WithSourcesOnly]: resolve imports only against the in-memory sources
//   - [WithSyntheticRoot]: synthetic identities for in-memory sources
//   - [WithImportsAllowed]: with false, refuse import declarations
//
// # Constraint System
//
// Properties carry typed constraints that define valid value ranges. The
// [Constraint] interface is implemented by concrete types for each DSL data
// type: [StringConstraint], [IntegerConstraint], [FloatConstraint],
// [BooleanConstraint], [TimestampConstraint], [DateConstraint],
// [UUIDConstraint], [EnumConstraint], [PatternConstraint],
// [VectorConstraint], [ListConstraint], and [AliasConstraint].
//
// The String, Integer, Float and List constraints have constructors for bounded
// and unbounded variants (e.g., [NewStringConstraint], [StringLenBetween]); the
// other kinds take no bounds. Use [ResolveAlias] to
// unwrap alias chains.
//
// # Structural Hash
//
// [StructuralHash] computes a deterministic SHA-256 digest over the rules that
// decide what instance data is valid, across the schema's whole import
// closure. For every member of [Schema.Closure] it hashes the member's schema
// name, its types with their properties, primary keys, relations,
// compositions, supertypes, constraints, invariants and abstract and part
// markers, and its data types. Each member is framed under its schema name, the entry schema
// first and the rest ordered by name, so the order imports are declared in does
// not enter the digest. A relation target is the pair (owning schema name, type
// name). Closure membership is part of the identity, so the digest moves on a
// change to an imported schema this schema never references. The snapshot
// package uses the digest for compatibility verification.
//
// Two schemas with one digest agree on every hashed rule in their closures.
// The digest leaves out the following, and two such schemas can differ in each:
//
//   - Invariant names, documentation comments and source spans. An invariant's
//     name is its failure message; its expression is hashed.
//   - Annotations. They drive downstream store DDL and the write path's
//     update, and never reject data, so a schema that gains its first
//     annotation still hashes identically and every persisted snapshot header
//     hash stays valid.
//   - Source paths. A member is framed by its declared name, so embedded://
//     and disk loads of one schema text agree.
//   - The entry schema's name surface: its direct import set and its import
//     aliases. One member set under two import graphs or two aliases hashes the
//     same, though a type name one of them resolves, such as lb.T, can miss
//     under the other. A snapshot names a type by its declaring schema's name
//     and its own, so the snapshot reader resolves it through the member set
//     alone.
//   - Which member owns a supertype, and which data type a property names. A
//     supertype hashes by its type name, and the subtype hashes the members it
//     inherits. A property hashes its resolved constraint and never a data
//     type's name. The member that declares a supertype or a data type hashes
//     it in its own frame. So "K extends a.Base" and "K extends b.Base" hash the
//     same when a and b declare Base alike, and a property typed a.Code, one
//     typed b.Code and one typed String[3, 3] hash the same when each Code is
//     String[3, 3]. Instance validity agrees in each case.
//
// So a test that compares a generated package's SchemaHash constant with
// StructuralHash cannot see stale generated code after any change the digest
// leaves out: the code embeds the schema sources and their documentation
// comments. After a change to the name surface, a supertype's owner or a
// property's data type, a field's Go type or a graph field can differ too.
//
// # Merged Views and Property Identity
//
// A property inherited from several direct supertypes, none of which descends from
// another, carries the UNION of their merged views' annotations, so an annotation a
// middle type dropped does not return. When that union differs from the annotations of the declaration
// the entry is taken from, the entry in
// [Type.AllProperties] is a synthesized copy, not
// the Property its declaring type holds in [Type.Properties].
//
// [Property.Origin] bridges the two views: it returns the property as declared,
// and p itself when nothing was merged. Any map keyed by declared properties
// and read back while iterating a merged view must look up through it —
//
//	name, ok := dtFieldNames[p.Origin()]
//
// — or the synthesized entry misses the table: gogen returns an error there, and
// markdown degrades with none. Everything Origin discards is annotation state; name, constraint,
// datatype reference, optionality, primary-key status, doc comment, span, and
// declaring scope are all preserved on the copy, so a consumer that reads only those
// needs nothing.
//
// # Immutability
//
// A loaded schema is immutable, and so is every value it hands out. This provides:
//   - Thread-safety for concurrent access
//   - Predictable behavior (no hidden mutations)
//   - Safe sharing across goroutines
//
// An accessor over records, or over the references that name them, returns an
// iterator, and an accessor over plain values returns a fresh slice.
//
// # Completion and Sealing
//
// Schemas undergo a completion phase that resolves their references and checks
// what they declare, among its steps:
//   - Import declarations record their targets' SourceIDs; the loader sets each
//     target schema
//   - Type inheritance is computed ([Type.SuperTypes])
//   - Relation targets are resolved
//   - Property collisions are detected across the inheritance hierarchy
//   - Alias constraints are resolved to their underlying types
//
// After completion, schemas are sealed to prevent further mutation. The
// [Load] family handles completion automatically.
//
// # Type Identity
//
// Types are identified by [TypeID], a tuple of (SourceID, name). This enables
// cross-schema type resolution and proper handling of imported types.
// Within one load, two handles name one type if and only if their TypeIDs are
// equal, because a load holds one compile of each source. == on two [Type] handles
// compares records, and a graph judges a record by the schema object, so two loads
// of one source give equal TypeIDs and different records.
//
// # Thread Safety
//
// A loaded schema is safe for concurrent reads.
//
// # Dependencies
//
//	schema  ──imports──▶  diag, internal/expr, location,
//	                      internal/parse, internal/source,
//	                      golang.org/x/text/unicode/norm
package schema
