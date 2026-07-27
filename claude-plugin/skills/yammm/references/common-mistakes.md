# Common Mistakes and Fixes

Wrong/right reference for frequent schema authoring errors. Each entry shows the broken pattern, why it fails, and the corrected form.

---

## 1. `primary required` Together

`primary` already implies required. The grammar treats them as mutually exclusive -- combining them is a **parse error**.

```yammm-invalid
// WRONG -- parse error: E_SYNTAX
id UUID primary required
```

```yammm-snippet
// RIGHT
id UUID primary
```

---

## 2. Missing `part` Keyword for Composition Targets

Types referenced by `*->` must be declared as `part type`. Using a plain `type` produces a semantic error.

```yammm-invalid
// WRONG -- Address is not a part type: E_INVALID_COMPOSITION_TARGET
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

Optional fields can be nil. An unguarded comparison against a nil field evaluates to false, tripping the invariant (`E_INVARIANT_FAIL`) on every instance where the optional field is absent.

```yammm-snippet
// WRONG -- loads clean; trips at validation time on every nil end_date
! "date_order" end_date > start_date
```

```yammm-snippet
// RIGHT -- guard with nil check
! "date_order" end_date == nil || end_date > start_date
```

---

## 4. Unqualified Imported Types

Imported types must be prefixed with their alias. Bare names resolve only within the current schema.

```yammm-invalid
// WRONG -- User is not in scope without prefix: E_UNKNOWN_TYPE
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

```yammm-invalid
// WRONG -- Integer cannot be a primary key: E_INVALID_PRIMARY_KEY_TYPE
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

```yammm-invalid
// WRONG -- only one option: E_INVALID_CONSTRAINT
status Enum["active"]
```

```yammm-snippet
// RIGHT -- at least two
status Enum["active", "inactive"]
```

---

## 8. Missing Second Bound in Brackets

When brackets are present, both bounds are required. Use `_` for unbounded sides.

```yammm-invalid
// WRONG -- single bound: E_SYNTAX
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

```yammm-invalid
// WRONG -- association to a part type: E_INVALID_ASSOCIATION_TARGET
part type LineItem {
    sku String[1, 40] primary
}

type Order {
    id UUID primary
    --> HAS_ITEM (many) LineItem
}
```

```yammm-snippet
// RIGHT -- composition
part type LineItem {
    sku String[1, 40] primary
}

type Order {
    id UUID primary
    *-> ITEMS (one:many) LineItem
}
```

---

## 10. Unkeyed Part Type in a `(many)` Composition

Part types are **exempt** from the primary-key requirement — they are identified
through their parent composition, so they may omit one. They are not *barred*
from declaring one, and in a `(many)` composition declaring one is usually the
better choice.

A composed child's identity is `[ParentKey, "COMPOSITION", ChildKeyOrIndex]`.
Without a primary key the child slot is its **0-based position in the array**, so
reordering the children — or inserting one — changes the identity of every child
after that point. With a primary key the child slot is that key, and identity
survives reordering.

```yammm-snippet
// RISKY -- identity is positional; reordering renumbers every later child
part type LineItem {
    sku String[1, 40] required
    qty Integer[1, _] required
}
```

```yammm-snippet
// BETTER -- stable identity independent of array order
part type LineItem {
    sku String[1, 40] primary
    qty Integer[1, _] required
}
```

A `(one)` composition has a single child and no index, so a key adds nothing
there; omitting it is fine.

---

## 11. Part Type Declaring an Association

Part types cannot have independent relationships. Only the parent type holds associations.

```yammm-invalid
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

## 12. Concrete Type Without a Primary Key (Declared or Inherited)

Every concrete type must declare or inherit at least one `primary` field (`E_NO_PRIMARY_KEY`). Abstract types MAY declare a primary field — concrete subtypes inherit it — but are not required to; when no ancestor declares one, the concrete type must.

```yammm-invalid
// WRONG -- Document neither declares nor inherits a primary key: E_NO_PRIMARY_KEY
abstract type Auditable {
    created_at Timestamp required
}

type Document extends Auditable {
    content String required
}
```

```yammm-snippet
// RIGHT -- declare on the concrete type...
type Document extends Auditable {
    id UUID primary
    content String required
}

// ...or declare on an abstract ancestor (inherited by all subtypes)
abstract type Identified {
    id UUID primary
}
```

---

## 13. Colons in Field Declarations

Yammm uses space separation, not colons. This is a common mistake when coming from JSON Schema or TypeScript.

```yammm-invalid
// WRONG -- not JSON syntax: E_SYNTAX
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

All built-in functions in invariants are capitalized. A lowercase name is not a
built-in, so the schema still **loads clean** — the failure surfaces at
validation time (`E_UNKNOWN_BUILTIN` / `E_EVAL_ERROR`), not at load. Nothing
catches this for you at compile time.

```yammm-snippet
// WRONG -- loads clean; fails at validation time
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

```yammm-invalid
// WRONG -- List not allowed in edge properties: E_LIST_ON_EDGE
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

```yammm-invalid
// WRONG -- widens parent bounds: E_PROPERTY_CONFLICT
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

```yammm-invalid
// WRONG -- brackets: E_SYNTAX
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

## 19. Reusing an Invariant Message

An invariant's string is its **message** and doubles as its **name**. Reusing one
is not an error — the schema loads clean and both invariants evaluate — but the
name is load-bearing in two ways, so a duplicate costs you something.

Within a type, two invariants sharing a message produce two failures reporting
the same text, and nothing in the diagnostic says which expression tripped.

Across inheritance it matters more: invariants merge by name, keep-first, so a
subtype declaring a parent's message **overrides** the parent's invariant rather
than adding to it. That is the intended way to refine an inherited rule — and a
silent surprise if the collision was accidental.

```yammm-snippet
// RISKY -- both evaluate; both failures read "check"
! "check" name -> Len > 0
! "check" email -> Len > 0
```

```yammm-snippet
// BETTER -- each failure names the rule that tripped
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

---

## 21. Single `@` for a Composite Index

`@index` is a property-level annotation that takes no arguments. A composite (multi-property) index is a type-level `@@index(...)` member. Writing `@index(a, b)` on a property is an error (`E_INVALID_ANNOTATION`).

```yammm-invalid
// WRONG -- @index takes no arguments: E_INVALID_ANNOTATION
type Document {
    content_hash String primary
    state        String @index(state, published_on)
}
```

```yammm-snippet
// RIGHT -- a single-property index trails the property; a composite is a @@ member
type Document {
    content_hash String primary
    state        String @index
    published_on Date

    @@index(state, published_on)
}
```
