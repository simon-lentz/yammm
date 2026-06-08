# DSL Syntax Reference

Full grammar reference for `.yammm` schema files. For a compact overview, see `quick-reference.md`.

---

## Schema Declaration

Every `.yammm` file begins with a schema declaration:

```yammm-snippet
schema "inventory"
```

The name is a quoted string. One schema per file.

---

## Imports

```yammm-snippet
import "./sibling_schema" as sibling       // relative path, explicit alias
import "../parent/schema" as parent        // relative path (up)
import "models/core/users" as users        // module path (from module root)
import "./defaults"                        // alias auto-derived as "defaults"
```

### Rules

- The `as alias` clause is optional. When omitted, the alias is auto-derived from the last path segment of the import path.
- Aliases must start with a letter (a-z, A-Z).
- Reserved keywords cannot be used as aliases: `schema`, `import`, `as`, `type`, `datatype`, `abstract`, `part`, `extends`, `includes`, `required`, `primary`, `one`, `many`, `in`. The literals `true`, `false`, and `nil` are also reserved. Built-in type names (`Integer`, `Float`, `Boolean`, `String`, `Enum`, `Pattern`, `Timestamp`, `Date`, `UUID`, `Vector`, `List`) are reserved because the lexer tokenizes them as literal tokens rather than identifiers.
- Circular imports are detected and rejected.
- Paths are sandboxed and cannot escape the module root.
- The `.yammm` extension is optional and appended if absent.
- Imported types and aliases must be qualified: `alias.TypeName`, `alias.MoneyType`.

---

## Custom Data Type Aliases

```yammm-snippet
type Email = Pattern["^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$"]
type Percentage = Float[0.0, 100.0]
type PositiveInt = Integer[1, _]
type Tags = List<String[1, 50]>
```

- Declared with uppercase names using `type Name = BuiltInType`.
- Aliases preserve the declared case (not lowercased internally).
- Can chain: alias A wraps a built-in, alias B wraps A.
- Cycles are detected and rejected during schema completion.
- Imported aliases require qualification: `common.Money`.

---

## Type Definitions

### Concrete Types

```yammm-snippet
type Product {
    id UUID primary
    name String[1, 200] required
    description String
}
```

Instantiable types. Every concrete type must have at least one `primary` field; multiple `primary` fields form a composite key.

### Abstract Types

```yammm-snippet
abstract type Auditable {
    created_at Timestamp required
    updated_at Timestamp
}
```

Cannot be instantiated. Define shared structure inherited by concrete types via `extends`. An abstract type may declare a `primary` field, which its concrete subtypes inherit; it is not itself required to have one.

### Part Types

```yammm-snippet
part type Address {
    street String[1, 200] required
    city String[1, 100] required
}
```

Owned by a parent via composition (`*->`). Cannot be instantiated directly. Cannot declare `primary` fields. Cannot declare associations (`-->`). Can only be targets of composition relationships.

### Inheritance

```yammm-snippet
type Document extends Auditable, Named {
    id UUID primary
    content String required
}
```

Multiple parents allowed (comma-separated). Inherits properties, associations, compositions, and invariants. Invariants with duplicate names are deduplicated (child version takes precedence).

---

## Property Definitions

```yammm-snippet
field_name Type primary    // primary key: unique, implicitly required
field_name Type required   // must be non-null
field_name Type            // optional (nullable)
```

- Names must start with a lowercase letter.
- Using `primary required` together is a **parse error** -- the grammar treats them as mutually exclusive (`(is_primary | is_required)?`). `primary` already implies required.

### Built-in Constraint Types

| Type | Syntax | Description |
|------|--------|-------------|
| `String` | `String[min, max]` | String with rune-length bounds |
| `Integer` | `Integer[min, max]` | Signed integer with inclusive bounds |
| `Float` | `Float[min, max]` | Floating point with inclusive bounds |
| `Boolean` | `Boolean` | True/false, no parameters |
| `Timestamp` | `Timestamp` or `Timestamp["fmt"]` | Date-time, default RFC3339 |
| `Date` | `Date` | Date only, no parameters |
| `UUID` | `UUID` | UUID string, no parameters |
| `Enum` | `Enum["a", "b", ...]` | Fixed set, minimum 2 options |
| `Pattern` | `Pattern["regex"]` or `Pattern["r1", "r2"]` | Regex-validated string. Max 2 patterns; both must match (conjunction). |
| `Vector` | `Vector[dims]` | Fixed-dimension numeric vector |
| `List` | `List<Type>[min, max]` | Ordered collection, optional length bounds |

### Primary Key Restrictions

Only `String`, `UUID`, `Date`, and `Timestamp` may be primary keys. `Integer`, `Float`, `Boolean`, `Enum`, `Pattern`, `Vector`, and `List` are rejected. Alias resolution applies -- a `DataType` alias resolving to an allowed type is accepted.

### Bound Syntax

- `_` means unbounded: `Integer[0, _]`, `Float[_, 100.0]`
- Both bounds required when brackets present: `String[1, 255]`
- Exact value: `String[2, 2]`
- Negative bounds allowed for Integer/Float: `Integer[-40, 50]`
- Integer literals valid as Float bounds: `Float[0, 1.0]`
- List length bounds after angle brackets: `List<String>[1, 10]`

---

## Relationships

### Associations (`-->`)

References between independently existing types:

```yammm-snippet
--> REL_NAME (multiplicity) TargetType
--> REL_NAME (multiplicity) alias.TargetType        // cross-schema
```

Associations cannot target part types.

### Edge Properties

Associations can carry their own properties:

```yammm-snippet
--> WORKS_AT (one) Company {
    start_date Date required
    title String[1, 100]
}
```

Edge properties cannot use `Vector` or `List` types.

### Reverse Clause

Declares the inverse relationship name:

```yammm-snippet
--> OWNS (many) Asset / owned_by (one)
```

### Compositions (`*->`)

Embeds part-type children within their parent:

```yammm-snippet
*-> ITEMS (one:many) LineItem
```

Composition targets must be `part type`.

### Multiplicity

| Syntax | Required | Cardinality |
|--------|----------|-------------|
| (omitted) / `(_)` / `(_:one)` | No | One |
| `(one)` / `(one:one)` | Yes | One |
| `(_:many)` / `(many)` | No | Many (0+) |
| `(one:many)` | Yes | Many (1+) |

---

## Invariants

Business logic constraints evaluated after type checking:

```yammm-snippet
! "error_id" expression
```

The error ID is a quoted string. Must be unique within a type.

```yammm
type Product {
    product_id String primary
    name String[1, 100] required
    price Float[0.0, _] required
    discount Float[0.0, 100.0]

    ! "name_not_blank" name != ""
    ! "discount_reasonable" discount == nil || discount <= 50.0
}
```

### Collection Invariants

Access composition children by relationship name, then use collection functions:

```yammm-snippet
! "has_items" ITEMS -> Len > 0
! "all_valid" ITEMS -> All |$item| { $item.quantity > 0 }
! "total_check" ITEMS -> Map |$i| { $i.price } -> Sum >= 10.0
```

See `references/expressions.md` for the full expression language, operator precedence, pipeline syntax, lambda syntax, and all built-in functions.
