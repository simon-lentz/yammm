---
description: Format a .yammm schema file using canonical yammm style
argument-hint: "<file.yammm>"
allowed-tools: ["Read", "Write", "Glob"]
---

# format-schema

Format a yammm schema file using canonical style. This command reads the file and applies formatting rules directly. For editor-integrated formatting, use the `yammm-lsp` format-on-save or format command in the editor.

**Identify the target file:**

1. If an argument was provided, use it as the file path
2. If the user has a `.yammm` file open, use that
3. Otherwise, search for `.yammm` files with `Glob` and ask which one

**Read the file, then apply canonical yammm formatting:**

1. **Section ordering** -- schema declaration first (after leading comments), then imports, then type aliases, then type definitions
2. **Import grouping** -- all imports together with no blank lines between them; one blank line after the import block
3. **Blank lines** -- single blank line between top-level declarations (types, aliases); no trailing blank lines at end of file; no blank lines inside type bodies except one blank line to separate fields from relationships, and relationships from invariants
4. **Indentation** -- single tab for type body members; no indentation for top-level declarations
5. **Trailing whitespace** -- remove from all lines
6. **Property alignment** -- within each type body, align field names, types, and modifiers into consistent columns using spaces
7. **Consistent spacing** -- single space between `-->` / `*->` and relationship name; single space around multiplicity `(one)` / `(many)`; no extra blank lines between consecutive fields

**Write the result** using the Write tool. Show the user a summary of what changed (number of lines modified, categories of changes). If no changes were needed, report "File is already formatted."
