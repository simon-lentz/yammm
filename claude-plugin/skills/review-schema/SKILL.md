---
name: review-schema
description: >-
  Reviews .yammm schema files for quality, consistency, and
  common mistakes. Use when reviewing a schema, checking for
  best practices, or asking for feedback on schema design.
allowed-tools: Read Grep Glob Bash(yammm *)
argument-hint: "[schema-file-path]"
---

# Schema Quality Review

Review `.yammm` schema files for correctness, consistency, and best practices. This skill produces a structured report with errors, warnings, and suggestions.

If `$ARGUMENTS` provides a file path, review that file. Otherwise, ask which schema to review.

---

## Process

1. **Compile the schema.** Run `yammm validate <file>` and `yammm check <file>` if data is available. Report any compiler diagnostics before proceeding to the manual checklist. Diagnostics go to **stderr at every severity**, and a warning does not change the exit code -- capture stderr and read it rather than trusting exit 0.

2. **Apply the review checklist** below against every type, property, relationship, and invariant in the schema. Read any imported schemas referenced by the file.

3. **Produce a structured report** in the output format at the end of this document.

---

## Review Checklist

### 1. Syntax Correctness

- Fields use space separation: `field_name Type modifier`, never colons.
- Keywords are correct: `type`, `abstract type`, `part type`, `schema`, `import`, `extends`.
- String literals use double quotes. Regex patterns inside `Pattern["..."]` use proper escaping.
- The `schema` declaration at the top uses a quoted string: `schema "name"`.

### 2. Primary Keys

- Every concrete type (not abstract, not part) has at least one field marked `primary` (multiple `primary` fields form a composite key).
- Abstract types are not required to declare a `primary` field; when they do, concrete subtypes inherit it (otherwise each concrete subtype supplies its own key).
- Part types are exempt from the primary-key requirement (they are identified through their parent composition) but are permitted to declare one. Do not report a keyed part type as an error; in a `(many)` composition it is preferable, giving each child a stable identity rather than a positional index.
- Primary key types are restricted to `String`, `UUID`, `Date`, and `Timestamp`. `Integer`, `Float`, `Boolean`, `Enum`, `Pattern`, `Vector`, and `List` are rejected. DataType aliases are resolved before checking.

### 3. Field Modifiers

- `primary` and `required` are never combined on the same field -- this is a parse error, not just redundant.
- Optional fields (no modifier) are intentionally optional -- flag fields that look like they should be `required` but are not.
- No unknown modifiers appear (only `primary`, `required`, or nothing).

### 4. Constraint Bounds

- Bounded types use correct notation: `String[min, max]`, `Integer[min, max]`, `Float[min, max]`.
- `_` is used for unbounded sides: `Integer[0, _]`, `String[1, _]`.
- Both bounds are present when brackets are used -- `String[255]` alone is invalid.
- Ranges are logically valid: min <= max.
- `Enum` values are quoted strings with at least two options.
- `Vector` takes a single positive integer dimension.
- `List` uses angle brackets for element type and optional square brackets for length bounds.
- `List` and `Vector` cannot appear in relationship property blocks.
- `Pattern` accepts 1 or 2 regex strings (max 2, conjunction semantics).

### 5. Multiplicity

- Multiplicity uses parentheses, never brackets: `(one)`, `(many)`, `(_:one)`, `(_:many)`.
- Required relationships use `(one)` or `(one:many)`. Optional use `(_:one)`, `(_:many)`, or `(many)`.
- `(many)` is optional (0-or-more).
- Flag `*-> (one)` as unusual -- verify it is intentional.

### 6. Invariants

- Syntax is `! "error_id" expression` -- the error ID is a quoted string.
- Built-in function names are capitalized: `Len`, `All`, `Any`, `AllOrNone`, `Count`, `Filter`, `Map`, `Reduce`, `Contains`, `StartsWith`, `EndsWith`, `Upper`, `Lower`, `Trim`, `Sum`, `Min`, `Max`, `Abs`, `Floor`, `Ceil`, `Round`, `Default`, `Coalesce`, `TypeOf`, `IsNil`.
- Pipeline syntax uses `->`: `ITEMS -> All |$item| { $item.quantity > 0 }`.
- Lambda parameters are prefixed with `$`: `|$x| { ... }`, `|$acc, $x| { ... }`.
- Nil checks use `== nil` or `!= nil`, or `val -> IsNil`.
- Invariant error IDs are unique within a type.

### 7. Part Types

- Part types are declared with `part type`, not plain `type`.
- Part types are only referenced as targets of composition edges (`*->`), never associations (`-->`).
- Associations (`-->`) target a concrete type only -- never an abstract type (which has no instances to reference) and never a part type.
- Part types may declare a `primary` field, and are exempt from being required to. Flag an unkeyed part type in a `(many)` composition as a suggestion, never an error.
- Part types cannot declare associations (`-->`).

### 8. Imports

- Import paths are quoted strings: `import "path/to/schema" as alias`.
- Aliases start with a letter and are not reserved keywords (`true`, `false`, `nil`, `type`, `schema`, `import`, and built-in type names are reserved).
- The `as alias` clause is optional -- alias is auto-derived from the last path segment when omitted.
- Imported types are referenced as `alias.TypeName` in relationship targets.
- No circular import chains.
- Relative paths start with `./` or `../`.

### 9. Inheritance

- The target of `extends` must be an `abstract type`.
- A child type can narrow parent constraint bounds but never widen them.
- A child type can add new fields and relationships not present on the parent.
- A child type does not redeclare the same field without narrowing the constraint.
- Enum narrowing: child enum values must be a subset of parent enum values.
- Invariants are inherited from parents (deduplicated by name, child overrides parent).

### 10. Annotations

The loader rejects a structurally wrong annotation, so these are the checks it cannot make.

- Every `@index` and `@@index` serves a stated lookup. An index nobody queries is a permanent write-time and storage cost. Ask what reads it; flag the ones with no answer.
- `@@index` argument **order is significant** -- it is the declared order, and the emitted composite index is built in it. Flag a composite whose order does not match how callers narrow the query.
- Flag a single-property `@index` whose property is also a `@@index` member, and confirm both are wanted. The loader accepts the pair; whether it is redundant depends on the store's composite-index behaviour, not on the schema.
- **Shadowed annotations are a review error, not a warning.** A subtype re-declaring an inherited property (identically or narrowing) drops that property's annotations unless they are re-stated. The load only warns (`W_ANNOTATION_SHADOWED`) and still succeeds, so it is easy to ship. Run `yammm validate` and read **stderr** -- a warning does not change the exit code.
- `@writeOnce` is a data-integrity decision, not a hint: it makes the Neo4j write path set the property on create only. Flag creation-time fields that lack it (`created_at`, `first_seen`, an origin identifier), and flag any that carry it but are legitimately updated.
- **The sole-primary-key redundancy rule has a known blind spot.** `@index` on a primary-key property is rejected only when every *visible* concrete type keying on it keys on it alone, and descendants declared in **other schema files** are invisible to that check. The same model is therefore accepted when split across files and rejected when written in one. Check annotated abstract mixins by hand.
- `@vector(cosine|euclidean)` requires a `Vector[N]` property; the emitted index dimension comes from `N`, so confirm `N` matches the embedding model actually in use. A mismatch is not a schema error -- it is a silent retrieval failure.
- Every `@fulltext` / `@@fulltext` serves a stated content-search need -- the same test as `@index`, and the two are not interchangeable: flag fulltext declared on a property callers only ever filter or sort exactly (that is `@index`'s job), and vice versa.
- The fulltext analyzer is not declarable: emitted DDL carries no `OPTIONS` clause, the store's default analyzer applies, and the diff does not compare analyzer configuration -- a remote index with a custom analyzer but matching properties reads as a Match. Flag a schema whose consumers depend on a non-default analyzer; that dependency lives outside the schema's authority and needs its own record.
- `@fulltext` on a primary-key member is legal for both placements -- do not flag it as redundant. The key's uniqueness constraint backs a range index, which cannot serve fulltext queries.
- Annotations are excluded from the structural hash, so adding, removing, or changing one never invalidates a persisted `.ys` snapshot. Do not flag annotation churn as a migration concern.

### 11. Common Anti-Patterns

- **Bare String everywhere**: Flag fields using plain `String` that clearly have a bounded domain. Suggest adding bounds.
- **Missing invariants**: Flag types with multiple related fields but no cross-field invariants (e.g., `start_date` + `end_date` without ordering).
- **Overly broad Enum**: >15 values may indicate a `Pattern` or separate type is better.
- **Deep composition nesting**: >2 levels of `*->` is unusual. Flag for review.
- **Duplicated field patterns**: Multiple types repeating the same fields suggest extracting an `abstract type`.
- **Missing reverse clause**: Relationships that semantically imply a named reverse benefit from `/ reverse_name`.
- **Unbounded List without invariant**: `List<T>` with no length bounds and no `Len` invariant may accept arbitrarily large payloads.
- **Associations targeting part types**: Associations (`-->`) cannot target `part type`. Only compositions (`*->`) can.

---

## Output Format

```md
## Schema Review: <file path>

### Errors -- Must fix
Items that are syntactically or semantically invalid.
- [E1] <type or line context>: <description>

### Warnings -- Likely mistakes
Items that are technically valid but likely indicate a problem.
- [W1] <type or line context>: <description>

### Suggestions -- Improvement opportunities
Opportunities to improve clarity, safety, or maintainability.
- [S1] <type or line context>: <description>

### Summary
<One-paragraph assessment: error/warning/suggestion counts and overall quality.>
```

Always produce all four sections. Write "None." for empty sections.
