# YAMMM

[![Go Reference](https://pkg.go.dev/badge/github.com/simon-lentz/yammm.svg)](https://pkg.go.dev/github.com/simon-lentz/yammm)
[![Go Version](https://img.shields.io/badge/go-1.26+-blue.svg)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

YAMMM (Yet Another Meta-Meta Model) is a Go library for defining schemas in a small DSL (`.yammm` files) and validating Go data against them at runtime. It provides post-validation services including graph traversal and integrity checking.

## Features

- **Schema DSL**: Define types, properties, relationships, and constraints in `.yammm` files
- **Runtime validation**: Validate Go maps, structs, and JSON against compiled schemas
- **Relationship modeling**: Associations (references) and compositions (ownership) with multiplicity
- **Invariants**: Boolean constraint expressions evaluated at validation time
- **Graph construction**: Build in-memory graphs from validated instances with integrity checking
- **Graph persistence**: Save, load, verify, and inspect graph snapshots (`.ys` format)
- **Structured diagnostics**: Stable error codes with source location tracking
- **Cross-schema imports**: Modular schemas with sandboxed path resolution
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

    "github.com/simon-lentz/yammm/graph"
    "github.com/simon-lentz/yammm/instance"
    "github.com/simon-lentz/yammm/schema"
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
}
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
        WithPrimaryKey("id", schema.StringConstraint{}).
        WithProperty("name", schema.StringConstraint{}).
        WithOptionalProperty("age", schema.IntegerConstraint{}).
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
Adapter                  : adapter/json, adapter/csv, adapter/neo4j
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
| `graph/walk` | Visitor-pattern graph traversal |
| `snapshot` | Graph persistence: marshal, load, verify, and inspect `.ys` files |
| `diag` | Structured diagnostics with stable error codes |
| `location` | Source positions, spans, and canonical paths |
| `immutable` | Immutable data structures for validated output |
| `format` | Canonical `.yammm` file formatting |
| `adapter/json` | JSON/JSONC parsing with location tracking |
| `adapter/csv` | CSV data parsing and writing |
| `adapter/neo4j` | Neo4j constraint generation and Cypher query building |
| `lsp` | Language Server Protocol server for `.yammm` files |

### Entry Point Pattern

Diagnostic-producing operations return `(T, diag.Result)`:

- `result.HasFatal()`: Unrecoverable condition (I/O failure, context cancellation)
- `result.HasErrors()`: Semantic failure (structured issues)
- `result.OK()`: Success (may have warnings)

Pure transformations (serialization, query generation) return `(T, error)`.

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
    *-> WHEELS (one:many) Wheel         // required, at least one
}
```

### Invariants

Invariants are constraint expressions evaluated at validation time:

```yammm
type Person {
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
yammm snapshot save <schema> <data...> -o <file.ys>    # build graph, persist
yammm snapshot save <schema> <data...> --into <file.ys> # merge into existing
yammm snapshot info <file.ys>                           # metadata + stats
yammm snapshot verify <schema> <file.ys>                # schema compat check
yammm export <schema> <data...> -o <file>               # CSV/JSON export
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
- **[Language Specification](docs/SPEC.md)**: Complete DSL reference with grammar, expressions, and built-in functions

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
