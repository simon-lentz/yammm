# YAMMM

[![Go Reference](https://pkg.go.dev/badge/github.com/simon-lentz/yammm.svg)](https://pkg.go.dev/github.com/simon-lentz/yammm)
[![Go Version](https://img.shields.io/badge/go-1.26.10+-blue.svg)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

YAMMM is a Go library for defining schemas in a small DSL (`.yammm` files) and validating Go data against them at runtime. It provides post-validation services including graph traversal and integrity checking.

> **About the name** — YAMMM stands for *Yet Another Meta-Meta Model*, in the modeling-stack sense: your data is the model, the schema that describes it is the metamodel, and the language schemas are written in sits one tier above that. The third *m* is not a typo.

## Features

- **Schema DSL**: Define types, properties, relationships, and constraints in `.yammm` files
- **Runtime validation**: Validate Go maps against compiled schemas, and JSON data files with `yammm check`
- **Relationship modeling**: Associations (references) and compositions (ownership) with multiplicity
- **Invariants**: Boolean constraint expressions evaluated at validation time
- **Schema annotations**: Declare validated `@index` / `@@index` / `@vector` / `@fulltext` / `@@fulltext` metadata that adapters turn into store DDL, and `@writeOnce` properties that the Neo4j write path sets once
- **Graph construction**: Build in-memory graphs from validated instances with integrity checking
- **Graph persistence**: Save, load, verify, inspect, and edit-metadata-in-place graph snapshots (`.ys` format)
- **Batch assembly**: Validate and add from many workers through one validator and one graph, merging a record listed under several parents, and resume a batch from a prior snapshot
- **Structured diagnostics**: Stable error codes with source location tracking
- **Cross-schema imports**: Modular schemas with sandboxed path resolution
- **Go code generation**: Generate Go structs from schemas with `yammm gen --to go`
- **Markdown documentation generation**: Render a schema as a Markdown reference document with a Mermaid class diagram via `yammm gen --to md`
- **CLI tool**: `yammm` binary for validation, formatting, data checking, snapshot management, generation, Neo4j DDL, and a diff against and an introspection of a live Neo4j database

## Installation

```bash
go get github.com/simon-lentz/yammm
```

Requires Go 1.26.10 or later.

## Quick Start

### Define a Schema

Create a file `vehicles.yammm`:

```yammm-schema
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
    if person.IsZero() {
        log.Fatal("refused: its producer reported the value") // an instance.Refused
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

A pipeline validates each record and adds what the validator returns. `instance.Validator` and `graph.Graph` are both safe for concurrent use, so one validator and one graph serve every worker. Each call returns its own result, and a `Snapshot` holds no diagnostics, so a worker keeps the results it needs. At the end of the batch, wait for the workers, then check the graph and take its snapshot:

```go
v := instance.NewValidator(s)
g := graph.New(s)

for i, rec := range records { // rec is an instance.RawInstance
    valid, result := v.ValidateOne(ctx, "Person", rec)
    if result.HasErrors() {
        log.Fatalf("record %d: %s", i, result)
    }
    if valid.IsZero() { // the record holds an instance.Refused value
        log.Fatalf("record %d: %v", i, instance.ErrRefusedValue)
    }
    if result := g.Add(ctx, valid); result.HasErrors() {
        log.Fatalf("record %d: %s", i, result)
    }
}

if result := g.Check(ctx); result.HasErrors() {
    log.Fatal("batch failed:", result.Err())
}
data, result := snapshot.Marshal(ctx, g.Snapshot())
```

For a source that lists one record under several parents, `g.AddOrMerge(ctx, valid)` takes `Add`'s place: at a primary key the graph already holds, a record with no composed children merges: its association targets are installed on the held root and `merged` is true. A record with composed children is refused there as `Add` refuses a duplicate (`E_DUPLICATE_PK`), and a merge that would give a `(one)` association a second target is refused whole (`E_GRAPH_CARDINALITY`); `merged` is false whenever the result holds an error.

To resume a persisted batch on a later run, seed the graph from a loaded snapshot with `graph.NewFromSnapshot(s, snap)`, which takes a snapshot bound to the schema as it stands and refuses one built against another schema that breaks any of the schema's structural facts, save that it derives the Records fact's records again, refusing an `empty` record under a (one) association, and drops an absent record under a relation that is no association of the schema — new adds resolve against the seeded instances and `Check` covers the union. `snapshot.UpdateMetadata` rewrites a snapshot's metadata in place, without a full load/re-marshal round-trip; `snapshot.UpdateMetadataOrReMarshal` falls back to that round-trip, with `W_UPDATE_METADATA_FALLBACK`, when the in-place rewrite refuses for any reason but a cancelled context or metadata that is not valid UTF-8.

### Build Instances Programmatically

`instance.BuilderFor` returns a builder bound to a schema type that constructs `RawInstance` values while enforcing schema shape — unknown properties, unknown relations, edge shapes it cannot build (a composition, an association with edge properties, a target key of the wrong arity), and cardinality mismatches surface from `Build()` with the offending call site's file:line:

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

## Architecture

The module is organized into layers with strict dependency ordering:

```text
Primary API              : schema, instance, graph, snapshot
Foundation               : location, diag, immutable
Tooling                  : format
Adapter                  : adapter/neo4j
Generator                : schema/gogen, schema/markdown
LSP                      : lsp (Language Server Protocol server)
CLI                      : cmd/yammm, cmd/yammm-lsp
Internal                 : internal/* (no compatibility guarantees)
```

### Key Packages

| Package | Purpose |
| ------- | ------- |
| `schema` | Type system, constraints, and schema loading |
| `instance` | Instance validation and constraint checking |
| `graph` | Instance graph construction and integrity checking |
| `snapshot` | Graph persistence as `.ys` files; its package doc lists what it does |
| `diag` | Structured diagnostics with stable error codes |
| `location` | Source positions, spans, and canonical paths |
| `immutable` | Immutable data structures for validated output |
| `format` | Canonical `.yammm` file formatting |
| `adapter/neo4j` | The Neo4j mapping of a schema; its package doc lists what it holds |
| `schema/gogen` | Go source generation from a schema; its package doc lists what it emits |
| `schema/markdown` | Markdown + Mermaid documentation generation from a schema |
| `lsp` | Language Server Protocol server for `.yammm` files |

### Entry Point Pattern

Diagnostic-producing operations return `(T, diag.Result)`:

- `result.HasFatal()`: Any Fatal issue (such as an I/O failure, a cancellation, an internal fault the validator or the snapshot writer reports, input a reader cannot go past, or an argument or option a loader refuses; the schema loader reports its internal faults at Error)
- `result.HasErrors()`: Any Error or Fatal issue
- `result.OK()`: Success (may have warnings). A validator's zero instance beside it held an `instance.Refused` value, which its producer reported

The pure transformations of the Neo4j adapter and the generators (Cypher batch queries, Go source, and Markdown generation) return their output beside a Go `error`, as `(T, error)`, but for `adapter/neo4j`'s `BuildBatchRelationshipMergeQuery`, a query template that cannot fail; the Neo4j adapter's schema-derived DDL and shape builders report through a `diag.Result`, and its drop-statement builders return `(string, error)`. Snapshot serialization (`snapshot.Marshal`) is diagnostic-producing and returns `(T, diag.Result)`.

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

### Annotations

Annotations attach validated, store-agnostic metadata that adapters turn into store DDL. They do not change which instance data is valid, and they are excluded from the structural hash, so adding one never invalidates a persisted snapshot.

```yammm
type Document {
    content_hash String      primary
    state        String      @index
    title        String      @fulltext
    published_on Date
    embedding    Vector[768] @vector(cosine)
    first_seen   Timestamp   @writeOnce

    @@index(state, published_on)
    @@fulltext(title, state)
}
```

| Annotation | Placement | Meaning |
| ---------- | --------- | ------- |
| `@index` | property | Single-property range index on a scalar |
| `@@index(a, b, …)` | type | Composite range index; argument order is significant |
| `@vector(cosine\|euclidean)` | property | ANN vector index on a `Vector[N]` property |
| `@fulltext` | property | Single-property fulltext index on a text property (`String`, `Pattern`, `Enum`) |
| `@@fulltext(a, b, …)` | type | Fulltext index scored across the referenced text properties |
| `@writeOnce` | property | Set on node creation, never rewritten |

`yammm neo4j indexes <schema>` emits the index DDL; `yammm neo4j diff` compares it against a live database. See [SPEC.md](docs/SPEC.md#annotations) for eligibility rules and diagnostics.

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
yammm fmt <schema>... [-w|--check]                       # canonical formatting (stdout; -w rewrites, --check lists unformatted)
yammm check <schema> <data...>                           # validate JSON data files, read as one document
yammm gen --to go <schema>                               # generate Go source from a schema
yammm gen --to md <schema>                               # generate Markdown docs with a Mermaid class diagram
yammm snapshot save <schema> <data...> -o <file.ys>      # build graph, persist
yammm snapshot save <schema> <data...> --into <file.ys>  # merge into an existing snapshot
yammm snapshot info <file.ys>                            # metadata + stats
yammm snapshot verify <schema> <file.ys>                 # schema-compatibility check
yammm snapshot update-metadata --set k=v <file.ys>       # rewrite metadata keys in place (--set/--unset)
yammm neo4j constraints <schema>                         # generate Neo4j constraint statements
yammm neo4j indexes <schema>                             # generate Neo4j index statements from annotations
yammm neo4j diff <schema>                                # compare schema constraints and indexes against a live database
yammm neo4j introspect                                   # inspect a live database's schema
```

Every command writes its diagnostics to **stderr**, at every severity, and its
results to stdout. A warning is not a failure — a run that prints warnings and
nothing else still exits `0` — so do not read a zero exit code as "no
diagnostics." (Before v0.9.0 a warnings-only result printed nothing at all, so
an unchanged schema may emit new stderr output; the `Result` a library caller
receives is unchanged.)

## IDE Support

The [`lsp/`](lsp/) package provides a Language Server Protocol server for YAMMM schema files, with the binary entry point at [`cmd/yammm-lsp/`](cmd/yammm-lsp/):

- Real-time diagnostics (parse errors, semantic errors, import issues)
- Go-to-definition for types, properties, and imports
- Hover information with documentation, constraints, and annotations
- Completion for keywords, types, snippets, and annotations (names and arguments, sourced from the built-in registry)
- Document symbols for outline and breadcrumbs
- Formatting with canonical style

Install the [VS Code extension](https://marketplace.visualstudio.com/items?itemName=simon-lentz.yammm) or build the LSP binary from source with `make build`.

## Documentation

- **[Go Reference](https://pkg.go.dev/github.com/simon-lentz/yammm)**: API documentation on pkg.go.dev
- **[Language Specification](docs/SPEC.md)**: DSL reference — grammar, types, expressions, constraints, and diagnostic codes
- **[Library API Reference](docs/API.md)**: Go library reference and versioning policy

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
