// Package load provides schema loading functionality.
//
// The Load functions parse and complete YAMMM schema files,
// handling imports and cross-schema type resolution.
//
// # Basic Usage
//
//	schema, result, err := load.Load(ctx, "path/to/schema.yammm")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	if result.HasErrors() {
//	    for _, issue := range result.Issues() {
//	        fmt.Println(issue)
//	    }
//	}
//
// # String Loading
//
// For testing or embedded schemas, use LoadString:
//
//	schema, result, err := load.LoadString(ctx, source, "test.yammm")
//
// # In-Memory Sources
//
// For complex test scenarios with imports:
//
//	sources := map[string][]byte{
//	    "main.yammm":   mainContent,
//	    "common.yammm": commonContent,
//	}
//	schema, result, err := load.LoadSources(ctx, sources, "/project")
//
// # Options
//
// Customize loading behavior with options:
//
//	schema, result, err := load.Load(ctx, path,
//	    load.WithRegistry(registry),
//	    load.WithModuleRoot("/project"),
//	    load.WithIssueLimit(50),
//	)
package load
