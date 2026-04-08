# Graph Traversal Reference

The `graph/walk` package provides programmatic traversal of graph snapshots using the Visitor pattern. Traversal order is deterministic.

```go
import "github.com/simon-lentz/yammm/graph/walk"
```

---

## Walk (Full Graph)

```go
func Walk(ctx context.Context, result *graph.Snapshot, visitor Visitor, opts ...Option) error
```

Traverses the entire graph snapshot, calling visitor methods for each instance, property, edge, and composition. Returns on the first error from the visitor or if the context is cancelled.

## Instance (Single Subtree)

```go
func Instance(ctx context.Context, inst *graph.Instance, visitor Visitor, opts ...Option) error
```

Traverses a single instance and its composed children. Does **not** call `VisitEdge` (edges require full graph context). Useful for inspecting one instance subtree without traversing the entire graph.

---

## Visitor Interface

```go
type Visitor interface {
    EnterInstance(inst *graph.Instance) error
    ExitInstance(inst *graph.Instance) error
    VisitProperty(inst *graph.Instance, name string, value immutable.Value) error
    VisitEdge(edge *graph.Edge) error
    EnterComposition(inst *graph.Instance, relationName string) error
    ExitComposition(inst *graph.Instance, relationName string) error
}
```

### BaseVisitor

Embed `BaseVisitor` to implement only the methods you need:

```go
type MyVisitor struct {
    walk.BaseVisitor
    count int
}

func (v *MyVisitor) EnterInstance(_ *graph.Instance) error {
    v.count++
    return nil
}
```

`BaseVisitor` provides no-op implementations for all 6 methods.

---

## Traversal Ordering

All ordering is deterministic and reproducible:

| Element | Order |
|---------|-------|
| Types | Lexicographic by type name |
| Instances | Primary key order within each type |
| Properties | Alphabetic by property name |
| Edges | Sorted per snapshot edge ordering |
| Compositions | Relation name order |
| Composed children | Primary key order (or insertion order if PKs are zero) |

---

## Options

```go
walk.WithLogger(logger *slog.Logger)    // enable debug logging
```

---

## Error Handling

- If any visitor method returns a non-nil error, traversal stops immediately and `Walk`/`Instance` returns that error.
- Context cancellation stops traversal and returns the context error.
- `walk.ErrNilVisitor` is returned if the visitor is nil.
- `Walk` returns nil if the snapshot is nil. `Instance` returns nil if the instance is nil.

---

## Visit Order for Each Instance

For each instance, the walker calls methods in this order:

1. `EnterInstance(inst)`
2. For each property (alphabetic): `VisitProperty(inst, name, value)`
3. For each composition (by relation name):
   a. `EnterComposition(inst, relationName)`
   b. Recursively visit each composed child (same order)
   c. `ExitComposition(inst, relationName)`
4. For each edge (sorted): `VisitEdge(edge)` *(Walk only, skipped by Instance)*
5. `ExitInstance(inst)`

---

## Example: Count Instances by Type

```go
type Counter struct {
    walk.BaseVisitor
    counts map[string]int
}

func (c *Counter) EnterInstance(inst *graph.Instance) error {
    c.counts[inst.TypeName()]++
    return nil
}

counter := &Counter{counts: make(map[string]int)}
if err := walk.Walk(ctx, snap, counter); err != nil {
    return err
}
// counter.counts["User"] == 42, etc.
```

## Example: Extract Property Values

```go
type Extractor struct {
    walk.BaseVisitor
    emails []string
}

func (e *Extractor) VisitProperty(inst *graph.Instance, name string, val immutable.Value) error {
    if inst.TypeName() == "User" && name == "email" {
        if s, ok := val.AsString(); ok {
            e.emails = append(e.emails, s)
        }
    }
    return nil
}
```
