# Vendored Grammars

## markdown.tmLanguage.json

- **Source**: [microsoft/vscode](https://github.com/microsoft/vscode) `extensions/markdown-basics/syntaxes/markdown.tmLanguage.json`
- **Commit**: `a0ff9d162e91` (main, fetched 2026-02-19)
- **License**: MIT (https://github.com/microsoft/vscode/blob/main/LICENSE.txt)
- **Purpose**: Host grammar for snapshot testing the yammm markdown injection grammar. Provides the `text.html.markdown` scope that the injection grammar targets via `injectionSelector: "L:text.html.markdown"`.

This file is used only for `npm run test:grammar:injection` and is not bundled in the published extension.
