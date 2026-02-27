---
# yammm plugin settings (v0.1.0)
# Copy this file to .claude/yammm.local.md in your project root.

# Agent model preference: "sonnet" or "haiku"
default_model: sonnet

# Run schema-reviewer automatically after schema-author writes a file
auto_review: false

# Include an Auditable abstract type stub in /new-schema output
scaffold_audit_fields: false
---

# yammm Plugin Settings

This file configures the yammm Claude Code plugin. Place it at `.claude/yammm.local.md` in your project root.

## Available Settings

| Setting | Type | Default | Description |
| ------- | ---- | ------- | ----------- |
| `default_model` | `sonnet` \| `haiku` | `sonnet` | Model used by schema-author and schema-reviewer agents |
| `auto_review` | `bool` | `false` | Automatically run schema-reviewer after schema-author writes a file |
| `scaffold_audit_fields` | `bool` | `false` | Include an `Auditable` abstract type stub when `/new-schema` scaffolds a file |
