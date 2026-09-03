# Type System Reference

Detailed reference for the yammm type system: built-in constraint types, custom aliases, abstract and part types, and inheritance mechanics.

---

## Built-in Constraint Types

### Integer

Signed integer values with optional bounds.

**Syntax:** `Integer` or `Integer[min, max]`

- Bounds are inclusive
- `_` means unbounded on that side
- Negative bounds allowed: `Integer[-40, 50]`
- Validation accepts signed and unsigned integers. Unsigned values exceeding `int64` max are rejected.

```yammm-snippet
age Integer                     // unbounded
count Integer[0, _]             // non-negative
priority Integer[1, 10]         // 1 through 10
temperature Integer[-40, 50]    // negative lower bound
```

### Float

Floating-point values with optional bounds.

**Syntax:** `Float` or `Float[min, max]`

- Bounds are inclusive
- Integer literals valid as bounds: `Float[0, 1.0]`
- Negative bounds allowed: `Float[-90.0, 90.0]`

```yammm-snippet
percentage Float[0.0, 100.0]
ratio Float[0, 1.0]
latitude Float[-90.0, 90.0]
```

### Boolean

True/false values. No parameters.

**Syntax:** `Boolean`

### String

UTF-8 string values with optional length bounds counted in runes (not bytes).

**Syntax:** `String` or `String[minLen, maxLen]`

```yammm-snippet
name String[1, 100]        // 1 to 100 runes
code String[3, 3]           // exactly 3 runes
notes String[_, 1000]       // max 1000 runes
```

### Enum

Value from a fixed set of string options.

**Syntax:** `Enum["option1", "option2", ...]`

- At least two options required
- Trailing comma allowed
- Enum narrowing in inheritance: child values must be a subset of parent values

```yammm-snippet
status Enum["pending", "approved", "rejected"]
priority Enum["low", "medium", "high", "critical"]
```

### Pattern

String validated against one or two regular expressions.

**Syntax:** `Pattern["regex"]` or `Pattern["regex1", "regex2"]`

- Follows Go `regexp` package syntax
- Maximum of 2 patterns (performance cap)
- When two patterns are provided, the value must match both (conjunction semantics)

```yammm-snippet
email Pattern["^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$"]
code Pattern["^[A-Z]", "[0-9]$"]    // must start with uppercase AND end with digit
```

### Timestamp

Date-time value with optional format specification.

**Syntax:** `Timestamp` or `Timestamp["format"]`

- Default format is RFC3339: `"2006-01-02T15:04:05Z07:00"`
- Format string follows Go time formatting conventions
- Accepts a string or a `time.Time`; **stores the string**, rendered through the declared format when there is one and through RFC 3339 with nanoseconds otherwise
- A declared format that cannot represent an instant (no zone, no fraction, or neither) loses what it omits and draws `W_TIMESTAMP_LOSSY_FORMAT` at schema load

```yammm-snippet
created_at Timestamp
log_time Timestamp["2006-01-02 15:04:05"]
```

### Date

Date value without a time component. No parameters.

**Syntax:** `Date`

- Accepts a `"YYYY-MM-DD"` string or a `time.Time`; **stores the string**. A `time.Time` truncates to its calendar day **in its own location**, so 00:30 `+02:00` is the 19th and the same instant at 22:30 `Z` is the 18th

### UUID

Universally unique identifier. No parameters.

**Syntax:** `UUID`

- Accepts a string or a `uuid.UUID`; **stores the canonical lowercase hyphenated text**, so uppercase, brace-wrapped, `urn:uuid:` and bare-hex spellings all reach one value and one primary key

### Vector

Fixed-dimension numeric vector for embeddings, coordinates, etc.

**Syntax:** `Vector[dimensions]`

- Dimensions must be a positive integer
- Accepts float slices/arrays (`[]float32`/`[]float64`)
- NaN, Inf, and non-float elements rejected
- Cannot be a primary key
- Cannot be used in relationship properties

```yammm-snippet
embedding Vector[768]
coordinates Vector[3]
```

### List

Ordered collection with specified element type and optional length bounds.

**Syntax:** `List<ElementType>` or `List<ElementType>[min, max]`

- Element type: any built-in type, `DataType` alias, `List` (nesting), or `Vector`
- Length bounds in square brackets after angle brackets
- `_` means unbounded: `List<String>[1, _]`
- Cannot be a primary key
- Cannot be used in relationship (edge) properties

```yammm-snippet
tags List<String>
scores List<Integer[0, 100]>
top_five List<Float>[5, 5]
matrix List<List<Float>>
embeddings List<Vector[768]>[_, 10]
```

**Neo4j export caveat.** The last two — a list whose *element* is itself a collection — are valid yammm but have no Neo4j equivalent, and constraint generation is all-or-nothing, so one such property makes `yammm neo4j constraints` emit **nothing for the entire schema** (`E_NEO4J_UNSUPPORTED_TYPE`). If the model must export to Neo4j, represent the inner collection as a `part type` reached by a composition. A bare `Vector[N]` is fine; only a `Vector` nested inside a `List` is not.

---

## Primary Key Types

| Allowed | Types |
| ------- | ----- |
| Yes | `String`, `UUID`, `Date`, `Timestamp` |
| No | `Integer`, `Float`, `Boolean`, `Enum`, `Pattern`, `Vector`, `List` |

Alias resolution applies: `type VIN = String[17, 17]` is allowed as a primary key because it resolves to `String`.

---

## Bound Syntax Rules

1. **Both bounds required** when brackets present: `Integer[5, _]`, not `Integer[5]`.
2. **`_` means unbounded**: `Integer[0, _]`, `Float[_, 100.0]`.
3. **Exact value**: `String[5, 5]`.
4. **Integer bounds for Float**: `Float[0, 1.0]`.
5. **Negative bounds**: `Integer[-100, 100]`, `Float[-1.0, 1.0]`.
6. **List length bounds**: `List<String>[1, 10]` (counts elements).

---

## Custom Data Type Aliases

Named references to built-in constraint types for consistency and reuse.

**Syntax:** `type AliasName = BuiltInType`

```yammm-snippet
type Email = Pattern["^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$"]
type Percentage = Float[0.0, 100.0]
type PositiveInt = Integer[1, _]
type Tags = List<String[1, 50]>
```

### Rules

- Must be declared with an uppercase identifier
- Aliases preserve the declared case (not lowercased internally)
- Can chain: alias A wraps a built-in, alias B wraps A
- Alias cycles are detected and rejected during schema completion (not during parsing)
- Imported aliases require qualification: `common.Money`

---

## Abstract Types

Define shared structure that cannot be instantiated directly.

```yammm-snippet
abstract type Auditable {
    created_at Timestamp required
    updated_at Timestamp
}
```

### Rules

- Cannot create instances of abstract types
- Can define properties, associations, compositions, and invariants
- Can extend other abstract types
- Multiple types can extend the same abstract type
- An abstract type may declare a `primary` field (inherited by its concrete subtypes), but is not required to
- Cannot be the target of an association (`-->`) -- associations must reference a concrete type

---

## Part Types

Entities owned by and embedded within a parent via composition (`*->`).

```yammm-snippet
part type Address {
    street String[1, 200] required
    city String[1, 100] required
}
```

### Rules

- Only referenced as targets of composition relationships (`*->`)
- Cannot be instantiated directly at the top level
- Exempt from the primary-key requirement, but permitted to declare one. A composed child's identity is `[ParentKey, "COMPOSITION", ChildKeyOrIndex]`; without a key the slot is the child's 0-based array position, so prefer a `primary` in a `(many)` composition to keep identity stable across reordering
- Cannot declare associations (`-->`) -- part types cannot have independent references
- Associations (`-->`) from other types cannot target part types
- Composition data is embedded inline in instance documents

---

## Type Modifiers Summary

| Modifier | Keyword | Instantiable | Association Target | Composition Target |
| -------- | ------- | ------------ | ----------------- | ----------------- |
| Concrete | `type` | Yes | Yes | No |
| Abstract | `abstract type` | No | No | No |
| Part | `part type` | No (standalone) | No | Yes |

---

## Inheritance Mechanics

### The `extends` Keyword

```yammm-snippet
type Document extends Auditable, Named {
    id UUID primary
    content String required
}
```

Multiple inheritance is supported (comma-separated parents). The target of `extends` must be an `abstract type`.

### What Is Inherited

- All properties from parent types
- All association relationships
- All composition relationships
- Invariants (deduplicated by name -- child version takes precedence)

### Constraint Narrowing Rules

A child type may override an inherited property by re-declaring it with a **narrower** (more restrictive) constraint. Widening is rejected at load time.

**Valid narrowing:**

```yammm-snippet
abstract type Base {
    age Integer[0, 150]
    name String[1, 100]
}

type Restricted extends Base {
    age Integer[18, 65]         // min raised, max lowered
    name String[1, 50]          // max lowered
}
```

| Change | Direction | Allowed |
| ------ | --------- | ------- |
| Raise minimum bound | Narrowing | Yes |
| Lower maximum bound | Narrowing | Yes |
| Lower minimum bound | Widening | No |
| Raise maximum bound | Widening | No |
| Add bounds to unbounded parent | Narrowing | Yes |
| Remove bounds from bounded parent | Widening | No |

### Enum Narrowing

Child Enum values must be a subset of parent Enum values:

```yammm-snippet
abstract type Base {
    priority Enum["low", "medium", "high", "critical"]
}

type Restricted extends Base {
    priority Enum["medium", "high"]    // valid: subset of parent
}
```

### List Narrowing

- Raise minimum length or lower maximum length
- Narrow the element type constraint (e.g., `List<Integer[0, 100]>` narrows `List<Integer>`)

### Property Modifier Override

A child can promote an optional property to required:

```yammm-snippet
abstract type Base {
    description String          // optional
}

type Strict extends Base {
    description String required // now required (narrowing)
}
```

Making a required field optional is widening and not allowed.

### Relationship Uniqueness

After inheritance, one relation name carries one definition. Two own relations under one name draw `E_DUPLICATE_RELATION`; inherited definitions that differ, or an association and a composition sharing a name, draw `E_RELATION_COLLISION`, reported once on the type that combines them and naming every rival. Identical definitions reached through two ancestors merge silently.

### Extending Imported Types

```yammm-snippet
import "./base_types" as base

type SpecialDocument extends base.Document {
    classification Enum["public", "internal", "confidential"] required
}
```
