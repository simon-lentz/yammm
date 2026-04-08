# Quick DSL Reference

Compact syntax overview for `.yammm` schema files. For the full grammar, see `dsl-syntax.md`. For constraint type details, see `type-system.md`.

---

## Schema Structure

```yammm-snippet
schema "name"
import "./path" as alias         // alias optional, auto-derived from path
type AliasName = BuiltInType     // custom data type alias
```

Reserved keywords: `schema`, `import`, `as`, `type`, `datatype`, `abstract`, `part`, `extends`, `includes`, `required`, `primary`, `one`, `many`, `in`, `true`, `false`, `nil`. Built-in type names (`Integer`, `Float`, `Boolean`, `String`, `Enum`, `Pattern`, `Timestamp`, `Date`, `UUID`, `Vector`, `List`) are also reserved.

---

## Type Definitions

```yammm-snippet
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
|------|--------|-------|
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

```yammm-snippet
--> REL_NAME (multiplicity) TargetType              // association
--> REL_NAME (multiplicity) TargetType { props }    // with edge properties
--> REL_NAME (many) Target / reverse_name (one)     // reverse clause
*-> REL_NAME (multiplicity) PartType                 // composition
```

| Multiplicity | Required | Cardinality |
|-------------|----------|-------------|
| (omitted) / `(_)` / `(_:one)` | No | One |
| `(one)` / `(one:one)` | Yes | One |
| `(_:many)` / `(many)` | No | Many (0+) |
| `(one:many)` | Yes | Many (1+) |

---

## Invariants

```yammm-snippet
! "error_id" expression
! "guard" end_date == nil || end_date > start_date
! "all_valid" ITEMS -> All |$item| { $item.quantity > 0 }
```

Built-in functions are capitalized: `Len`, `All`, `Any`, `Filter`, `Map`, `Reduce`, `Contains`, `Sum`, `Default`, `Coalesce`, `IsNil`, etc. See `expressions.md` for the full expression language.
