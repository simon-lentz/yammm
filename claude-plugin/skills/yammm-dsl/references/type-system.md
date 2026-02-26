# Yammm Type System Reference

Detailed reference for the yammm type system: built-in constraint types, custom aliases, abstract and part types, and inheritance mechanics.

---

## Built-in Constraint Types

### Integer

Represents signed integer values with optional bounds.

**Syntax:** `Integer` or `Integer[min, max]`

- Bounds are inclusive
- `_` means unbounded on that side
- Negative bounds are allowed with a leading `-`
- Validation accepts signed and unsigned integers; unsigned inputs larger than `int64` are rejected before bound checks

```yammm
age Integer                     // Unbounded integer
count Integer[0, _]             // Non-negative (0 or greater)
priority Integer[1, 10]         // 1 through 10 inclusive
temperature Integer[-40, 50]    // Negative lower bound
index Integer[_, 99]            // No minimum, max 99
```

### Float

Represents floating-point values with optional bounds.

**Syntax:** `Float` or `Float[min, max]`

- Bounds are inclusive
- Both integer and float literals are valid as bounds
- Negative bounds allowed

```yammm
temperature Float               // Unbounded float
percentage Float[0.0, 100.0]    // 0 to 100 inclusive
ratio Float[0, 1.0]             // 0 to 1 (integer bound is valid)
latitude Float[-90.0, 90.0]     // Geographic latitude
```

### Boolean

Represents true/false values. No parameters.

**Syntax:** `Boolean`

```yammm
active Boolean
is_published Boolean required
```

### String

Represents UTF-8 string values with optional length bounds counted in runes (not bytes).

**Syntax:** `String` or `String[minLen, maxLen]`

- Bounds count Unicode runes
- `_` means unbounded

```yammm
name String                     // Unbounded string
name String[1, 100]             // 1 to 100 runes
code String[3, 3]               // Exactly 3 runes
notes String[_, 1000]           // Maximum 1000 runes
```

### Enum

Represents a value from a fixed set of string options.

**Syntax:** `Enum["option1", "option2", ...]`

- At least two options are required
- Trailing comma is allowed

```yammm
status Enum["pending", "approved", "rejected"]
priority Enum["low", "medium", "high", "critical"]
color Enum["red", "green", "blue",]     // Trailing comma OK
```

### Pattern

Represents a string that must match one or more regular expressions.

**Syntax:** `Pattern["regex"]` or `Pattern["regex1", "regex2"]`

- Follows Go `regexp` package syntax
- When two patterns are provided, the value must match both

```yammm
email Pattern["^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$"]
phone Pattern["^\\d{3}-\\d{3}-\\d{4}$"]
```

### Timestamp

Represents a date-time value with optional format specification.

**Syntax:** `Timestamp` or `Timestamp["format"]`

- Default format is RFC3339: `"2006-01-02T15:04:05Z07:00"`
- Format string follows Go time formatting conventions

```yammm
created_at Timestamp                                    // RFC3339
event_time Timestamp["2006-01-02T15:04:05Z07:00"]       // Explicit RFC3339
log_time Timestamp["2006-01-02 15:04:05"]               // Custom format
```

### Date

Represents a date value without a time component. No parameters.

**Syntax:** `Date`

```yammm
birth_date Date
expiry_date Date required
```

### UUID

Represents a universally unique identifier string. No parameters.

**Syntax:** `UUID`

```yammm
id UUID primary
external_ref UUID
correlation_id UUID required
```

### Vector

Represents a fixed-dimension numeric vector for embeddings, coordinates, etc.

**Syntax:** `Vector[dimensions]`

- Dimensions must be a positive integer
- Validation accepts float slices/arrays (`[]float32`/`[]float64`)
- NaN, Inf, and non-float elements are rejected
- Cannot be used in relationship properties

```yammm
embedding Vector[768]           // 768-dimensional embedding
coordinates Vector[3]           // 3D coordinates
```

---

## Bound Syntax Rules

When specifying bounds on `Integer`, `Float`, or `String`:

1. **Both bounds required when brackets present.** You cannot write `Integer[5]`; it must be `Integer[5, _]` or `Integer[_, 5]`.

2. **`_` means unbounded.** Use on either side: `Integer[0, _]` (no upper bound), `Float[_, 100.0]` (no lower bound).

3. **Exact value.** Set both bounds equal: `String[5, 5]` (exactly 5 runes).

4. **Integer bounds for Float.** Integer literals are valid float bounds: `Float[0, 1.0]`.

5. **Negative bounds.** Supported for Integer and Float: `Integer[-100, 100]`, `Float[-1.0, 1.0]`.

---

## Custom Data Type Aliases

Aliases create named references to built-in constraint types. They simplify schemas and enforce consistent constraints across fields.

**Syntax:** `type AliasName = BuiltInType`

```yammm
type Email = Pattern["^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$"]
type Percentage = Float[0.0, 100.0]
type PositiveInt = Integer[1, _]
type ShortCode = String[2, 10]
type Priority = Enum["low", "medium", "high", "critical"]
```

### Alias Rules

- Must be declared with an uppercase identifier
- Stored internally as lowercase
- Can chain: `type A = Integer[0, _]` then use `A` in properties
- Alias cycles are detected and rejected during parsing
- Imported aliases must be qualified: `common.Money`

### Usage

```yammm
type Money = Float[0.0, _]

type Product {
    id UUID primary
    name String[1, 200] required
    price Money required
    discount Percentage
}
```

---

## Abstract Types

Abstract types define shared structure that cannot be instantiated directly. Other types must extend them to inherit their properties and relationships.

**Syntax:** `abstract type TypeName { ... }`

```yammm
abstract type Auditable {
    created_at Timestamp required
    updated_at Timestamp
    created_by String
}

abstract type Named {
    name String[1, 200] required
}
```

### Rules

- Cannot create instances of abstract types
- Can define properties, associations, compositions, and invariants
- Can extend other abstract types
- Multiple types can extend the same abstract type

---

## Part Types

Part types represent entities that are owned by and embedded within a parent type via composition (`*->`). They cannot exist independently.

**Syntax:** `part type TypeName { ... }`

```yammm
part type Address {
    street String[1, 200] required
    city String[1, 100] required
    postal_code String[3, 20] required
}

type Customer {
    id UUID primary
    name String[1, 100] required
    *-> ADDRESSES (one:many) Address
}
```

### Rules

- Part types can only be targets of composition relationships (`*->`)
- Cannot be instantiated directly at the top level
- Composition data is embedded inline in instance documents (not via reference)
- The composition target must be a concrete `part` type (not abstract)

---

## Type Modifiers Summary

| Modifier | Keyword | Instantiable | As Association Target | As Composition Target |
|----------|---------|--------------|----------------------|----------------------|
| Concrete | `type` | Yes | Yes | No |
| Abstract | `abstract type` | No | Yes (via subtypes) | No |
| Part | `part type` | No (standalone) | No | Yes |

---

## Inheritance Mechanics

### The extends Keyword

Types inherit properties, associations, and compositions from parent types using `extends`.

**Syntax:** `type ChildType extends ParentType1, ParentType2 { ... }`

```yammm
abstract type Auditable {
    created_at Timestamp required
    updated_at Timestamp
}

abstract type Named {
    name String[1, 200] required
}

type Document extends Auditable, Named {
    id UUID primary
    content String required
}
```

Multiple inheritance is supported: a type can extend multiple parent types, separated by commas.

### What Is Inherited

- All properties from parent types
- All association relationships from parent types
- All composition relationships from parent types
- Invariants are NOT inherited (they belong to the declaring type)

### Constraint Narrowing Rules

A child type may override an inherited property by re-declaring it with a **narrower** (more restrictive) constraint. Widening is never allowed.

**Valid narrowing** (tightening constraints):

```yammm
abstract type Base {
    age Integer[0, 150]
    name String[1, 100]
    score Float[0.0, 100.0]
}

type Restricted extends Base {
    age Integer[18, 65]         // Narrowed: min raised, max lowered
    name String[1, 50]          // Narrowed: max lowered
    score Float[50.0, 100.0]    // Narrowed: min raised
}
```

**Invalid widening** (rejected at load time):

```yammm
abstract type Base {
    age Integer[0, 150]
    name String[1, 100]
}

// ERROR: These would widen the parent constraints
type Invalid extends Base {
    age Integer[0, 200]         // INVALID: max increased (wider)
    name String[0, 100]         // INVALID: min decreased (wider)
}
```

### Narrowing Direction

| Change | Direction | Allowed |
|--------|-----------|---------|
| Raise minimum bound | Narrowing | Yes |
| Lower maximum bound | Narrowing | Yes |
| Lower minimum bound | Widening | No |
| Raise maximum bound | Widening | No |
| Add bounds to unbounded parent | Narrowing | Yes |
| Remove bounds from bounded parent | Widening | No |

### Property Modifier Override

A child type can also promote an optional property to required:

```yammm
abstract type Base {
    description String          // Optional
}

type Strict extends Base {
    description String required // Now required (narrowing)
}
```

Making a required field optional would be widening and is not allowed.

### Relationship Uniqueness

After inheritance, relationship definitions must be unique by name and target pair. Duplicate name/target pairs from multiple parents are reported as errors.

### Extending Imported Types

Types from imported schemas can be extended using qualified references:

```yammm
import "./base_types" as base

type SpecialDocument extends base.Document {
    classification Enum["public", "internal", "confidential"] required
}
```
