# Yammm Common Schema Patterns

Reusable schema patterns for common modeling scenarios. All examples are generic and can be adapted to any domain.

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

For schemas that track when external data was retrieved:

```yammm
abstract type Trackable {
    created_at Timestamp required
    updated_at Timestamp
    fetched_at Timestamp required
}
```

---

## Soft Delete Pattern

Mark entities as inactive rather than physically removing them.

```yammm
abstract type SoftDeletable {
    is_active Boolean required
    deactivated_at Timestamp
    deactivated_reason Enum["removed", "expired", "merged", "manual"]

    ! "deactivation_consistency" is_active || deactivated_at != nil
}
```

The invariant ensures that inactive records always have a deactivation timestamp.

```yammm
type Account extends SoftDeletable {
    id String primary
    name String[1, 100] required
}
```

---

## Normalization Pattern

Store both raw and normalized forms of a value, with an invariant ensuring the normalized form is populated.

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

```yammm
type User {
    id UUID primary
    email String required
}
```

### External ID with Format Constraint

```yammm
type ExternalCode = String[6, 10]

type Product {
    sku ExternalCode primary
    name String[1, 200] required
}
```

### Composite Key via Invariant

Yammm supports a single `primary` field per type. For composite uniqueness, use multiple required fields and document the composite key intent:

```yammm
type Enrollment {
    id UUID primary
    student_id String required
    course_id String required

    ! "composite_populated" student_id -> Len > 0 && course_id -> Len > 0
}
```

---

## Enumeration Patterns

### Status Field

```yammm
type Task {
    id UUID primary
    title String[1, 200] required
    status Enum["draft", "open", "in_progress", "done", "cancelled"] required
}
```

### Category with Custom Alias

```yammm
type Severity = Enum["low", "medium", "high", "critical"]

type Incident {
    id UUID primary
    title String[1, 300] required
    severity Severity required
    resolved Boolean required
}
```

---

## Cross-Field Validation Invariants

### Date Range Validation

```yammm
type Event {
    id UUID primary
    name String[1, 200] required
    start_date Date required
    end_date Date

    ! "end_after_start" end_date == nil || end_date > start_date
}
```

### Conditional Requirement

Require one field to be present when another has a specific value:

```yammm
type Payment {
    id UUID primary
    method Enum["card", "wire", "crypto"] required
    card_number String
    wire_reference String

    ! "card_requires_number" method != "card" || card_number != nil
    ! "wire_requires_ref" method != "wire" || wire_reference != nil
}
```

### Mutual Exclusion

Ensure two boolean flags are never both true:

```yammm
type Feature {
    id String primary
    is_deprecated Boolean required
    is_experimental Boolean required

    ! "not_both" !(is_deprecated && is_experimental)
}
```

### Percentage Sum

Validate that percentages across fields total correctly:

```yammm
type Allocation {
    id UUID primary
    equity_pct Float[0.0, 100.0] required
    bond_pct Float[0.0, 100.0] required
    cash_pct Float[0.0, 100.0] required

    ! "pct_total" equity_pct + bond_pct + cash_pct == 100.0
}
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
    customer_id String required

    *-> ITEMS (one:many) LineItem

    ! "has_items" ITEMS -> Len > 0
    ! "all_positive_qty" ITEMS -> All |$i| { $i.quantity > 0 }
    ! "all_priced" ITEMS -> All |$i| { $i.unit_price > 0.0 }
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
    name String[1, 200] required

    *-> INGREDIENTS (one:many) Ingredient

    ! "has_ingredients" INGREDIENTS -> Len > 0
    ! "pct_total" INGREDIENTS -> Map |$i| { $i.percentage } -> Sum == 100.0
    ! "unique_names" INGREDIENTS -> Map |$i| { $i.name } -> Unique -> Len == INGREDIENTS -> Len
}
```

### Filtering and Counting

```yammm
part type TestResult {
    name String[1, 100] required
    passed Boolean required
    score Float[0.0, 100.0]
}

type TestSuite {
    id String primary

    *-> RESULTS (many) TestResult

    ! "all_passed" RESULTS -> All |$r| { $r.passed }
    ! "min_passing_score" RESULTS -> Filter |$r| { $r.passed } -> All |$r| { $r.score >= 60.0 }
}
```

---

## Relationship Patterns

### One-to-One via Association

```yammm
type User {
    id UUID primary
    username String[3, 50] required
}

type Profile {
    id UUID primary
    bio String[_, 1000]
    --> BELONGS_TO (one) User
}
```

### One-to-Many via Association

```yammm
type Author {
    id UUID primary
    name String[1, 100] required
}

type Book {
    id UUID primary
    title String[1, 300] required
    --> WRITTEN_BY (one) Author / BOOKS (many)
}
```

### Owned Children via Composition

```yammm
part type PhoneNumber {
    number Pattern["^\\+?[0-9\\-\\s]+$"] required
    label Enum["home", "work", "mobile"] required
}

type Contact {
    id UUID primary
    name String[1, 100] required
    *-> PHONES (one:many) PhoneNumber
}
```

### Many-to-Many via Link/Junction Type

Model many-to-many relationships using an explicit link type with two associations:

```yammm
type Student {
    id UUID primary
    name String[1, 100] required
}

type Course {
    id UUID primary
    title String[1, 200] required
}

type Enrollment {
    id UUID primary
    enrolled_at Timestamp required
    grade Float[0.0, 100.0]
    --> STUDENT (one) Student
    --> COURSE (one) Course
}
```

---

## Edge Property Patterns

### Weighted Relationship

```yammm
type Person {
    id UUID primary
    name String[1, 100] required
    --> KNOWS (_:many) Person {
        weight Float[0.0, 1.0] required
        since Date
    }
}
```

### Timestamped Edge

```yammm
type Employee {
    id UUID primary
    name String[1, 100] required
    --> WORKS_AT (one) Company {
        start_date Date required
        end_date Date
        title String[1, 100]
        is_current Boolean required
    }
}

type Company {
    id UUID primary
    name String[1, 200] required
}
```

---

## Import Patterns

### Relative Import (Sibling Schema)

```yammm
schema "orders"

import "./products" as products

type OrderLine {
    id UUID primary
    quantity Integer[1, _] required
    --> PRODUCT (one) products.Product
}
```

### Module Import (From Module Root)

```yammm
schema "reports"

import "models/core/users" as users
import "models/core/organizations" as orgs

type Report {
    id UUID primary
    title String[1, 300] required
    --> CREATED_BY (one) users.User
    --> BELONGS_TO (one) orgs.Organization
}
```

### Cross-Schema References with Data Type Aliases

```yammm
// common.yammm
schema "common"
type Money = Float[0.0, _]
type Percentage = Float[0.0, 100.0]
```

```yammm
// billing.yammm
schema "billing"
import "./common" as common

type Invoice {
    id UUID primary
    amount common.Money required
    tax_rate common.Percentage required
}
```

### Extending Imported Types

```yammm
// base.yammm
schema "base"
abstract type Auditable {
    created_at Timestamp required
    updated_at Timestamp
}
```

```yammm
// domain.yammm
schema "domain"
import "./base" as base

type Document extends base.Auditable {
    id UUID primary
    title String[1, 200] required
    content String required
}
```
