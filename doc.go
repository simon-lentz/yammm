// Package yammm provides schema definition and instance validation for Go applications.
//
// YAMMM is a library for defining schemas in a
// small DSL (.yammm files) and validating Go data against them at runtime.
// It provides post-validation services including snapshot persistence and
// integrity checking.
//
// # Package Structure
//
// Foundation:
//
//   - [github.com/simon-lentz/yammm/location]: Source identities, positions and spans
//   - [github.com/simon-lentz/yammm/instance/path]: Instance path formatting
//   - [github.com/simon-lentz/yammm/diag]: Structured diagnostics with stable error codes
//   - [github.com/simon-lentz/yammm/immutable]: Immutable collection primitives
//
// Primary API — the load, validate, assemble, persist pipeline:
//
//   - [github.com/simon-lentz/yammm/schema]: Type system, constraints, annotations, and schema loading
//   - [github.com/simon-lentz/yammm/instance]: Instance validation and constraint checking
//   - [github.com/simon-lentz/yammm/graph]: Instance graph construction and integrity checking
//   - [github.com/simon-lentz/yammm/snapshot]: Graph snapshot persistence in the .ys format
//
// Generators — an artifact from a schema:
//
//   - [github.com/simon-lentz/yammm/schema/cypher]: Neo4j constraint and index DDL, and the storage mapping it shares with neo4j
//   - [github.com/simon-lentz/yammm/schema/gogen]: Go source generation from a schema
//   - [github.com/simon-lentz/yammm/schema/markdown]: Markdown + Mermaid documentation generation from a schema
//
// The Neo4j write path — a graph's data in Neo4j's form, and the drift diff:
//
//   - [github.com/simon-lentz/yammm/neo4j]: Cypher write queries, parameter coercion, introspection and the constraint and index diff
//
// The formatter and the language server are internal packages. Their surfaces
// are the binaries: the yammm CLI's fmt command and yammm-lsp.
//
// Test helpers: [github.com/simon-lentz/yammm/snapshot/snapshottest] builds
// snapshot fixtures. In-module suites also use packages under internal/, among
// them internal/instancetest, which builds instance fixtures, and
// internal/yammmtest, the repo-wide support for golden files, structural diffs
// and a context that counts its cancellation checks.
//
// The neo4j package depends on library packages; no library package depends
// on neo4j, and only neo4j and the CLI import the Neo4j driver.
//
// # Entry Points
//
// Schema loading:
//
//	import "github.com/simon-lentz/yammm/schema"
//
//	s, result := schema.Load(ctx, "path/to/schema.yammm")
//	if result.HasFatal() {
//	    // Such as an I/O failure, a cancellation, a refused option, a header that does not parse, or an internal fault
//	}
//	if result.HasErrors() {
//	    // Schema compilation errors
//	}
//
// Instance validation:
//
//	import "github.com/simon-lentz/yammm/instance"
//
//	validator := instance.NewValidator(s)
//	valids, result := validator.Validate(ctx, typeName, rawInstances)
//	if !result.OK() {
//	    // Validation failures (type mismatch, missing required, etc.)
//	}
//	// valids contains validated instances (zero entries for failed instances)
//
// Graph construction:
//
//	import "github.com/simon-lentz/yammm/graph"
//
//	g := graph.New(s)
//	for _, inst := range valids {
//	    if inst.IsZero() { continue }
//	    result := g.Add(ctx, inst)
//	    if !result.OK() {
//	        // Diagnostic issues (duplicate PK, etc.)
//	    }
//	}
//	result = g.Check(ctx)
//	if !result.OK() {
//	    // An unresolved required association, or a cancellation
//	}
package yammm
