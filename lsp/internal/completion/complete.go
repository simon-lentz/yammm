package completion

import (
	"cmp"
	"log/slog"
	"slices"
	"strings"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"

	"github.com/simon-lentz/yammm/lsp/internal/analysis"
	"github.com/simon-lentz/yammm/lsp/internal/docstate"
	"github.com/simon-lentz/yammm/lsp/internal/lsputil"
	"github.com/simon-lentz/yammm/lsp/internal/protocol"
	"github.com/simon-lentz/yammm/lsp/internal/symbols"
)

// Pre-allocated Detail strings for completion items to avoid per-item heap allocations.
// Static strings that appear in many items share a single *string.
var (
	detailBuiltinType = new("Built-in type")
	detailType        = new("Type")
	detailDatatype    = new("Datatype")
)

// BuiltinTypes are the built-in type keywords available for property types.
var BuiltinTypes = []string{
	"Boolean",
	"Date",
	"Enum",
	"Float",
	"Integer",
	"List",
	"Pattern",
	"String",
	"Timestamp",
	"UUID",
	"Vector",
}

// Complete returns completion items for the given position.
// snapshot may be nil -- graceful degradation provides keywords, snippets,
// and built-in types without a schema.
// The line and char parameters are LSP-encoding coordinates.
func Complete(snapshot *analysis.Snapshot, doc *docstate.Snapshot, line, char int, enc lsputil.PositionEncoding, logger *slog.Logger) []protocol.CompletionItem {
	if snapshot != nil && snapshot.EntryVersion != doc.Version {
		logger.Debug("serving stale snapshot for completion",
			"uri", doc.URI,
			"snapshot_version", snapshot.EntryVersion,
			"doc_version", doc.Version,
		)
	}

	var byteOffset int
	usedRegistry := false
	if snapshot != nil && snapshot.Sources != nil {
		if offset, ok := lsputil.ByteOffsetFromLSP(
			snapshot.Sources,
			doc.SourceID,
			line,
			char,
			enc,
		); ok {
			byteOffset = offset
			usedRegistry = true
			lineStart, lineOk := snapshot.Sources.LineStartByte(doc.SourceID, line+1)
			if lineOk {
				byteOffset -= lineStart
				if byteOffset < 0 {
					byteOffset = 0
				}
			}
		}
	}
	if !usedRegistry {
		byteOffset = ComputeByteOffsetFromText(doc.Text, line, char, enc)
	}

	ctx := DetectContext(doc, line, byteOffset)

	logger.Debug("completion context", "context", ctx)

	var items []protocol.CompletionItem

	switch ctx {
	case TopLevel:
		items = TopLevelCompletions()
	case TypeBody:
		items = TypeBodyCompletions()
	case Extends:
		items = TypeCompletions(snapshot, doc.SourceID)
	case PropertyType:
		items = PropertyTypeCompletions(snapshot, doc.SourceID)
	case RelationTarget:
		items = TypeCompletions(snapshot, doc.SourceID)
	case ImportPath:
		items = ImportCompletions()
	default:
		items = TopLevelCompletions()
	}

	slices.SortFunc(items, func(a, b protocol.CompletionItem) int {
		if a.SortText != nil && b.SortText != nil {
			return cmp.Compare(*a.SortText, *b.SortText)
		}
		return cmp.Compare(a.Label, b.Label)
	})

	return items
}

// ComputeByteOffsetFromText computes a byte offset within a line from document text.
// This is used when no source registry is available (before first analysis).
// It respects the negotiated position encoding (UTF-16 or UTF-8).
func ComputeByteOffsetFromText(text string, lspLine, lspChar int, enc lsputil.PositionEncoding) int {
	lines := strings.Split(text, "\n")
	if lspLine >= len(lines) {
		return lspChar // fallback
	}
	return lsputil.CharToByteOnLine([]byte(lines[lspLine]), lspChar, enc)
}

// TopLevelCompletions returns completions for top-level context.
func TopLevelCompletions() []protocol.CompletionItem {
	items := []protocol.CompletionItem{
		KeywordCompletion("schema", "schema \"${1:name}\"", "Schema declaration"),
		KeywordCompletion("import", "import \"${1:./path}\"${2: as ${3:alias}}", "Import statement"),
		KeywordCompletion("type", "type ${1:Name} {\n\t$0\n}", "Type declaration"),
		KeywordCompletion("abstract type", "abstract type ${1:Name} {\n\t$0\n}", "Abstract type declaration"),
		KeywordCompletion("part type", "part type ${1:Name} {\n\t$0\n}", "Part type declaration"),
	}

	// Add datatype completions (3.6)
	items = append(items, KeywordCompletion("datatype", "type ${1:Name} = ${2|String,Integer,Float,Boolean,UUID,Date,Timestamp,Enum,Pattern|}", "Datatype alias"))
	items = append(items, KeywordCompletion("datatype with constraint", "type ${1:Name} = ${2|String,Integer|}[${3:min}, ${4:max}]", "Datatype alias with numeric constraints"))

	return items
}

// TypeBodyCompletions returns completions for inside a type body.
func TypeBodyCompletions() []protocol.CompletionItem {
	items := []protocol.CompletionItem{
		// Property snippets - modifiers are space-separated per grammar (only 'primary' or 'required')
		// Format: ${N|, modifier1, modifier2|} - empty first option, space-prefixed subsequent
		SnippetCompletion("property", "${1:name} ${2|String,Integer,Float,Boolean,UUID|}${3|, required, primary|}", "Property declaration"),
		SnippetCompletion("property with constraint", "${1:name} ${2:String}[${3:1}, ${4:100}]${5|, required|}", "Property with constraint"),

		// Relation snippets
		SnippetCompletion("association", "--> ${1:NAME} (${2|one,many|}) ${3:TargetType}", "Association declaration"),
		SnippetCompletion("composition", "*-> ${1:CONTAINS} (${2|one,many|}) ${3:PartType}", "Composition declaration"),

		// Invariant snippet
		SnippetCompletion("invariant", "! \"${1:message}\" ${2:expression}", "Invariant declaration"),
	}

	// Add built-in types for quick access
	for _, t := range BuiltinTypes {
		sortText := "2_" + t
		kind := protocol.CompletionItemKindTypeParameter
		items = append(items, protocol.CompletionItem{
			Label:    t,
			Kind:     &kind,
			SortText: &sortText,
			Detail:   detailBuiltinType,
		})
	}

	return items
}

// TypeCompletions returns type name completions.
// sourceID should be the canonical (symlink-resolved) SourceID from the document.
func TypeCompletions(snapshot *analysis.Snapshot, sourceID location.SourceID) []protocol.CompletionItem {
	items := make([]protocol.CompletionItem, 0)

	if snapshot == nil || snapshot.Schema == nil {
		return items
	}

	// Add local types
	for name := range snapshot.Schema.Types() {
		sortText := "0_" + name // Local types first
		kind := protocol.CompletionItemKindClass
		items = append(items, protocol.CompletionItem{
			Label:    name,
			Kind:     &kind,
			SortText: &sortText,
			Detail:   detailType,
		})
	}

	// Add imported types with qualifier
	idx := snapshot.SymbolIndexAt(sourceID)
	if idx != nil {
		for i := range idx.Symbols {
			sym := &idx.Symbols[i]
			if sym.Kind == symbols.SymbolImport {
				imp, ok := sym.Data.(*schema.Import)
				if !ok || imp.Schema() == nil {
					continue
				}

				alias := imp.Alias()
				// Allocate detail string once per import alias, shared across all types from this import.
				detail := "Imported type (" + alias + ")"
				for typeName := range imp.Schema().Types() {
					qualifiedName := alias + "." + typeName
					sortText := "1_" + qualifiedName // Imported types second
					kind := protocol.CompletionItemKindClass
					items = append(items, protocol.CompletionItem{
						Label:    qualifiedName,
						Kind:     &kind,
						SortText: &sortText,
						Detail:   &detail,
					})
				}
			}
		}
	}

	return items
}

// PropertyTypeCompletions returns completions for property type position.
// sourceID should be the canonical (symlink-resolved) SourceID from the document.
func PropertyTypeCompletions(snapshot *analysis.Snapshot, sourceID location.SourceID) []protocol.CompletionItem {
	var items []protocol.CompletionItem

	// Add built-in types first
	for _, t := range BuiltinTypes {
		sortText := "0_" + t
		kind := protocol.CompletionItemKindTypeParameter
		items = append(items, protocol.CompletionItem{
			Label:    t,
			Kind:     &kind,
			SortText: &sortText,
			Detail:   detailBuiltinType,
		})
	}

	if snapshot == nil || snapshot.Schema == nil {
		return items
	}

	// Add local datatypes
	for name := range snapshot.Schema.DataTypes() {
		sortText := "1_" + name
		kind := protocol.CompletionItemKindTypeParameter
		items = append(items, protocol.CompletionItem{
			Label:    name,
			Kind:     &kind,
			SortText: &sortText,
			Detail:   detailDatatype,
		})
	}

	// Add imported datatypes with qualifier
	idx := snapshot.SymbolIndexAt(sourceID)
	if idx != nil {
		for i := range idx.Symbols {
			sym := &idx.Symbols[i]
			if sym.Kind == symbols.SymbolImport {
				imp, ok := sym.Data.(*schema.Import)
				if !ok || imp.Schema() == nil {
					continue
				}

				alias := imp.Alias()
				// Allocate detail string once per import alias, shared across all datatypes from this import.
				detail := "Imported datatype (" + alias + ")"
				for dtName := range imp.Schema().DataTypes() {
					qualifiedName := alias + "." + dtName
					sortText := "2_" + qualifiedName
					kind := protocol.CompletionItemKindTypeParameter
					items = append(items, protocol.CompletionItem{
						Label:    qualifiedName,
						Kind:     &kind,
						SortText: &sortText,
						Detail:   &detail,
					})
				}
			}
		}
	}

	return items
}

// ImportCompletions returns completions for import context.
func ImportCompletions() []protocol.CompletionItem {
	// Use optional alias form: ${2: as ${3:alias}} makes the " as alias" part optional.
	// This matches the grammar (alias is optional) and topLevelCompletions().
	return []protocol.CompletionItem{
		SnippetCompletion("import", "import \"${1:./path}\"${2: as ${3:alias}}", "Import statement"),
	}
}

// KeywordCompletion creates a keyword completion item.
func KeywordCompletion(label, insertText, detail string) protocol.CompletionItem {
	kind := protocol.CompletionItemKindKeyword
	format := protocol.InsertTextFormatSnippet
	sortText := "0_" + label
	return protocol.CompletionItem{
		Label:            label,
		Kind:             &kind,
		Detail:           &detail,
		InsertText:       &insertText,
		InsertTextFormat: &format,
		SortText:         &sortText,
	}
}

// SnippetCompletion creates a snippet completion item.
func SnippetCompletion(label, insertText, detail string) protocol.CompletionItem {
	kind := protocol.CompletionItemKindSnippet
	format := protocol.InsertTextFormatSnippet
	sortText := "1_" + label
	return protocol.CompletionItem{
		Label:            label,
		Kind:             &kind,
		Detail:           &detail,
		InsertText:       &insertText,
		InsertTextFormat: &format,
		SortText:         &sortText,
	}
}
