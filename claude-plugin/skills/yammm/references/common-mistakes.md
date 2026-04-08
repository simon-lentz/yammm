# Common Mistakes and Fixes

Wrong/right reference for frequent schema authoring errors. Each entry shows the broken pattern, why it fails, and the corrected form.

---

## 1. `primary required` Together

`primary` already implies required. The grammar treats them as mutually exclusive -- combining them is a **parse error**.

```yammm-snippet
// WRONG -- parse error
id UUID primary required
```

```yammm-snippet
// RIGHT
id UUID primary
```

---

## 2. Missing `part` Keyword for Composition Targets

Types referenced by `*->` must be declared as `part type`. Using a plain `type` produces a semantic error.

```yammm-snippet
// WRONG -- Address is not a part type
type Address {
    street String required
}

type Person {
    id UUID primary
    *-> HOME_ADDRESS (one) Address
}
```

```yammm-snippet
// RIGHT
part type Address {
    street String[1, 200] required
    city String[1, 100] required
}

type Person {
    id UUID primary
    *-> HOME_ADDRESS (one) Address
}
```

---

## 3. Missing Nil Guard in Invariants

Optional fields can be nil. Invariants that compare optional fields without a nil check fail at evaluation time.

```yammm-snippet
// WRONG -- fails when end_date is nil
! "date_order" end_date > start_date
```

```yammm-snippet
// RIGHT -- guard with nil check
! "date_order" end_date == nil || end_date > start_date
```

---

## 4. Unqualified Imported Types

Imported types must be prefixed with their alias. Bare names resolve only within the current schema.

```yammm-snippet
// WRONG -- User is not in scope without prefix
import "./users" as users
--> CREATED_BY (one) User
```

```yammm-snippet
// RIGHT
import "./users" as users
--> CREATED_BY (one) users.User
```

---

## 5. Disallowed Primary Key Type

Only `String`, `UUID`, `Date`, and `Timestamp` are valid primary key types. `Integer`, `Float`, `Boolean`, `Enum`, `Pattern`, `Vector`, and `List` are rejected.

```yammm-snippet
// WRONG -- Integer cannot be a primary key
type Record {
    id Integer primary
}
```

```yammm-snippet
// RIGHT
type Record {
    id UUID primary
}
```

---

## 6. Bare `String` Without Bounds

Plain `String` accepts any length. Fields with a known domain should use bounds for data integrity.

```yammm-snippet
// WRONG -- no length constraint
type User {
    id UUID primary
    name String required
    email String required
}
```

```yammm-snippet
// RIGHT -- bounded to domain limits
type User {
    id UUID primary
    name String[1, 100] required
    email String[1, 320] required
}
```

---

## 7. Single-Option Enum

Enums require at least two options. A single-option Enum is a parse error.

```yammm-snippet
// WRONG -- only one option
status Enum["active"]
```

```yammm-snippet
// RIGHT -- at least two
status Enum["active", "inactive"]
```

---

## 8. Missing Second Bound in Brackets

When brackets are present, both bounds are required. Use `_` for unbounded sides.

```yammm-snippet
// WRONG -- single bound
name String[255]
count Integer[0]
```

```yammm-snippet
// RIGHT -- both bounds present
name String[1, 255]
count Integer[0, _]
```

---

## 9. Association Targeting a Part Type

Associations (`-->`) connect independently existing types. Part types can only be referenced via compositions (`*->`).

```yammm-snippet
// WRONG -- association to a part type
part type LineItem { ... }

type Order {
    id UUID primary
    --> HAS_ITEM (many) LineItem
}
```

```yammm-snippet
// RIGHT -- composition
part type LineItem { ... }

type Order {
    id UUID primary
    *-> ITEMS (one:many) LineItem
}
```

---

## 10. Part Type Declaring a Primary Field

Part types are identified by their parent composition. They cannot have primary keys.

```yammm-snippet
// WRONG
part type Address {
    id UUID primary
    street String[1, 200] required
}
```

```yammm-snippet
// RIGHT
part type Address {
    street String[1, 200] required
    city String[1, 100] required
}
```

---

## 11. Part Type Declaring an Association

Part types cannot have independent relationships. Only the parent type holds associations.

```yammm-snippet
// WRONG
part type LineItem {
    quantity Integer[1, _] required
    --> PRODUCT (one) Product
}
```

```yammm-snippet
// RIGHT -- move association to the parent
part type LineItem {
    sku String[1, 50] required
    quantity Integer[1, _] required
}

type Order {
    id UUID primary
    *-> ITEMS (one:many) LineItem
}
```

---

## 12. Abstract Type Declaring a Primary Field

Abstract types are not instantiable. The concrete child type supplies the primary key.

```yammm-snippet
// WRONG
abstract type Auditable {
    id UUID primary
    created_at Timestamp required
}
```

```yammm-snippet
// RIGHT
abstract type Auditable {
    created_at Timestamp required
    updated_at Timestamp
}

type Document extends Auditable {
    id UUID primary
    content String required
}
```

---

## 13. Colons in Field Declarations

Yammm uses space separation, not colons. This is a common mistake when coming from JSON Schema or TypeScript.

```yammm-snippet
// WRONG -- not JSON syntax
name: String required
age: Integer
```

```yammm-snippet
// RIGHT -- space-separated
name String required
age Integer
```

---

## 14. Lowercase Built-in Function Names

All built-in functions in invariants are capitalized. Lowercase names are resolved as property references.

```yammm-snippet
// WRONG -- lowercase function names
! "check_len" name -> len > 0
! "all_valid" ITEMS -> all |$i| { $i.qty > 0 }
! "has_name" name -> contains("test")
```

```yammm-snippet
// RIGHT -- capitalized
! "check_len" name -> Len > 0
! "all_valid" ITEMS -> All |$i| { $i.qty > 0 }
! "has_name" name -> Contains("test")
```

---

## 15. Vector or List in Edge Properties

Relationship property blocks cannot use `Vector` or `List` types.

```yammm-snippet
// WRONG -- List not allowed in edge properties
--> RATED (many) Product {
    score Float[0.0, 5.0] required
    tags List<String>
}
```

```yammm-snippet
// RIGHT -- remove disallowed types from edge
--> RATED (many) Product {
    score Float[0.0, 5.0] required
    tag String[1, 50]
}
```

---

## 16. Widening Constraint in Child Type

A child type can narrow inherited constraints but never widen them.

```yammm-snippet
// WRONG -- widens parent bounds
abstract type Base {
    age Integer[18, 65]
}

type Permissive extends Base {
    age Integer[0, 150]
}
```

```yammm-snippet
// RIGHT -- narrows parent bounds
abstract type Base {
    age Integer[0, 150]
}

type Restricted extends Base {
    age Integer[18, 65]
}
```

---

## 17. Brackets Instead of Parentheses for Multiplicity

Multiplicity uses parentheses. Square brackets are for type bounds.

```yammm-snippet
// WRONG -- brackets
--> MANAGES [many] Department
--> BELONGS_TO [one] Team
```

```yammm-snippet
// RIGHT -- parentheses
--> MANAGES (many) Department
--> BELONGS_TO (one) Team
```

---

## 18. Missing Schema Declaration

Every `.yammm` file must start with a `schema` declaration.

```yammm-snippet
// WRONG -- no schema declaration
type User {
    id UUID primary
    name String[1, 100] required
}
```

```yammm-snippet
// RIGHT
schema "users"

type User {
    id UUID primary
    name String[1, 100] required
}
```

---

## 19. Duplicate Invariant Error IDs

Invariant error IDs must be unique within a type. Duplicates cause a semantic error.

```yammm-snippet
// WRONG -- "check" used twice
! "check" name -> Len > 0
! "check" email -> Len > 0
```

```yammm-snippet
// RIGHT -- unique IDs
! "name_not_empty" name -> Len > 0
! "email_not_empty" email -> Len > 0
```

---

## 20. Unbounded List Without Length Constraint

`List<T>` with no bounds and no `Len` invariant accepts arbitrarily large payloads.

```yammm-snippet
// RISKY -- unbounded
tags List<String>
```

```yammm-snippet
// BETTER -- bounded, or add an invariant
tags List<String[1, 50]>[0, 20]

// OR
tags List<String[1, 50]>
! "tag_limit" tags -> Len <= 100
```
