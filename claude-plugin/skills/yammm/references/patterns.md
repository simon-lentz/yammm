# Common Schema Patterns

Reusable schema patterns for common modeling scenarios. All examples are generic and adaptable.

---

## Audit Field Patterns

Track creation and modification timestamps on entities.

```yammm
abstract type Auditable {
    created_at Timestamp required
    updated_at Timestamp
    created_by String
}

type Article extends Auditable {
    id UUID primary
    title String[1, 200] required
    body String required
}
```

For schemas that track external data retrieval:

```yammm-snippet
abstract type Trackable {
    created_at Timestamp required
    updated_at Timestamp
    fetched_at Timestamp required
}
```

---

## Soft Delete Pattern

```yammm
abstract type SoftDeletable {
    is_active Boolean required
    deactivated_at Timestamp
    deactivated_reason Enum["removed", "expired", "merged", "manual"]

    ! "deactivation_consistency" is_active || deactivated_at != nil
}

type Account extends SoftDeletable {
    id String primary
    name String[1, 100] required
}
```

---

## Normalization Pattern

```yammm
type Organization {
    id UUID primary
    name_raw String[1, 500] required
    name_norm String[1, 500] required

    ! "norm_not_empty" name_norm -> Len > 0
    ! "norm_is_lowercase" name_norm == name_norm -> Lower
}
```

---

## Identifier Patterns

### Single Primary Key

```yammm-snippet
type User {
    id UUID primary
    email String required
}
```

### External ID with Format Constraint

```yammm-snippet
type ExternalCode = String[6, 10]

type Product {
    sku ExternalCode primary
    name String[1, 200] required
}
```

### Composite Primary Key

A type may declare multiple `primary` fields; together they form the composite key:

```yammm-snippet
type Enrollment {
    student_id String primary
    course_id String primary
    enrolled_at Timestamp required
}
```

---

## Negative and Signed Bounds

Integer and Float support negative bounds for domains with signed ranges:

```yammm-snippet
temperature Integer[-40, 50]
latitude Float[-90.0, 90.0]
longitude Float[-180.0, 180.0]
altitude Integer[-500, 10000]
offset Integer[-12, 14]            // UTC offset in hours
```

---

## Multiplicity Forms

All supported multiplicity syntax:

```yammm-snippet
--> MANAGES (_) Department          // optional one (default)
--> MANAGES (_:one) Department      // optional one (explicit)
--> MANAGES (one) Department        // required one
--> MANAGES (one:one) Department    // required one (explicit)
--> HAS (_:many) Task               // optional many (0+)
--> HAS (many) Task                 // optional many (0+)
--> HAS (one:many) Task             // required many (1+)
```

---

## Pattern Matching in Invariants

### Regex Match Operators

`=~` and `!~` for inline regex validation in invariants:

```yammm-snippet
! "valid_email" email =~ /.+@.+\..+/
! "no_spaces" code !~ /\s/
! "starts_with_letter" name =~ /^[a-zA-Z]/
```

### `in` Membership Operator

Test membership in a list literal:

```yammm-snippet
! "valid_status" status in ["active", "inactive", "pending"]
! "allowed_region" region in ["US", "EU", "APAC"]
```

---

## Pattern Constraint Variations

### Single Pattern

```yammm-snippet
type Email = Pattern["^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$"]
```

### Two-Pattern Conjunction

Both patterns must match (AND semantics). Maximum 2 patterns.

```yammm-snippet
type AlphanumericCode = Pattern["^[A-Z]", "[0-9]$"]    // starts uppercase AND ends digit
```

### Timestamp with Custom Format

```yammm-snippet
log_time Timestamp["2006-01-02 15:04:05"]
compact_date Timestamp["20060102"]
```

---

## Enumeration Patterns

### Status Field

```yammm-snippet
type Task {
    id UUID primary
    title String[1, 200] required
    status Enum["draft", "open", "in_progress", "done", "cancelled"] required
}
```

### Category with Custom Alias

```yammm-snippet
type Severity = Enum["low", "medium", "high", "critical"]

type Incident {
    id UUID primary
    title String[1, 300] required
    severity Severity required
}
```

---

## List Patterns

### Tag / Multi-Value Fields

```yammm-snippet
type Article {
    id UUID primary
    title String[1, 200] required
    tags List<String[1, 50]>
    categories List<String[1, 100]>[1, 5]
}
```

### Bounded Numeric Lists

```yammm-snippet
type Survey {
    id UUID primary
    scores List<Integer[1, 10]>[1, _]

    ! "has_scores" scores -> Len > 0
    ! "average_above_5" scores -> Sum / (scores -> Len) >= 5.0
}
```

### List with Alias Element Type

```yammm-snippet
type EmailAddress = Pattern["^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$"]

type ContactCard {
    id UUID primary
    emails List<EmailAddress>[1, 5]
}
```

### List Invariants

```yammm-snippet
type Config {
    id String primary
    allowed_hosts List<String[1, 253]> required

    ! "has_hosts" allowed_hosts -> Len > 0
    ! "no_empty_hosts" allowed_hosts -> All |$h| { $h -> Len > 0 }
    ! "max_hosts" allowed_hosts -> Len <= 50
}
```

---

## Vector Pattern

Fixed-dimension numeric vectors for embeddings and coordinates:

```yammm-snippet
type Document {
    id UUID primary
    title String[1, 200] required
    embedding Vector[768]
}

type Location {
    id UUID primary
    coordinates Vector[3]
}
```

---

## Cross-Field Validation Invariants

### Date Range Validation

```yammm-snippet
! "end_after_start" end_date == nil || end_date > start_date
```

### Conditional Requirement

```yammm-snippet
! "card_requires_number" method != "card" || card_number != nil
! "wire_requires_ref" method != "wire" || wire_reference != nil
```

### Mutual Exclusion

```yammm-snippet
! "not_both" !(is_deprecated && is_experimental)
```

### Percentage Sum

```yammm-snippet
! "pct_total" equity_pct + bond_pct + cash_pct == 100.0
```

### Ternary Conditional

```yammm-snippet
! "discount_limit" is_premium ? { discount <= 50.0 : discount <= 20.0 }
```

---

## AllOrNone Pattern

Validate that a group of fields are either all present or all absent:

```yammm-snippet
! "address_complete" [street, city, postal_code] -> AllOrNone |$f| { $f != nil }
```

---

## Reduce Patterns

### Custom Aggregation

```yammm-snippet
! "total_weight" ITEMS -> Map |$i| { $i.weight } -> Reduce(0.0) |$acc, $w| { $acc + $w } <= 100.0
```

### Reduce Without Init (First Element as Seed)

```yammm-snippet
! "max_score" scores -> Reduce |$max, $s| { $max -> Max($s) } >= 50
```

---

## Default / Coalesce Patterns

Nil-safe value resolution:

```yammm-snippet
! "has_display_name" name -> Default(username) -> Len > 0
! "has_contact" email -> Coalesce(phone, address) != nil
```

---

## Collection Invariants via Compositions

### Order Items Validation

```yammm
part type LineItem {
    sku String[1, 50] required
    quantity Integer[1, _] required
    unit_price Float[0.0, _] required
}

type Order {
    id UUID primary
    *-> ITEMS (one:many) LineItem

    ! "has_items" ITEMS -> Len > 0
    ! "all_positive_qty" ITEMS -> All |$i| { $i.quantity > 0 }
    ! "max_line_items" ITEMS -> Len <= 100
}
```

### Nested Validation with Aggregation

```yammm
part type Ingredient {
    name String[1, 100] required
    percentage Float[0.0, 100.0] required
}

type Recipe {
    id UUID primary
    *-> INGREDIENTS (one:many) Ingredient

    ! "pct_total" INGREDIENTS -> Map |$i| { $i.percentage } -> Sum == 100.0
    ! "unique_names" INGREDIENTS -> Map |$i| { $i.name } -> Unique -> Len == INGREDIENTS -> Len
}
```

---

## Relationship Patterns

### One-to-One via Association

```yammm-snippet
--> BELONGS_TO (one) User
```

### One-to-Many via Association

```yammm-snippet
--> WRITTEN_BY (one) Author / BOOKS (many)
```

### Owned Children via Composition

```yammm-snippet
*-> PHONES (one:many) PhoneNumber
```

### Many-to-Many via Junction Type

```yammm
type Enrollment {
    id UUID primary
    enrolled_at Timestamp required
    --> STUDENT (one) Student
    --> COURSE (one) Course
}
```

### Edge Properties

```yammm-snippet
--> WORKS_AT (one) Company {
    start_date Date required
    title String[1, 100]
}

--> KNOWS (_:many) Person {
    weight Float[0.0, 1.0] required
}
```

---

## Import Patterns

### Relative Import

```yammm-snippet
import "./products" as products
--> PRODUCT (one) products.Product
```

### Module Import

```yammm-snippet
import "models/core/users" as users
--> CREATED_BY (one) users.User
```

### Cross-Schema Data Type Aliases

```yammm-snippet
import "./common" as common
amount common.Money required
```

### Extending Imported Types

```yammm-snippet
import "./base" as base
type Document extends base.Auditable {
    id UUID primary
    content String required
}
```
