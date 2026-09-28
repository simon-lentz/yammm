// Package adapter provides format-specific adapters that parse data into
// [instance.RawInstance] values and write a [graph.Snapshot], plus a family of
// schema-to-artifact generators. Each adapter subpackage targets a
// specific output — a data format (JSON, CSV, Neo4j Cypher) or a generated
// artifact (Go source, JSON Schema, Markdown) — and may have its own external
// dependencies.
//
// # Architectural Boundary
//
// Adapters are the outermost layer of the module. This design provides:
//
//   - Dependency hygiene via import granularity: Go packages are granular at the
//     import level. Consumers who import only schema and instance depend on no
//     adapter's third-party module: adapter/json pulls tidwall/jsonc,
//     adapter/markdown goldmark and golang.org/x/net/html, and adapter/neo4j
//     the Neo4j driver's dbtype package, each only when that adapter is
//     imported.
//
//   - Clear library/consumer boundary: The adapter package explicitly imports
//     the library to use it, mirroring how downstream consumers structure their
//     own adapters.
//
//   - Extensibility signal: Users see adapter/json and understand they can
//     create adapter/myformat using the same pattern.
//
// # Data In, Snapshots Out
//
// The data adapters read and write one way. The JSON and CSV parsers return
// [instance.RawInstance] values for the validator. The JSON and CSV serializers
// and the Neo4j adapter's batch node and edge queries take a complete
// [graph.Snapshot]; the Neo4j adapter's statement builders take labels and
// keys. No adapter takes an [instance.ValidInstance].
//
// The generator adapters (gogen, jschema, markdown) take neither: they
// are schema-in, bytes-out. Each consumes a completed schema and emits an
// artifact — Go source, a JSON Schema document, a Markdown reference — with no
// instance-data path and no dependency on instance or graph.
//
// # Dependency Direction
//
// Adapters depend on library packages; library packages never depend on adapters:
//
//	adapter/csv       ──imports──▶  instance, diag, location, location/path, graph,
//	                                immutable, schema, adapter/internal/constraintof,
//	                                adapter/internal/refusal, adapter/internal/typetag
//	adapter/gogen     ──imports──▶  schema, location, internal/ident
//	adapter/jschema   ──imports──▶  schema
//	adapter/json      ──imports──▶  instance, diag, location, location/path, graph,
//	                                immutable, schema, adapter/internal/constraintof,
//	                                adapter/internal/refusal, adapter/internal/typetag,
//	                                github.com/tidwall/jsonc
//	adapter/markdown  ──imports──▶  location, schema, github.com/yuin/goldmark,
//	                                github.com/yuin/goldmark/ast,
//	                                github.com/yuin/goldmark/extension,
//	                                github.com/yuin/goldmark/extension/ast,
//	                                github.com/yuin/goldmark/renderer,
//	                                github.com/yuin/goldmark/renderer/html,
//	                                github.com/yuin/goldmark/text,
//	                                github.com/yuin/goldmark/util,
//	                                golang.org/x/net/html
//	adapter/neo4j     ──imports──▶  schema, graph, immutable, diag, location,
//	                                adapter/internal/constraintof,
//	                                github.com/neo4j/neo4j-go-driver/v6/neo4j/dbtype
//
// # Layering Discipline
//
// The production code of every adapter but one imports no core internal/*
// package, keeping a clean separation between core library internals and the
// adapter layer: the data adapters (csv, json, neo4j) and two of the three
// generators (jschema, markdown) reach only for the public API. The gogen
// adapter is the sole exception: it imports internal/ident, the module's
// rune-aware identifier-casing transform, to derive Go identifiers, and no
// other production package imports it. The rule binds production code
// alone: adapter tests import the module's test support (internal/yammmtest,
// internal/instancetest) and the internals a test checks against
// (internal/source, internal/parse).
//
// The adapter layer's own internal packages — adapter/internal/typetag for a
// type tag's syntax and adapter/internal/refusal for a refusal's class, which
// the JSON and CSV adapters share, and adapter/internal/constraintof for the
// constraint a value renders through, which the Neo4j writer reads too — are
// not an exception to this rule: they are the adapters', not the core's.
//
// # Subpackages
//
//   - [csv]: CSV/TSV adapter with schema-driven type coercion
//   - [gogen]: Go source generator (structs, enums, EDGE_ associations, a Graph aggregate, and the embedded schema source) from a schema and its import closure
//   - [jschema]: JSON Schema (draft 2020-12) generator describing the instance data the validator accepts, for editor-assisted authoring
//   - [json]: JSON adapter that reads JSONC and records a location for every instance it parses
//   - [markdown]: Markdown reference generator with a Mermaid class diagram over the import closure
//   - [neo4j]: Neo4j constraint and index DDL, label mapping, write query generation, and drift diffs against a live database
package adapter
