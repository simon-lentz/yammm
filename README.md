# YAMMM

[![Go Reference](https://pkg.go.dev/badge/github.com/simon-lentz/yammm.svg)](https://pkg.go.dev/github.com/simon-lentz/yammm)
[![Go Version](https://img.shields.io/badge/go-1.24+-blue.svg)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

YAMMM (Yet Another Meta-Meta Model) is a Go library for defining schemas in a small DSL (`.yammm` files) and validating Go data against them at runtime. It provides post-validation services including graph traversal and integrity checking.

## Features

- **Schema DSL**: Define types, properties, relationships, and constraints in `.yammm` files
- **Runtime validation**: Validate Go maps, structs, and JSON against compiled schemas
- **Relationship modeling**: Associations (references) and compositions (ownership) with multiplicity
- **Invariants**: Boolean constraint expressions evaluated at validation time
- **Graph construction**: Build in-memory graphs from validated instances with integrity checking
- **Structured diagnostics**: Stable error codes with source location tracking
- **Cross-schema imports**: Modular schemas with sandboxed path resolution

## Installation

```bash
go get github.com/simon-lentz/yammm
```

Requires Go 1.24 or later.

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
    "github.com/simon-lentz/yammm/schema/load"
)

func main() {
    ctx := context.Background()

    // Load schema from file
    schema, result, err := load.Load(ctx, "vehicles.yammm")
    if err != nil {
        log.Fatal("load error:", err)
    }
    if !result.OK() {
        log.Fatal("schema errors:", result)
    }

    // Create validator and graph
    validator := instance.NewValidator(schema)
    g := graph.New(schema)

    // Validate a person instance
    personRaw := instance.RawInstance{
        Properties: map[string]any{
            "id":   "550e8400-e29b-41d4-a716-446655440000",
            "name": "Alice",
            "age":  int64(30),
        },
    }

    person, failure, err := validator.ValidateOne(ctx, "Person", personRaw)
    if err != nil {
        log.Fatal("validation error:", err)
    }
    if failure != nil {
        log.Fatal("validation failed:", failure.Result)
    }

    // Add to graph
    if result, err := g.Add(ctx, person); err != nil || !result.OK() {
        log.Fatal("graph error")
    }

    // Check graph integrity
    result, _ = g.Check(ctx)
    fmt.Println("Graph OK:", result.OK())
}
```

### Build Schemas Programmatically

```go
import (
    "github.com/simon-lentz/yammm/location"
    "github.com/simon-lentz/yammm/schema"
    "github.com/simon-lentz/yammm/schema/build"
)

s, result := build.NewBuilder().
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
Primary API (stable)     : schema, instance, graph
Foundation (stable)      : location, diag, immutable
Adapter                  : adapter/json
Tooling                  : lsp
Internal                 : internal/* (no compatibility guarantees)
```

### Key Packages

| Package | Purpose |
| ------- | ------- |
| `schema` | Type system, constraints, and schema compilation |
| `schema/load` | Load schemas from `.yammm` files |
| `schema/build` | Programmatic schema construction |
| `instance` | Instance validation and constraint checking |
| `graph` | Instance graph construction and integrity checking |
| `diag` | Structured diagnostics with stable error codes |
| `location` | Source positions, spans, and canonical paths |
| `adapter/json` | JSON/JSONC parsing with location tracking |

### Entry Point Pattern

All public entry points follow the `(Output, diag.Result, error)` pattern:

- `err != nil`: Catastrophic failure (I/O, internal corruption)
- `err == nil && !result.OK()`: Semantic failure (structured issues)
- `err == nil && result.OK()`: Success (may have warnings)

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
    startDate Date required
    endDate Date

    ! "end date must be after start date" endDate > startDate
    ! "name cannot be empty" Len(name) > 0
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
    for _, issue := range result.Issues() {
        fmt.Printf("[%s] %s: %s\n", issue.Severity, issue.Code, issue.Message)
    }
}
```

Diagnostic codes are stable identifiers for programmatic matching (e.g., `E_TYPE_MISMATCH`, `E_MISSING_REQUIRED`, `E_INVARIANT_FAIL`).

## IDE Support

The `lsp` package provides a Language Server Protocol server for YAMMM schema files:

- Real-time diagnostics (parse errors, semantic errors, import issues)
- Go-to-definition for types, properties, and imports
- Hover information with documentation and constraints
- Completion for keywords, types, and snippets
- Document symbols for outline and breadcrumbs
- Formatting with canonical style

See [`lsp/editors/vscode/README.md`](lsp/editors/vscode/README.md) for VS Code extension setup.

### LSP Quickstart

Get the full VS Code editing experience for `.yammm` files in a few steps:

**Prerequisites**: Go 1.24+, Node.js 18+, npm

```bash
# Clone the repository
git clone https://github.com/simon-lentz/yammm.git
cd yammm

# Build the LSP server and VS Code extension
make build-vscode

# Package the extension as a .vsix file
make package-vscode
```

Then install in VS Code:

1. Open the Command Palette (`Cmd+Shift+P` / `Ctrl+Shift+P`)
2. Run **Extensions: Install from VSIX...**
3. Select `lsp/editors/vscode/yammm-0.1.0.vsix`
4. Reload VS Code

Open any `.yammm` file to enjoy syntax highlighting, diagnostics, completions, go-to-definition, and formatting.

## Documentation

- **[Go Reference](https://pkg.go.dev/github.com/simon-lentz/yammm)**: API documentation on pkg.go.dev
- **[Language Specification](docs/SPEC.md)**: Complete DSL reference with grammar, expressions, and built-in functions

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
