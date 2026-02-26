---
description: Scaffold a new .yammm schema file with boilerplate type definitions
argument-hint: "<schema-name>"
allowed-tools: ["Write", "Read", "Glob"]
---

# new-schema

Scaffold a new yammm schema file. Ask the user for:

1. **Schema name** -- the identifier used in the `schema "name"` declaration (e.g., `billing_invoices`)
2. **Output path** -- where to write the file (default: current directory, named `{schema_name}.yammm`)
3. **Entity names** -- the types to stub out (e.g., "Invoice, LineItem, Payment")

For each entity, ask whether it is:

- A concrete type (default -- gets a `primary` field stub)
- An abstract type (shared base -- no primary key)
- A part type (owned child -- used in compositions)

Generate the `.yammm` file with:

- `schema "name"` declaration
- Type stubs with a placeholder `primary` field (`id String primary`) for concrete types
- Comments indicating where to add properties and relationships
- A composition stub if any part types were specified

Example output for `schema "billing_invoices"` with types Invoice (concrete), LineItem (part):

```yammm
schema "billing_invoices"

// Data type aliases
// type Money = Float[0.0, _]

type Invoice {
    id String primary
    // Add properties here
    // field_name Type [required]

    // Composition for owned line items
    *-> ITEMS (one:many) LineItem
}

part type LineItem {
    // Add properties here (no primary key needed for part types)
    // field_name Type [required]
}
```

After writing the file, suggest the user run `/validate-schema` to check it, or invoke the schema-author agent for help filling in the details.
