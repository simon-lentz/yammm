// Package yammm provides schema definition and instance validation for Go applications.
//
// YAMMM (Yet Another Meta-Meta Model) is a library for defining schemas in a
// small DSL (.yammm files) and validating Go data against them at runtime.
// It provides post-validation services including snapshot persistence and
// integrity checking.
//
// # Package Structure
//
// The module's packages and what they provide:
//
//   - [github.com/simon-lentz/yammm/location]: Source positions, spans, and canonical paths
//   - [github.com/simon-lentz/yammm/diag]: Structured diagnostics with stable error codes
//   - [github.com/simon-lentz/yammm/schema]: Type system, constraints, schema loading, and programmatic building
//   - [github.com/simon-lentz/yammm/schema/expr]: Expression AST types for invariants
//   - [github.com/simon-lentz/yammm/instance]: Instance validation and constraint checking
//   - [github.com/simon-lentz/yammm/graph]: Instance graph construction and integrity checking
//   - [github.com/simon-lentz/yammm/adapter/json]: JSON parsing and serialization
//   - [github.com/simon-lentz/yammm/adapter/neo4j]: Neo4j constraint generation and Cypher query building
//
// Adapters depend on library packages; library packages never depend on adapters.
//
// # Entry Points
//
// Schema loading:
//
//	import "github.com/simon-lentz/yammm/schema"
//
//	s, result := schema.Load(ctx, "path/to/schema.yammm")
//	if result.HasFatal() {
//	    // I/O or cancellation error
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
//	// valids contains validated instances (nil entries for failed instances)
//
// Graph construction:
//
//	import "github.com/simon-lentz/yammm/graph"
//
//	g := graph.New(s)
//	for _, inst := range valids {
//	    if inst == nil { continue }
//	    result := g.Add(ctx, inst)
//	    if !result.OK() {
//	        // Diagnostic issues (duplicate PK, etc.)
//	    }
//	}
//	result := g.Check(ctx)
//	if !result.OK() {
//	    // Unresolved required associations
//	}
package yammm
