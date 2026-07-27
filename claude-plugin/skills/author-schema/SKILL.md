---
name: author-schema
description: >-
  Designs and writes .yammm schema files from requirements.
  Use when creating a new schema, designing a data model in
  yammm, or scaffolding types and relationships from data
  descriptions.
allowed-tools: Read Write Edit Grep Glob Bash(yammm *)
argument-hint: "[description of what to model]"
---

# Schema Authoring

Design and write `.yammm` schema files from requirements. This skill guides the authoring process from understanding the domain through compile-checked output.

If `$ARGUMENTS` provides a description, use it as the starting point for step 1.

---

## Process

1. **Understand the data model.** Ask clarifying questions if the domain, entities, relationships, or constraints are ambiguous. Identify primary keys, required fields, optionality, and cardinality before writing anything.

2. **Design the schema.** Choose appropriate types, constraint bounds, relationships (association vs composition), inheritance, and invariants. Prefer explicit constraints over loose ones.

3. **Write the .yammm file.** Produce the complete schema using correct syntax. Place it in the location the user specifies, or propose a sensible path.

4. **Verify.** After writing, run `yammm validate <file>` to compile-check the schema. Fix any reported errors before finishing. Diagnostics go to **stderr at every severity** and a warning does not change the exit code, so read the output rather than trusting exit 0 -- `W_ANNOTATION_SHADOWED` in particular means a re-declaration silently dropped an inherited annotation.

---

## Design Guidance

- **Primary keys**: Every concrete (non-abstract, non-part) type must have at least one `primary` field; multiple `primary` fields form a composite key. Only `String`, `UUID`, `Date`, and `Timestamp` are allowed as primary key types.
- **Required fields**: Mark fields `required` when a null value is never valid. Leave fields unmarked (optional) when absence is a legitimate state.
- **Constraint bounds**: Use bounded types to enforce data integrity. `String[1, 255]` is better than bare `String`. Use `_` for unbounded sides.
- **Type aliases**: Define `type Email = Pattern["..."]` to reduce repetition and improve readability.
- **Abstract types**: Use `abstract type` for shared field sets (e.g., audit fields). Concrete types extend them with `extends`.
- **Part types and compositions**: Declare `part type` for entities that have no independent existence. Reference them only via `*->`. Part types cannot have `primary` fields or associations.
- **Associations**: Use `-->` for relationships between independent types. An association must target a concrete type (not abstract or part) — its edge is resolved by the target's primary key, which only concrete instances have. Edge properties cannot use `Vector` or `List` types.
- **Lists**: Use `List<ElementType>` for ordered multi-value fields. Add length bounds when the domain has known limits.
- **Invariants**: Add `! "error_id" expression` for business rules. Use capitalized built-in functions: `Len`, `All`, `Any`, `Contains`, etc.
- **Using `primary required` together is a parse error** -- `primary` already implies required.
- **Annotations**: Declare store-level intent the model implies. They do not affect which data is valid, and they are excluded from the structural hash, so adding one never invalidates a persisted snapshot.
  - `@index` on a property the domain says will be filtered or sorted on. Scalars only. Not on a sole primary key -- its uniqueness constraint already backs an index, and the loader rejects it.
  - `@@index(a, b)` at the type level for a composite lookup. Argument **order is significant** and should match the intended query shape; write it in the order a caller narrows.
  - `@vector(cosine)` or `@vector(euclidean)` on a `Vector[N]` property. `N` is the embedding model's dimension, so pick the model before writing the type.
  - `@writeOnce` on anything set at creation and never rewritten -- `created_at`, `first_seen`, an origin identifier. It makes the Neo4j write path set the property `ON CREATE` only. Never on a primary-key member: a merge match key is already immutable, and the loader rejects it.
  - Do not annotate speculatively. Every index is a write-time and storage cost the model is committing the deployment to.

---

## References

For detailed syntax, patterns, and expression language, consult the `yammm` skill's reference files:

- `references/dsl-syntax.md` -- full grammar for types, properties, relationships, annotations, imports
- `references/type-system.md` -- constraint types, aliases, abstract/part, inheritance rules
- `references/patterns.md` -- common modeling patterns with examples
- `references/expressions.md` -- operators, pipeline, lambdas, built-in functions for invariants
- `references/quick-reference.md` -- compact syntax cheat sheet, including the blessed annotation set
