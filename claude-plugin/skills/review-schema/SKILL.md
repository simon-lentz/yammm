---
name: review-schema
metadata:
  version: 0.1.0
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

1. **Compile the schema.** Run `yammm validate <file>` and `yammm check <file>` if data is available. Report any compiler diagnostics before proceeding to the manual checklist.

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

- Every concrete type (not abstract, not part) has exactly one field marked `primary`.
- Abstract types do not declare `primary` fields (the concrete child supplies the key).
- Part types do not declare `primary` fields (they are identified by their parent composition).
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
- Part types do not declare `primary` fields.
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

### 10. Common Anti-Patterns

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
