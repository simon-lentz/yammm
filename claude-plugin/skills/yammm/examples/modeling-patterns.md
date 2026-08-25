# Modeling Patterns

Complete mini-schemas demonstrating how to model different domain shapes in yammm.

---

## E-Commerce Order

Types, part types, compositions, associations, edge properties, type aliases, and collection invariants working together.

```yammm
schema "ecommerce"

type Money = Float[0.0, _]
type SKU = String[3, 50]
type Email = Pattern["^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$"]

abstract type Auditable {
    created_at Timestamp required
    updated_at Timestamp
}

type Customer extends Auditable {
    id UUID primary
    email Email required
    name String[1, 100] required
    tier Enum["standard", "premium", "enterprise"] required
}

type Product extends Auditable {
    id UUID primary
    sku SKU required
    name String[1, 200] required
    price Money required
    is_active Boolean required

    ! "price_positive" price > 0.0
}

part type LineItem {
    sku SKU required
    quantity Integer[1, _] required
    unit_price Money required
    discount Money

    ! "discount_cap" discount == nil || discount <= unit_price * quantity
}

type Order extends Auditable {
    id UUID primary
    status Enum["pending", "confirmed", "shipped", "delivered", "cancelled"] required
    currency String[3, 3] required
    notes String[_, 1000]

    --> PLACED_BY (one) Customer
    *-> ITEMS (one:many) LineItem

    ! "has_items" ITEMS -> Len > 0
    ! "max_items" ITEMS -> Len <= 200
    ! "all_positive_qty" ITEMS -> All |$i| { $i.quantity > 0 }
    ! "order_value" ITEMS -> Map |$i| { $i.unit_price * $i.quantity } -> Sum > 0.0
}
```

**Patterns demonstrated:** Type aliases for domain types (`Money`, `SKU`, `Email`). Abstract audit fields. Part type for line items with per-item invariants. Composition with `(one:many)` requiring at least one. Collection invariants using `All`, `Map`, `Sum`.

---

## Organizational Hierarchy

Abstract types, multiple inheritance, required vs optional relationships, and conditional invariants.

```yammm
schema "organization"

abstract type Named {
    name String[1, 200] required
    description String[_, 2000]
}

abstract type Temporal {
    effective_from Date required
    effective_until Date

    ! "date_range" effective_until == nil || effective_until > effective_from
}

type Department extends Named {
    id UUID primary
    code String[2, 10] required
    budget Float[0.0, _]
    is_active Boolean required

    --> PARENT (_) Department

    ! "code_uppercase" code == code -> Upper
}

type Role extends Named {
    id UUID primary
    level Enum["individual", "manager", "director", "executive"] required
    max_headcount Integer[1, _]
}

type Employee extends Named, Temporal {
    id UUID primary
    employee_number String[1, 20] required
    email Pattern["^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$"] required
    is_active Boolean required

    --> WORKS_IN (one) Department {
        start_date Date required
        end_date Date
    }
    --> HAS_ROLE (one) Role
    --> REPORTS_TO (_) Employee

    ! "active_has_dates" !is_active || effective_from != nil
}
```

**Patterns demonstrated:** Multiple abstract type inheritance (`Named`, `Temporal`). Self-referential associations (`PARENT -> Department`, `REPORTS_TO -> Employee`). Edge properties on `WORKS_IN`. Optional parent relationship (`(_)` multiplicity). Conditional invariant linking active status to dates.

---

## Content Management with Imports

Multi-schema design with imports, cross-schema relationships, part types, and List fields.

### `schemas/core.yammm`

```yammm
schema "core"

type Slug = Pattern["^[a-z0-9]+(-[a-z0-9]+)*$"]

abstract type Publishable {
    status Enum["draft", "review", "published", "archived"] required
    published_at Timestamp
    published_by String[1, 100]

    ! "publish_consistency" status != "published" || published_at != nil
    ! "publisher_present" published_at == nil || published_by != nil
}

type Author {
    id UUID primary
    name String[1, 100] required
    bio String[_, 500]
    email Pattern["^.+@.+\\..+$"] required
}

type Tag {
    id UUID primary
    name String[1, 50] required
    slug Slug required
}
```

### `schemas/articles.yammm`

```yammm
schema "articles"

import "./core" as core

part type Section {
    heading String[1, 200] required
    body String[1, _] required
    position Integer[0, _] required
}

type Article extends core.Publishable {
    id UUID primary
    title String[1, 300] required
    slug core.Slug required
    summary String[1, 500] required
    reading_time_minutes Integer[1, 120]
    keywords List<String[1, 50]>[_, 10]

    --> WRITTEN_BY (one) core.Author
    --> TAGGED_WITH (many) core.Tag
    *-> SECTIONS (one:many) Section

    ! "has_sections" SECTIONS -> Len > 0
    ! "sections_ordered" SECTIONS -> Map |$s| { $s.position } -> Unique -> Len == SECTIONS -> Len
    ! "keyword_uniqueness" keywords == nil || keywords -> Unique -> Len == keywords -> Len
}
```

**Patterns demonstrated:** Cross-schema imports with qualified references (`core.Author`, `core.Slug`). Imported abstract type extended in a different schema. Imported type aliases used as field types. Part type for ordered sections with position-uniqueness invariant. List field with element constraints and max length. Collection invariants checking uniqueness via `Unique -> Len` comparison.
