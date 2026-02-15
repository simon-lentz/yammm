// Package yammm provides schema definition and instance validation for Go applications.
//
// YAMMM (Yet Another Meta-Meta Model) is a library for defining schemas in a
// small DSL (.yammm files) and validating Go data against them at runtime.
// It provides post-validation services including graph traversal and
// integrity checking.
//
// # Architecture Overview
//
// The module is organized into layers with strict dependency ordering:
//
//	Foundation tier (no internal dependencies):
//	  - location: Source positions, spans, and canonical paths
//	  - diag: Structured diagnostics with stable error codes
//	  - immutable: Read-only wrappers for safe data sharing
//
//	Core library tier:
//	  - schema: Type system, constraints, and schema compilation
//	  - instance: Instance validation and constraint checking
//	  - graph: Instance graph construction and integrity checking
//
//	Adapter tier:
//	  - adapter/json: JSON parsing with location tracking
//
// # Entry Points
//
// Schema loading:
//
//	import "github.com/simon-lentz/yammm/schema/load"
//
//	schema, result, err := load.Load(ctx, "path/to/schema.yammm")
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
//	    if !result.OK() {
//	        // Diagnostic issues (duplicate PK, etc.)
//	    }
//	}
//	result, err := g.Check(ctx)
//	if err != nil {
//	    // Internal error or context cancelled
//	}
//	if !result.OK() {
//	    // Unresolved required associations
//	}
//
// # Subpackages
//
// See the individual package documentation for detailed usage:
//
//   - [github.com/simon-lentz/yammm/diag]: Structured diagnostics
//   - [github.com/simon-lentz/yammm/location]: Source location tracking
//   - [github.com/simon-lentz/yammm/immutable]: Read-only data wrappers
//   - [github.com/simon-lentz/yammm/schema]: Schema types and constraints
//   - [github.com/simon-lentz/yammm/schema/load]: Schema file loading
//   - [github.com/simon-lentz/yammm/schema/build]: Programmatic schema building
//   - [github.com/simon-lentz/yammm/instance]: Instance validation
//   - [github.com/simon-lentz/yammm/graph]: Instance graph management
//   - [github.com/simon-lentz/yammm/adapter/json]: JSON adapter
//   - [github.com/simon-lentz/yammm/lsp]: Language Server Protocol server
package yammm
