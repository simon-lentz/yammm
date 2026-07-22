# YAMMM

[![Go Reference](https://pkg.go.dev/badge/github.com/simon-lentz/yammm.svg)](https://pkg.go.dev/github.com/simon-lentz/yammm)
[![Go Version](https://img.shields.io/badge/go-1.26+-blue.svg)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

YAMMM is a Go library for defining schemas in a small DSL (`.yammm` files) and validating Go data against them at runtime. It provides post-validation services including graph traversal and integrity checking.

> **About the name** — YAMMM stands for *Yet Another Meta-Meta Model*, in the modeling-stack sense: your data is the model, the schema that describes it is the metamodel, and the language schemas are written in sits one tier above that. The third *m* is not a typo.

## Features

- **Schema DSL**: Define types, properties, relationships, and constraints in `.yammm` files
- **Runtime validation**: Validate Go maps, structs, and JSON against compiled schemas
- **Relationship modeling**: Associations (references) and compositions (ownership) with multiplicity
- **Invariants**: Boolean constraint expressions evaluated at validation time
- **Schema annotations**: Declare validated `@index` / `@@index` / `@vector` / `@writeOnce` metadata that adapters turn into store DDL
- **Graph construction**: Build in-memory graphs from validated instances with integrity checking
- **Graph persistence**: Save, load, verify, inspect, and edit-metadata-in-place graph snapshots (`.ys` format)
- **Batch assembly**: Concurrent-safe validate → add → check → snapshot pipeline surface, resumable from a prior snapshot
- **Structured diagnostics**: Stable error codes with source location tracking
- **Cross-schema imports**: Modular schemas with sandboxed path resolution
- **Go code generation**: Generate Go structs from schemas with `yammm gen --to go`
- **JSON Schema generation**: Emit editor-ready JSON Schema (draft 2020-12) for instance-data authoring with `yammm gen --to jsonschema`
- **Markdown documentation generation**: Render a schema as a Markdown reference document with a Mermaid class diagram via `yammm gen --to md`
- **CLI tool**: `yammm` binary for snapshot management, data export, and validation

## Installation

```bash
go get github.com/simon-lentz/yammm
```

Requires Go 1.26 or later.

## Quick Start

### Define a Schema

Create a file `vehicles.yammm`:

```yammm
schema "Vehicles"

type Person {
    id UUID primary
    name String[1, 100] required
    age Integer[0, 150]
}

type Car {
    vin String primary
    model String required
    year Integer[1900, 2100] required

    --> OWNER (one) Person
}
```

### Load and Validate

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/simon-lentz/yammm/graph"
    "github.com/simon-lentz/yammm/instance"
    "github.com/simon-lentz/yammm/schema"
    "github.com/simon-lentz/yammm/snapshot"
)

func main() {
    ctx := context.Background()

    // Load schema from file
    s, result := schema.Load(ctx, "vehicles.yammm")
    if result.HasFatal() {
        log.Fatal("load error:", result)
    }
    if result.HasErrors() {
        log.Fatal("schema errors:", result)
    }

    // Create validator and graph
    validator := instance.NewValidator(s)
    g := graph.New(s)

    // Validate a person instance
    personRaw := instance.RawInstance{
        Properties: map[string]any{
            "id":   "550e8400-e29b-41d4-a716-446655440000",
            "name": "Alice",
            "age":  int64(30),
        },
    }

    person, result := validator.ValidateOne(ctx, "Person", personRaw)
    if !result.OK() {
        log.Fatal("validation failed:", result)
    }

    // Add to graph
    result = g.Add(ctx, person)
    if !result.OK() {
        log.Fatal("graph error:", result)
    }

    // Check graph integrity
    result = g.Check(ctx)
    fmt.Println("Graph OK:", result.OK())

    // Save graph snapshot to .ys file
    snap := g.Snapshot()
    data, result := snapshot.Marshal(ctx, snap)
    if !result.OK() {
        log.Fatal("marshal error:", result)
    }
    if err := os.WriteFile("vehicles.ys", data, 0o644); err != nil {
        log.Fatal("write error:", err)
    }
    fmt.Println("Snapshot saved to vehicles.ys")
}
```

### Assemble Batches

For pipeline workloads, `graph.BatchAssembler` composes the validator and graph into a single concurrent-safe surface that encodes the validate → add → check → snapshot ordering:

```go
ba := graph.NewBatchAssembler(ctx, s,
    graph.WithValidatorOptions(instance.RecommendedOptions()...))

for i, rec := range records { // rec is an instance.RawInstance
    if err := ba.Add("Person", rec); err != nil {
        log.Fatalf("record %d: %v", i, err)
    }
}

res, err := ba.Finalize(ctx) // res.Snapshot is non-nil even on error
if err != nil {
    log.Fatal("batch failed:", err)
}
data, result := snapshot.Marshal(ctx, res.Snapshot)
```

To resume a persisted batch on a later run, seed the assembler from a loaded snapshot with `graph.NewBatchAssemblerFromSnapshot(ctx, s, snap, opts...)` — new adds resolve against the seeded instances and `Finalize` checks the union. Snapshot metadata can be rewritten in place, without a full load/re-marshal round-trip, via `snapshot.UpdateMetadataOrReMarshal`.

### Build Instances Programmatically

`instance.BuilderFor` returns a builder bound to a schema type that constructs `RawInstance` values while enforcing schema shape — unknown properties, unknown relations, and cardinality mismatches surface from `Build()` with the offending call site's file:line:

```go
b, err := instance.BuilderFor(s, "Car")
if err != nil {
    log.Fatal(err)
}
raw, err := b.
    Property("vin", "1HGCM82633A004352").
    Property("model", "Accord").
    Property("year", int64(2003)).
    EdgeTo("OWNER", "550e8400-e29b-41d4-a716-446655440000").
    Build()
```

### Build Schemas Programmatically

```go
import (
    "github.com/simon-lentz/yammm/location"
    "github.com/simon-lentz/yammm/schema"
)

s, result := schema.NewBuilder().
    WithName("example").
    WithSourceID(location.MustNewSourceID("test://example.yammm")).
    AddType("Person").
        WithPrimaryKey("id", schema.NewStringConstraint()).
        WithProperty("name", schema.NewStringConstraint()).
        WithOptionalProperty("age", schema.IntegerBetween(0, 150)).
        Done().
    Build()

if result.HasErrors() {
    // Handle schema build errors
}
```

## Architecture

The module is organized into layers with strict dependency ordering:

```text
Primary API (stable)     : schema, instance, graph, snapshot
Foundation (stable)      : location, diag, immutable, format
Adapter                  : adapter/json, adapter/csv, adapter/neo4j, adapter/gogen
LSP                      : lsp (Language Server Protocol server)
CLI                      : cmd/yammm, cmd/yammm-lsp
Internal                 : internal/* (no compatibility guarantees)
```

### Key Packages

| Package | Purpose |
| ------- | ------- |
| `schema` | Type system, constraints, schema loading, and programmatic building |
| `schema/expr` | Expression AST types for invariants |
| `instance` | Instance validation and constraint checking |
| `graph` | Instance graph construction and integrity checking |
| `snapshot` | Graph persistence: marshal, load, verify, and inspect `.ys` files |
| `diag` | Structured diagnostics with stable error codes |
| `location` | Source positions, spans, and canonical paths |
| `immutable` | Immutable data structures for validated output |
| `format` | Canonical `.yammm` file formatting |
| `adapter/json` | JSON/JSONC parsing with location tracking |
| `adapter/csv` | CSV data parsing and writing |
| `adapter/neo4j` | Neo4j constraint generation and Cypher query building |
| `adapter/gogen` | Go source generation from a schema (structs, enums, `EDGE_` structs, `Graph`) |
| `adapter/jschema` | JSON Schema (draft 2020-12) generation from a schema, for editor-assisted data authoring |
| `adapter/markdown` | Markdown + Mermaid documentation generation from a schema |
| `lsp` | Language Server Protocol server for `.yammm` files |

### Entry Point Pattern

Diagnostic-producing operations return `(T, diag.Result)`:

- `result.HasFatal()`: Unrecoverable condition (I/O failure, context cancellation)
- `result.HasErrors()`: Semantic failure (structured issues)
- `result.OK()`: Success (may have warnings)

Pure adapter transformations (JSON/CSV serialization, Cypher, Go source, JSON Schema, and Markdown generation) return `(T, error)`. Snapshot serialization (`snapshot.Marshal`) is diagnostic-producing and returns `(T, diag.Result)`.

## Schema Language

### Types and Properties

```yammm
type Person {
    id UUID primary              // primary key (implicitly required)
    name String[1, 100] required // required with length constraint
    email String                 // optional
    age Integer[0, 150]          // optional with bounds
}
```

### Relationships

Associations reference independent entities:

```yammm
type Person {
    id UUID primary              // primary key (implicitly required)
    name String[1, 100] required // required with length constraint
    email String                 // optional
    age Integer[0, 150]          // optional with bounds
}

type Car {
    vin String primary
    owner_name String required
    mechanic_name List<String>

    --> OWNER (one) Person              // required, single
    --> MECHANICS (many) Person         // optional, multiple
}
```

Compositions embed owned entities:

```yammm
part type Wheel {
    position Enum["FL", "FR", "RL", "RR"] required
}

type Car {
    vin String primary
    *-> WHEELS (one:many) Wheel         // required, at least one
}
```

### Invariants

Invariants are constraint expressions evaluated at validation time:

```yammm
type Person {
    id UUID primary
    name String required
    startDate Date required
    endDate Date

    ! "end date must be after start date" endDate > startDate
    ! "name cannot be empty" name -> Len > 0
}
```

### Data Types

| Type | Description |
| ---- | ----------- |
| `Integer[min, max]` | Signed integer with optional bounds |
| `Float[min, max]` | Floating-point with optional bounds |
| `Boolean` | True/false |
| `String[minLen, maxLen]` | UTF-8 string with optional length bounds |
| `Enum["a", "b", ...]` | Fixed set of string values |
| `Pattern["regex"]` | String matching a regular expression |
| `Timestamp["format"]` | Date-time (default: RFC3339) |
| `Date` | Date without time component |
| `UUID` | Universally unique identifier |
| `Vector[dimensions]` | Fixed-dimension numeric vector |
| `List<T>[min, max]` | Ordered collection of typed values with optional length bounds |

Use `_` for unbounded limits: `Integer[0, _]` means non-negative.

### Imports

```yammm
schema "Main"

import "./common" as common

type Product {
    id UUID primary
    color common.Color required
}
```

## Diagnostics

The `diag` package provides structured diagnostics with stable error codes:

```go
if !result.OK() {
    for issue := range result.Issues() {
        fmt.Printf("[%s] %s: %s\n", issue.Severity(), issue.Code(), issue.Message())
    }
}
```

Diagnostic codes are stable identifiers for programmatic matching (e.g., `E_TYPE_MISMATCH`, `E_MISSING_REQUIRED`, `E_INVARIANT_FAIL`).

## CLI

The `yammm` binary ([`cmd/yammm/`](cmd/yammm/)) provides commands for working with schemas and data:

```bash
yammm validate <schema>                                  # validate a schema, report diagnostics
yammm fmt <schema> [-w]                                  # canonical formatting (stdout; -w rewrites in place)
yammm check <schema> <data>                              # validate JSON/CSV data against a schema
yammm load <schema> <data>                               # build an in-memory graph, report diagnostics
yammm gen --to go <schema>                               # generate Go source from a schema
yammm gen --to jsonschema <schema>                       # generate a JSON Schema for instance-data authoring
yammm gen --to md <schema>                               # generate Markdown docs with a Mermaid class diagram
yammm export <schema> <data> --to <json|csv|cypher>      # export a validated graph
yammm snapshot save <schema> <data...> -o <file.ys>      # build graph, persist
yammm snapshot save <schema> <data...> --into <file.ys>  # merge into an existing snapshot
yammm snapshot info <file.ys>                            # metadata + stats
yammm snapshot verify <schema> <file.ys>                 # schema-compatibility check
yammm snapshot update-metadata --set k=v <file.ys>       # rewrite metadata keys in place (--set/--unset)
yammm neo4j constraints <schema>                         # generate Neo4j constraint statements
yammm neo4j diff <schema>                                # compare schema constraints against a live database
yammm neo4j introspect                                   # inspect a live database's schema
```

## IDE Support

The [`lsp/`](lsp/) package provides a Language Server Protocol server for YAMMM schema files, with the binary entry point at [`cmd/yammm-lsp/`](cmd/yammm-lsp/):

- Real-time diagnostics (parse errors, semantic errors, import issues)
- Go-to-definition for types, properties, and imports
- Hover information with documentation and constraints
- Completion for keywords, types, and snippets
- Document symbols for outline and breadcrumbs
- Formatting with canonical style

Install the [VS Code extension](https://marketplace.visualstudio.com/items?itemName=simon-lentz.yammm) or build the LSP binary from source with `make build`.

## Documentation

- **[Go Reference](https://pkg.go.dev/github.com/simon-lentz/yammm)**: API documentation on pkg.go.dev
- **[Language Specification](docs/SPEC.md)**: DSL reference — grammar, types, expressions, constraints, and diagnostic codes
- **[Library API Reference](docs/API.md)**: Go library reference — loading, validation, graph construction, adapters, and formatting

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
