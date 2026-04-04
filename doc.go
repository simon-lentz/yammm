// Package yammm provides schema definition and instance validation for Go applications.
//
// YAMMM (Yet Another Meta-Meta Model) is a library for defining schemas in a
// small DSL (.yammm files) and validating Go data against them at runtime.
// It provides post-validation services including graph traversal and
// integrity checking.
//
// # Package Structure
//
// The module's packages and what they provide:
//
//   - [location]: Source positions, spans, and canonical paths
//   - [diag]: Structured diagnostics with stable error codes
//   - [schema]: Type system, constraints, schema loading, and programmatic building
//   - [schema/expr]: Expression AST types for invariants
//   - [instance]: Instance validation and constraint checking
//   - [graph]: Instance graph construction and integrity checking
//   - [graph/walk]: Visitor-pattern graph traversal
//   - [adapter/json]: JSON parsing and serialization
//   - [adapter/neo4j]: Neo4j constraint generation and Cypher query building
//
// Adapters depend on library packages; library packages never depend on adapters.
//
// # Entry Points
//
// Schema loading:
//
//	import "github.com/simon-lentz/yammm/schema"
//
//	s, result, err := schema.Load(ctx, "path/to/schema.yammm")
//	if err != nil {
//	    // I/O or internal error
//	}
//	if result.HasErrors() {
//	    // Schema compilation errors
//	}
//
// Instance validation:
//
//	import "github.com/simon-lentz/yammm/instance"
//
//	validator := instance.NewValidator(schema)
//	valid, failures, err := validator.Validate(ctx, typeName, rawInstances)
//	if err != nil {
//	    // I/O or internal error
//	}
//	// valid contains successfully validated instances
//	// failures contains validation failures with diagnostics
//
// Graph construction:
//
//	import "github.com/simon-lentz/yammm/graph"
//
//	g := graph.New(schema)
//	for _, inst := range valid {
//	    result, err := g.Add(ctx, inst)
//	    if err != nil {
//	        // Internal error or context cancelled
//	    }
//	    if err := result.Err(); err != nil {
//	        // Diagnostic issues (duplicate PK, etc.)
//	    }
//	}
//	result, err := g.Check(ctx)
//	if err != nil {
//	    // Internal error or context cancelled
//	}
//	if err := result.Err(); err != nil {
//	    // Unresolved required associations
//	}
package yammm
