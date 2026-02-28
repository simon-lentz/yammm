---
name: yammm-dsl
description: Use when working with .yammm schema files, writing yammm DSL schemas, understanding yammm syntax, or answering questions about the yammm type system, relationships, invariants, or expression language. Triggers on .yammm file operations and yammm DSL discussions.
---

# Yammm DSL Skill

Yammm (Yet Another Meta-Meta Model) is a domain-specific language for expressing typed data schemas with properties, relationships, invariants, and structured diagnostics.

**Detailed references** (loaded on demand):

- `references/expressions.md` -- Full expression language: operators, precedence, pipeline, lambdas, all built-in functions
- `references/type-system.md` -- Built-in constraint types, aliases, abstract/part types, inheritance and narrowing
- `references/patterns.md` -- Common schema patterns: audit fields, soft delete, normalization, relationship idioms

**Canonical specification**: `docs/SPEC.md` in the yammm repository (42KB). Consult it for edge cases not covered here or in references.

---

## Schema Structure

Every `.yammm` file defines a single schema. The file begins with a schema declaration, followed by optional imports, optional data type aliases, and type definitions.

```yammm-snippet
schema "inventory"

import "./common" as common

type ItemCode = String[3, 10]

type Item {
    code ItemCode primary
    name String[1, 255] required
}

abstract type Auditable {
    created_at Timestamp required
    updated_at Timestamp
}

part type Tag {
    label String[1, 50] required
}
```

**Keywords**: `schema`, `import`, `as`, `type`, `datatype`, `abstract`, `part`, `extends`, `includes`, `required`, `primary`, `one`, `many`, `in`.

**Type modifiers**:

| Modifier | Meaning |
| -------- | ------- |
| (none) | Concrete, instantiable type |
| `abstract` | Cannot be instantiated; must be extended |
| `part` | Composition-only; owned by a parent via `*->` |

---

## Property Definitions

Properties are the data fields of a type. Names must start with a lowercase letter.

```yammm-snippet
field_name Type primary    // Primary key (unique, implicitly required)
field_name Type required   // Must be non-null
field_name Type            // Optional (can be null)
```

### Built-in Constraint Types

| Type | Syntax | Description |
| ---- | ------ | ----------- |
| `String` | `String[min, max]` | String with length bounds (runes) |
| `Integer` | `Integer[min, max]` | Signed integer with bounds |
| `Float` | `Float[min, max]` | Floating point with bounds |
| `Boolean` | `Boolean` | True/false |
| `Timestamp` | `Timestamp` or `Timestamp["format"]` | ISO 8601 datetime (default RFC3339) |
| `Date` | `Date` | Date only (no time component) |
| `UUID` | `UUID` | UUID string |
| `Enum` | `Enum["a", "b", "c"]` | Enumeration (minimum 2 options) |
| `Pattern` | `Pattern["regex"]` | Regex-validated string |
| `Vector` | `Vector[dimensions]` | Fixed-dimension numeric vector |
| `List` | `List<ElementType>` or `List<ElementType>[min, max]` | Ordered collection with optional length bounds |

See `references/type-system.md` for detailed semantics, bound rules, and examples for each type.

### Primary Key Types

Only `String`, `UUID`, `Date`, and `Timestamp` are allowed as primary key types. All other types (Integer, Float, Boolean, Enum, Pattern, Vector, List) are rejected. Alias resolution applies: a `DataType` alias that resolves to an allowed type is accepted.

### Bound Syntax

- `_` means unbounded: `Integer[0, _]` (non-negative, no upper bound)
- Both bounds required when brackets present: `String[1, 255]`
- Exact value: `String[2, 2]` (exactly 2 runes)
- Negative bounds allowed for Integer/Float: `Integer[-40, 50]`
- List length bounds use square brackets after the angle-bracket element type: `List<String>[1, 10]`

### Custom Data Type Aliases

```yammm
type Email = Pattern["^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$"]
type Percentage = Float[0.0, 100.0]
type PositiveInt = Integer[1, _]
type ShortCode = String[2, 5]
```

Aliases are declared with uppercase names and can chain (A = B = built-in). Cycles are rejected.

---

## Relationships

### Association (Independent Entities)

References between independently existing types. Uses `-->` syntax.

```yammm-snippet
--> REL_TYPE (multiplicity) TargetType
--> REL_TYPE (multiplicity) imported_schema.TargetType
```

### Composition (Owned Entities)

Embeds part-type children within their parent. Uses `*->` syntax.

```yammm-snippet
*-> REL_TYPE (multiplicity) PartType
```

Part types can only exist within compositions and cannot be instantiated directly.

### Multiplicity

| Syntax | Required | Cardinality |
| ------ | -------- | ----------- |
| (omitted) | No | One |
| `(_)` | No | One |
| `(_:one)` | No | One |
| `(_:many)` | No | Many (0 or more) |
| `(one)` | Yes | One |
| `(one:one)` | Yes | One |
| `(one:many)` | Yes | Many (1 or more) |
| `(many)` | No | Many (0 or more) |

### Edge Properties

Associations can carry their own properties:

```yammm-snippet
--> WORKS_AT (one) Company {
    start_date Date required
    title String
}
```

### Reverse Clause

Declares the inverse relationship name as metadata:

```yammm-snippet
--> OWNS (many) Asset / owned_by (one)
```

---

## Invariants

Business logic constraints evaluated after type checking. Syntax: `! "error_message" expression`.

```yammm
type Product {
    product_id String primary
    name       String[1, 100] required
    price      Float[0.0, _] required
    discount   Float[0.0, 100.0]

    ! "name_not_blank" name != ""
    ! "discount_reasonable" discount == nil || discount <= 50.0
}
```

### Collection Invariants (via Compositions)

```yammm
part type LineItem {
    quantity Integer[1, _] required
    unit_price Float[0.0, _] required
}

type Order {
    id String primary
    *-> ITEMS (many) LineItem

    ! "has_items" ITEMS -> Len > 0
    ! "all_positive_qty" ITEMS -> All |$item| { $item.quantity > 0 }
}
```

See `references/expressions.md` for the full expression language, operator precedence, pipeline syntax, lambda syntax, and all built-in functions.

---

## Imports

### Syntax

```yammm-snippet
import "./sibling_schema" as sibling       // Relative path
import "../parent/schema" as parent        // Relative path (up)
import "models/core/users" as users        // Module path (from module root)
```

### Rules

- Aliases must start with a letter (a-z, A-Z)
- Cannot use reserved keywords as aliases
- Circular imports are detected and rejected
- Paths are sandboxed and cannot escape the module root
- The `.yammm` extension is optional and will be appended if not present

### Usage with Relationships

```yammm-snippet
import "./departments" as dept

type Employee {
    id UUID primary
    name String[1, 100] required
    --> BELONGS_TO (one) dept.Department
}
```

Imported data type aliases must also be qualified: `common.Money`.

---

## Inheritance

### Abstract Types and extends

```yammm-snippet
abstract type Auditable {
    created_at Timestamp required
    updated_at Timestamp
}

abstract type Named {
    name String[1, 200] required
}

type Document extends Auditable, Named {
    content String required
}
```

Multiple inheritance is supported. Properties, associations, compositions, and invariants are inherited from parents. Invariants with the same name are deduplicated (child's version takes precedence).

### Constraint Narrowing

Child types can tighten (but never widen) inherited constraints:

```yammm
abstract type Entity {
    age Integer[0, 150]
}

type Adult extends Entity {
    adult_id String primary
    age Integer[18, 150]    // Valid: min increased (narrowed)
}
```

Widening a constraint (e.g., `Integer[0, 200]` when parent declares `Integer[0, 150]`) is rejected.

---

## Common Mistakes

1. **Missing `part` keyword on composition targets.** Types used with `*->` must be declared as `part type`, not plain `type`.

2. **Using `primary` and `required` together.** A `primary` field is implicitly required. Writing `field String primary required` is redundant.

3. **Forgetting nil guards on optional fields in invariants.** If `end_date` is optional, write `end_date == nil || end_date > start_date` to avoid evaluation errors on nil values.

4. **Confusing `_` across contexts.** In bounds, `_` means unbounded (`Integer[0, _]`). In multiplicity, `_` means optional (`(_:many)`). In expressions, `_` is a nil literal (equivalent to `nil`).

5. **Enum with fewer than two options.** `Enum` requires at least two string values.

6. **Referencing imported types without qualifier.** Must write `alias.TypeName`, not just `TypeName`, for imported types and data type aliases.

7. **Using reserved keywords as import aliases.** Keywords like `type`, `schema`, `import`, and built-in type names like `String`, `Integer`, `List` cannot be used as aliases.

8. **Using a disallowed type as a primary key.** Only `String`, `UUID`, `Date`, and `Timestamp` may be primary keys. Types like `Integer`, `Boolean`, `Enum`, `List`, and `Vector` are rejected.

9. **Using `List` or `Vector` in edge properties.** Relationship property blocks (`--> REL { ... }`) do not support `Vector` or `List` data types.
