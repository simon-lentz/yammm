# Schema Improvements

Before/after transformations showing common schema quality improvements. Each section demonstrates a weak pattern and its corrected form.

---

## Bare Strings to Bounded Types

### Before

```yammm
schema "contacts"

type Contact {
    id UUID primary
    first_name String required
    last_name String required
    email String required
    phone String
    notes String
}
```

### After

```yammm
schema "contacts"

type Email = Pattern["^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$"]
type Phone = Pattern["^\\+?[0-9\\-\\s]{7,20}$"]

type Contact {
    id UUID primary
    first_name String[1, 100] required
    last_name String[1, 100] required
    email Email required
    phone Phone
    notes String[_, 2000]
}
```

**What changed:** Every String field now has bounds or a Pattern alias. `Email` and `Phone` use Pattern constraints for format enforcement. `notes` caps length without requiring a minimum.

---

## Missing Cross-Field Invariants

### Before

```yammm
schema "events"

type Event {
    id UUID primary
    name String[1, 200] required
    start_date Timestamp required
    end_date Timestamp
    min_attendees Integer[0, _]
    max_attendees Integer[0, _]
    status Enum["draft", "published", "cancelled"] required
    cancelled_at Timestamp
    cancelled_reason String
}
```

### After

```yammm
schema "events"

type Event {
    id UUID primary
    name String[1, 200] required
    start_date Timestamp required
    end_date Timestamp
    min_attendees Integer[0, _]
    max_attendees Integer[0, _]
    status Enum["draft", "published", "cancelled"] required
    cancelled_at Timestamp
    cancelled_reason String[1, 500]

    ! "end_after_start" end_date == nil || end_date > start_date
    ! "attendee_range" min_attendees == nil || max_attendees == nil || max_attendees >= min_attendees
    ! "cancel_consistency" status != "cancelled" || cancelled_at != nil
    ! "cancel_reason_present" cancelled_at == nil || cancelled_reason != nil
}
```

**What changed:** Four invariants enforce: date ordering, attendee range validity, cancellation timestamp required when cancelled, and reason required when timestamp is set. All use nil guards for optional fields.

---

## Repeated Fields to Abstract Type

### Before

```yammm
schema "cms"

type Article {
    id UUID primary
    title String[1, 300] required
    body String required
    created_at Timestamp required
    updated_at Timestamp
    created_by String[1, 100] required
    is_active Boolean required
    deactivated_at Timestamp
}

type Page {
    id UUID primary
    title String[1, 300] required
    slug String[1, 200] required
    body String required
    created_at Timestamp required
    updated_at Timestamp
    created_by String[1, 100] required
    is_active Boolean required
    deactivated_at Timestamp
}
```

### After

```yammm
schema "cms"

abstract type Auditable {
    created_at Timestamp required
    updated_at Timestamp
    created_by String[1, 100] required
}

abstract type SoftDeletable {
    is_active Boolean required
    deactivated_at Timestamp

    ! "deactivation_consistency" is_active || deactivated_at != nil
}

type Article extends Auditable, SoftDeletable {
    id UUID primary
    title String[1, 300] required
    body String required
}

type Page extends Auditable, SoftDeletable {
    id UUID primary
    title String[1, 300] required
    slug String[1, 200] required
    body String required
}
```

**What changed:** Audit and soft-delete fields extracted into abstract types with shared invariants. Each concrete type inherits the shared structure and only declares its own fields. The deactivation consistency invariant is defined once and inherited by both types.

---

## Regular Type to Part Type

### Before

```yammm
schema "orders"

type LineItem {
    id UUID primary
    sku String[1, 50] required
    quantity Integer[1, _] required
    unit_price Float[0.0, _] required
}

type Order {
    id UUID primary
    --> HAS_ITEM (many) LineItem
}
```

### After

```yammm
schema "orders"

part type LineItem {
    sku String[1, 50] required
    quantity Integer[1, _] required
    unit_price Float[0.0, _] required
}

type Order {
    id UUID primary
    *-> ITEMS (one:many) LineItem

    ! "has_items" ITEMS -> Len > 0
    ! "all_positive" ITEMS -> All |$i| { $i.quantity > 0 && $i.unit_price > 0.0 }
    ! "max_line_items" ITEMS -> Len <= 100
}
```

**What changed:** `LineItem` becomes a `part type` (no independent existence; identified through its parent composition). The association (`-->`) becomes a composition (`*->`). The multiplicity changes to `(one:many)` to require at least one item. Collection invariants enforce business rules on the embedded items.

---

## Unguarded Invariants to Nil-Safe Expressions

### Before

```yammm-snippet
// All fields below are optional -- these invariants fail at evaluation time
! "discount_cap" discount <= 50.0
! "date_order" end_date > start_date
! "ref_format" reference =~ /^REF-[0-9]+$/
! "total" subtotal + tax == total
```

### After

```yammm-snippet
! "discount_cap" discount == nil || discount <= 50.0
! "date_order" end_date == nil || end_date > start_date
! "ref_format" reference == nil || reference =~ /^REF-[0-9]+$/
! "total" subtotal == nil || tax == nil || total == nil || subtotal + tax == total
```

**What changed:** Each invariant guards against nil with short-circuit `||`. When the optional field is nil, the invariant passes. The comparison only runs when all operands are present.
