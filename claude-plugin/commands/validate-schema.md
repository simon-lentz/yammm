---
description: Validate a .yammm schema file with structural checks and report issues
argument-hint: "<file.yammm>"
allowed-tools: ["Read", "Glob"]
---

# validate-schema

Quick structural validation of a yammm schema file. This command performs a manual checklist against the file contents. For comprehensive semantic validation (type resolution, import resolution, invariant evaluation), use the editor's built-in LSP diagnostics provided by `yammm-lsp`.

**Identify the target file:**

1. If an argument was provided, use it as the file path
2. If the user has a `.yammm` file open in their editor, use that
3. Otherwise, search for `.yammm` files with `Glob` and ask which one

**Read the file and check each item:**

1. **Schema declaration** -- file starts with `schema "name"` (after optional comments)
2. **Primary keys** -- every concrete type (not `abstract`, not `part`) has exactly one field marked `primary`; abstract and part types have zero `primary` fields
3. **Field syntax** -- fields use space separation (`name Type modifier`), no colons between name and type
4. **Field modifiers** -- only `primary` or `required` appear as modifiers; `primary` and `required` are never combined on the same field
5. **Constraint bounds** -- bracketed types have two values (`Type[min, max]`); `_` for unbounded sides; min <= max
6. **Enum values** -- `Enum` has at least 2 quoted string values
7. **Multiplicity syntax** -- uses parentheses `(one)`, `(many)`, `(_:one)`, `(_:many)`, never brackets `[one]`
8. **Relationship syntax** -- associations use `-->`, compositions use `*->`; part types are only targets of `*->`
9. **Invariant syntax** -- invariants use `! "error_id" expression`; built-in function names are capitalized (`Len`, `All`, `Any`); pipeline syntax (`value -> Func`) is used, not call syntax (`Func(value)`)
10. **Import syntax** -- imports use quoted paths with `as` alias; aliases are valid identifiers and not reserved keywords

**Present results:**

Group findings by severity:

**Errors** (structural problems that will cause validation failures):

- Line number, issue description, how to fix

**Warnings** (likely mistakes):

- Line number, issue description, recommendation

If no issues found, report: "Quick check passed -- no structural issues found. For full semantic validation, check the editor's LSP diagnostics panel."

**Limitations:**

This command performs text-level structural checks only. It cannot detect:

- Unresolved imports or circular import chains
- Type mismatches in invariant expressions
- Constraint violations in inherited fields
- Cross-schema reference errors

These require the `yammm-lsp` language server. If the user reports issues this command missed, suggest they check the editor's diagnostics panel or invoke the schema-reviewer agent for a comprehensive review.
