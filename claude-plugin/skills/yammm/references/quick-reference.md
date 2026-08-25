# Quick DSL Reference

Compact syntax overview for `.yammm` schema files. For the full grammar, see `dsl-syntax.md`. For constraint type details, see `type-system.md`.

---

## Schema Structure

```text
schema "name"
import "./path" as alias         // alias optional, auto-derived from path
type AliasName = BuiltInType     // custom data type alias
```

Reserved keywords: `schema`, `import`, `as`, `type`, `datatype`, `abstract`, `part`, `extends`, `includes`, `required`, `primary`, `one`, `many`, `in`, `true`, `false`, `nil`. Built-in type names (`Integer`, `Float`, `Boolean`, `String`, `Enum`, `Pattern`, `Timestamp`, `Date`, `UUID`, `Vector`, `List`) are also reserved.

---

## Type Definitions

```text
type Concrete { ... }                              // instantiable
abstract type Shared { ... }                       // must be extended
part type Owned { ... }                            // composition-only
type Child extends Parent1, Parent2 { ... }        // inheritance
```

---

## Properties

```yammm-snippet
field_name Type primary    // unique, implicitly required
field_name Type required   // must be non-null
field_name Type            // optional (nullable)
```

Using `primary required` together is a parse error -- `primary` already implies required.

---

## Built-in Constraint Types

| Type | Syntax | Notes |
| ---- | ------ | ----- |
| `String` | `String[min, max]` | Length in runes; `_` = unbounded |
| `Integer` | `Integer[min, max]` | Signed; negative bounds allowed |
| `Float` | `Float[min, max]` | Inclusive bounds |
| `Boolean` | `Boolean` | No parameters |
| `Timestamp` | `Timestamp` or `Timestamp["format"]` | Default RFC3339 |
| `Date` | `Date` | Date only |
| `UUID` | `UUID` | UUID string |
| `Enum` | `Enum["a", "b", ...]` | Minimum 2 options |
| `Pattern` | `Pattern["regex"]` or `Pattern["r1", "r2"]` | Conjunction: must match all |
| `Vector` | `Vector[dims]` | Fixed-dimension numeric |
| `List` | `List<Type>` or `List<Type>[min, max]` | Ordered collection |

Primary key types: `String`, `UUID`, `Date`, `Timestamp` only. Aliases that resolve to these are accepted.

---

## Relationships

```text
--> REL_NAME (multiplicity) TargetType              // association
--> REL_NAME (multiplicity) TargetType { props }    // with edge properties
*-> REL_NAME (multiplicity) PartType                 // composition
```

| Multiplicity | Required | Cardinality |
| ----------- | -------- | ----------- |
| (omitted) / `(_)` / `(_:one)` | No | One |
| `(one)` / `(one:one)` | Yes | One |
| `(_:many)` / `(many)` | No | Many (0+) |
| `(one:many)` | Yes | Many (1+) |

---

## Annotations

Validated metadata for adapters (indexes, write-once). Property-level `@name`, type-level `@@name`.

```yammm-snippet
state      String      @index                // single-property range index
title      String      @fulltext             // single-property fulltext index
embedding  Vector[768] @vector(cosine)       // cosine | euclidean
first_seen Timestamp   @writeOnce            // set on create only
@@index(state, published_on)                 // composite range index (ordered)
@@fulltext(title, state)                     // multi-property fulltext index
```

- `@index` — a scalar property that is not the sole primary key.
- `@@index(p, …)` — one or more scalar references (order matters); primary-key members allowed.
- `@vector(sim)` — a `Vector[N]` property; `sim` is `cosine` or `euclidean`.
- `@fulltext` — a text property (`String`, `Pattern`, `Enum`); primary keys allowed (the range-index redundancy rule does not apply).
- `@@fulltext(p, …)` — one or more text references (order matters); primary-key members allowed.
- `@writeOnce` — any non-primary-key property.
- An annotation trails the datatype and any modifier; `@index()` (empty parens) is a syntax error.

---

## Invariants

```yammm-snippet
! "error_id" expression
! "guard" end_date == nil || end_date > start_date
! "all_valid" ITEMS -> All |$item| { $item.quantity > 0 }
```

Built-in functions are capitalized: `Len`, `All`, `Any`, `Filter`, `Map`, `Reduce`, `Contains`, `Sum`, `Default`, `Coalesce`, `IsNil`, etc. See `expressions.md` for the full expression language.
