// Package lsp implements a Language Server Protocol (LSP) server for YAMMM schema files
// and YAMMM code blocks embedded in Markdown documents.
//
// The LSP server provides IDE features including:
//   - Real-time diagnostics (parse errors, semantic errors, import issues)
//   - Go-to-definition for types, properties, and imports
//   - Hover information with documentation, constraints, and annotations
//   - Completion for keywords, types, and annotations
//   - Document symbols for outline and breadcrumbs
//   - Formatting with canonical style (tabs, LF)
//
// # Annotation Support
//
// Annotation names and their arguments complete, and an annotation name hovers
// with its description and argument hint. Both read the built-in registry
// through [github.com/simon-lentz/yammm/schema.AnnotationSpecs] and
// [github.com/simon-lentz/yammm/schema.VectorSimilarityFunctions] rather than
// hard-coding a list, so the editor cannot offer an annotation or a keyword the
// loader rejects.
//
// Completion is placement-aware: a property context offers the property-level
// names, a @@ context the type-level ones, and a name that takes arguments
// inserts its parentheses with the cursor inside, where argument completion
// takes over. Argument completion covers the @vector similarity keywords;
// @@index property references are not completed.
//
// Annotation hover is parse-independent — it scans the line rather than the
// analysis snapshot — so it still answers while the document does not compile,
// which is when a reader is most likely to be asking what an annotation means.
//
// The server communicates via JSON-RPC 2.0 over stdio (using creachadair/jrpc2)
// and implements LSP 3.16. It leverages the schema package's Load functions
// for analysis to ensure consistency between CLI and editor behavior.
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
//   - Feature providers: definition, hover, completion, symbols, formatting
//   - internal/workspace: Manages open documents, markdown lifecycle, analysis scheduling, dependency tracking, and URI mapping
//   - internal/docstate: Pure document types (.yammm overlays, text normalization, brace scanning) — true leaf, no internal imports
//   - internal/analysis: Wraps schema loading for import-aware analysis
//   - internal/completion: Completion provider (keywords, types)
//   - internal/definition: Go-to-definition provider
//   - internal/hover: Hover information provider
//   - internal/symbols: Symbol extraction and indexing
//   - internal/markdown: Code block extraction from Markdown
//   - internal/lsputil: URI/path conversion and position encoding
//   - internal/lsperr: Sentinel errors for middleware log escalation and test assertions
//   - internal/protocol: LSP protocol type definitions
//
// # Go API
//
// For programmatic embedding, use [NewServer] with a [Config] and options:
//
//	srv := lsp.NewServer(logger, lsp.Config{
//	    ModuleRoot: "/path/to/project",
//	    Version:    "0.2.0",
//	})
//	err := srv.RunStdio()
//
// [Server.Close] performs graceful shutdown. [Server.Mux] exposes the
// underlying handler map for custom protocol extensions.
//
// # CLI Usage
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
//
// # Thread Safety
//
// The [Server] handles concurrent LSP requests internally via the jrpc2
// server. Callers should not invoke server methods concurrently — the
// protocol layer serializes requests.
//
// # Dependencies
//
// The lsp package depends on yammm library packages (schema, diag, location,
// format) and [github.com/creachadair/jrpc2] for JSON-RPC 2.0 transport.
package lsp
