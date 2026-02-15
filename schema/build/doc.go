// Package build provides a fluent builder API for programmatically constructing schemas.
//
// The Builder allows constructing schemas without parsing from text sources.
// This is useful for testing, programmatic schema generation, and embedded schemas.
//
// # Basic Usage
//
//	s, result := build.NewBuilder().
//	    WithName("myschema").
//	    AddType("Person").
//	        WithProperty("name", schema.NewStringConstraint()).
//	        WithProperty("age", schema.NewIntegerConstraint()).
//	    Done().
//	    Build()
//
// # With Imports
//
// When using imports, you must provide a source ID and registry. Import path
// resolution depends on the SourceID type:
//
// ## File-Backed SourceID (Relative Paths Resolved Automatically)
//
// For schemas with file-backed SourceIDs (created via SourceIDFromPath or
// SourceIDFromAbsolutePath), relative import paths (./foo, ../bar) are resolved
// against the schema's directory:
//
//	s, result := build.NewBuilder().
//	    WithName("main").
//	    WithSourceID(location.MustSourceIDFromPath("/project/schemas/main.yammm")).
//	    WithRegistry(registry).
//	    AddImport("./common", "common").  // Resolved to /project/schemas/common.yammm
//	    AddType("User").
//	        Extends(schema.NewTypeRef("common", "Base", location.Span{})).
//	        WithProperty("email", schema.NewStringConstraint()).
//	    Done().
//	    Build()
//
// ## Synthetic SourceID (Schema Name Lookup or Custom Resolver)
//
// For schemas with synthetic SourceIDs (created via NewSourceID), there are
// two options for import resolution:
//
// Option 1 - Use schema names as paths (backward compatible):
//
//	s, result := build.NewBuilder().
//	    WithName("main").
//	    WithSourceID(location.MustNewSourceID("test://main.yammm")).
//	    WithRegistry(registry).
//	    AddImport("common", "common").  // Looked up by schema name
//	    AddType("User").
//	        Extends(schema.NewTypeRef("common", "Base", location.Span{})).
//	        WithProperty("email", schema.NewStringConstraint()).
//	    Done().
//	    Build()
//
// Option 2 - Provide an ImportResolver for relative paths:
//
//	resolver := func(path string) (location.SourceID, bool) {
//	    switch path {
//	    case "./common":
//	        return commonSchema.SourceID(), true
//	    default:
//	        return location.SourceID{}, false
//	    }
//	}
//	s, result := build.NewBuilder().
//	    WithName("main").
//	    WithSourceID(location.MustNewSourceID("test://main.yammm")).
//	    WithRegistry(registry).
//	    WithImportResolver(resolver).
//	    AddImport("./common", "common").  // Resolved via custom resolver
//	    AddType("User").
//	        Extends(schema.NewTypeRef("common", "Base", location.Span{})).
//	        WithProperty("email", schema.NewStringConstraint()).
//	    Done().
//	    Build()
//
// # Import Resolution Summary
//
// | SourceID Type | Path Type | Resolution |
// |---------------|-----------|------------|
// | File-backed   | Relative (./foo) | Resolve against schema directory |
// | File-backed   | Schema name | Look up by name |
// | Synthetic     | Relative (./foo) + Resolver | Use custom resolver |
// | Synthetic     | Relative (./foo) + No Resolver | Error (requires resolver) |
// | Synthetic     | Schema name | Look up by name |
package build
