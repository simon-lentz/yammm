// Package lsp implements a Language Server Protocol (LSP) server for YAMMM schema files
// and YAMMM code blocks embedded in Markdown documents.
//
// The LSP server provides IDE features including:
//   - Real-time diagnostics (parse errors, semantic errors, import issues)
//   - Go-to-definition for types, properties, and imports
//   - Hover information with documentation and constraints
//   - Completion for keywords and types
//   - Document symbols for outline and breadcrumbs
//   - Formatting with canonical style (tabs, LF)
//
// The server communicates via JSON-RPC 2.0 over stdio (using creachadair/jrpc2)
// and implements LSP 3.16. It leverages the existing schema/load package for
// analysis to ensure consistency between CLI and editor behavior.
//
// # Markdown Embedded Blocks
//
// YAMMM code blocks in Markdown files (.md, .markdown) receive diagnostics,
// hover, completion, go-to-definition, and document symbols support. Each
// code block is analyzed in isolation as an independent schema. Imports are
// not supported in markdown blocks and produce an E_IMPORT_NOT_ALLOWED
// diagnostic. Formatting is intentionally disabled for markdown files.
//
// # Architecture
//
// The root lsp package is a thin coordination layer containing the Server,
// protocol lifecycle, and feature providers. State management is enforced
// at the compiler level via internal packages.
//
// The server consists of:
//   - Server: Main LSP server handling protocol lifecycle
//   - Feature providers: Definition, hover, completion, symbols, formatting
//   - internal/workspace: Manages open documents, markdown lifecycle, analysis scheduling, dependency tracking, and URI mapping
//   - internal/docstate: Pure document types (.yammm overlays, text normalization, brace scanning) — true leaf, no internal imports
//   - internal/analysis: Wraps schema/load for import-aware analysis
//   - internal/symbols: Symbol extraction and indexing
//   - internal/format: YAMMM document formatting
//   - internal/markdown: Code block extraction from Markdown
//   - internal/lsputil: URI/path conversion and position encoding
//   - internal/lsperr: Sentinel errors for middleware log escalation and test assertions
//   - internal/protocol: LSP protocol type definitions
//
// # Usage
//
// The server is typically started via the yammm-lsp command:
//
//	yammm-lsp [options]
//
// The server communicates over stdio (implicit, no flag required).
//
// For debugging:
//
//	yammm-lsp --log-level debug --log-file /tmp/yammm-lsp.log
//
// # Limitations
//
// The server implements LSP 3.16, which does not support position encoding
// negotiation (added in LSP 3.17). UTF-16 encoding is assumed for all
// character positions.
//
// Documents must be opened (via textDocument/didOpen) before most LSP features
// work for that document. Specifically, hover, definition, completion, and
// formatting require the document to be open. This is because the server
// relies on overlay content for the most current text and analysis snapshots
// for semantic information. Imported files referenced by an open document
// are loaded from disk automatically during analysis.
//
// Only file:// URIs are supported. Documents with other URI schemes (such as
// untitled:, vscode-notebook-cell://, or custom editor schemes) are silently
// ignored in textDocument/didOpen. Editors opening unsaved buffers should
// save to a temporary file first or use a file:// URI pointing to a temp file.
package lsp
