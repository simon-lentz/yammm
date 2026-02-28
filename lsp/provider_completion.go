package lsp

import (
	"sort"
	"strings"

	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// CompletionContext represents the context for completion.
type CompletionContext int

const (
	ContextUnknown CompletionContext = iota
	ContextTopLevel
	ContextTypeBody
	ContextExtends
	ContextPropertyType
	ContextRelationTarget
	ContextImportPath
)

// builtinTypes are the built-in type keywords available for property types.
var builtinTypes = []string{
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

// textDocumentCompletion handles textDocument/completion requests.
//
//nolint:nilnil // LSP protocol: nil result means no completions
func (s *Server) textDocumentCompletion(_ *glsp.Context, params *protocol.CompletionParams) (any, error) {
	uri := params.TextDocument.URI

	s.logger.Debug("completion request",
		"uri", uri,
		"line", params.Position.Line,
		"character", params.Position.Character,
	)

	if mdSnap := s.workspace.GetMarkdownDocumentSnapshot(uri); mdSnap != nil {
		return s.markdownCompletion(params, mdSnap)
	}

	snapshot := s.workspace.LatestSnapshot(uri)

	doc := s.workspace.GetDocumentSnapshot(uri)
	if doc == nil {
		return nil, nil
	}

	return s.completionAtPosition(snapshot, doc,
		int(params.Position.Line), int(params.Position.Character))
}

// markdownCompletion handles completion requests within yammm code blocks in markdown files.
//
//nolint:nilnil // LSP protocol: nil result means no completions
func (s *Server) markdownCompletion(params *protocol.CompletionParams, mdSnap *MarkdownDocumentSnapshot) (any, error) {
	blockPos := mdSnap.MarkdownPositionToBlock(int(params.Position.Line), int(params.Position.Character))
	if blockPos == nil {
		return nil, nil
	}

	// Extract snapshot (may be nil — completionAtPosition gracefully degrades
	// to keywords, snippets, and built-in types without a snapshot).
	var snapshot *Snapshot
	if blockPos.BlockIndex < len(mdSnap.Snapshots) {
		snapshot = mdSnap.Snapshots[blockPos.BlockIndex]
	}

	if blockPos.BlockIndex >= len(mdSnap.Blocks) {
		return nil, nil
	}
	blockDocSnap := s.buildBlockDocumentSnapshot(mdSnap, mdSnap.Blocks[blockPos.BlockIndex])

	return s.completionAtPosition(snapshot, blockDocSnap, blockPos.LocalLine, blockPos.LocalChar)
}

// completionAtPosition returns completion items for the given position.
// snapshot may be nil — graceful degradation provides keywords, snippets,
// and built-in types without a schema.
// The line and char parameters are LSP-encoding coordinates.
func (s *Server) completionAtPosition(snapshot *Snapshot, doc *DocumentSnapshot, line, char int) (any, error) {
	if snapshot != nil && snapshot.EntryVersion != doc.Version {
		s.logger.Debug("serving stale snapshot for completion",
			"uri", doc.URI,
			"snapshot_version", snapshot.EntryVersion,
			"doc_version", doc.Version,
		)
	}

	var byteOffset int
	usedRegistry := false
	if snapshot != nil && snapshot.Sources != nil {
		if offset, ok := ByteOffsetFromLSP(
			snapshot.Sources,
			doc.SourceID,
			line,
			char,
			s.workspace.PositionEncoding(),
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
		byteOffset = s.computeByteOffsetFromText(doc.Text, line, char)
	}

	ctx := s.detectCompletionContext(doc, line, byteOffset)

	s.logger.Debug("completion context", "context", ctx)

	var items []protocol.CompletionItem

	switch ctx {
	case ContextTopLevel:
		items = s.topLevelCompletions()
	case ContextTypeBody:
		items = s.typeBodyCompletions()
	case ContextExtends:
		items = s.typeCompletions(snapshot, doc.SourceID)
	case ContextPropertyType:
		items = s.propertyTypeCompletions(snapshot, doc.SourceID)
	case ContextRelationTarget:
		items = s.typeCompletions(snapshot, doc.SourceID)
	case ContextImportPath:
		items = s.importCompletions()
	default:
		items = s.topLevelCompletions()
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].SortText != nil && items[j].SortText != nil {
			return *items[i].SortText < *items[j].SortText
		}
		return items[i].Label < items[j].Label
	})

	return items, nil
}

// detectCompletionContext analyzes text around cursor to determine context.
// It accepts a DocumentSnapshot to leverage cached LineState for O(1) type body detection.
func (s *Server) detectCompletionContext(doc *DocumentSnapshot, line, character int) CompletionContext {
	lines := strings.Split(doc.Text, "\n")
	if line < 0 || line >= len(lines) {
		return ContextUnknown
	}

	currentLine := lines[line]

	// Get text before cursor on current line
	if character > len(currentLine) {
		character = len(currentLine)
	}
	beforeCursor := currentLine[:character]

	// Check for import path context (before property type to avoid "import " matching)
	if s.isImportContext(beforeCursor) {
		return ContextImportPath
	}

	// Check for extends context
	if s.isExtendsContext(beforeCursor, lines, line) {
		return ContextExtends
	}

	// Check for relation target context (after --> or *->)
	if s.isRelationTargetContext(beforeCursor) {
		return ContextRelationTarget
	}

	// Check for property type context (identifier followed by space)
	if s.isPropertyTypeContext(beforeCursor) {
		return ContextPropertyType
	}

	// Check if we're inside a type body using cached LineState (O(1))
	// Falls back to direct computation if LineState is unavailable.
	// Pass character offset to handle cursor before closing brace on same line.
	if s.isInsideTypeBody(doc, lines, line, character) {
		return ContextTypeBody
	}

	// Default to top-level
	return ContextTopLevel
}

// isExtendsContext checks if cursor is after "extends" keyword.
func (s *Server) isExtendsContext(beforeCursor string, lines []string, currentLine int) bool {
	trimmed := strings.TrimSpace(beforeCursor)

	// Direct match on current line
	if strings.HasSuffix(trimmed, "extends") ||
		strings.Contains(trimmed, "extends ") {
		return true
	}

	// Check if we're on a continuation of extends (after comma)
	if strings.HasSuffix(trimmed, ",") || trimmed == "" {
		// Look backwards for extends on previous lines
		for i := currentLine; i >= 0 && i > currentLine-3; i-- {
			prevLine := strings.TrimSpace(lines[i])
			if strings.Contains(prevLine, "extends") && !strings.HasSuffix(prevLine, "{") {
				return true
			}
			if strings.HasSuffix(prevLine, "{") {
				break
			}
		}
	}

	return false
}

// isRelationTargetContext checks if cursor is after --> or *-> (relation arrow).
func (s *Server) isRelationTargetContext(beforeCursor string) bool {
	trimmed := strings.TrimSpace(beforeCursor)

	// Check for patterns like "--> NAME (one)" or "--> NAME (many)"
	// Also check for incomplete patterns
	if strings.Contains(trimmed, "-->") || strings.Contains(trimmed, "*->") {
		// Check if we're past the relation name and multiplicity
		if strings.Contains(trimmed, ")") {
			// After closing paren of multiplicity, we're at target
			afterParen := trimmed[strings.LastIndex(trimmed, ")")+1:]
			return strings.TrimSpace(afterParen) == "" ||
				!strings.Contains(afterParen, " ")
		}
	}

	return false
}

// isPropertyTypeContext checks if cursor is at property type position.
func (s *Server) isPropertyTypeContext(beforeCursor string) bool {
	trimmed := strings.TrimSpace(beforeCursor)

	// Check if line starts with an identifier followed by space (property name position)
	// Pattern: "identifier " where we need to provide type
	parts := strings.Fields(trimmed)
	if len(parts) == 1 && isIdentifier(parts[0]) {
		// Just an identifier - might be property name waiting for type
		if strings.HasSuffix(beforeCursor, " ") {
			return true
		}
	}

	return false
}

// isImportContext checks if cursor is in an import statement's path portion.
// Returns true only when the cursor is positioned where import path completion
// would be helpful:
//   - After "import" keyword (before or inside quoted path)
//   - Not after the closing quote of a complete path
//   - Not in the alias portion (after "as" keyword)
//
// Requires word boundary after "import" to avoid false positives on identifiers
// like "imported_name" or "importFrom".
func (s *Server) isImportContext(beforeCursor string) bool {
	trimmed := strings.TrimSpace(beforeCursor)

	// Not import context if we haven't typed "import" yet
	if !strings.HasPrefix(trimmed, "import") {
		return false
	}

	// Check for word boundary after "import"
	rest := trimmed[6:] // len("import") = 6
	if len(rest) == 0 {
		return true // Just "import"
	}
	if rest[0] != ' ' && rest[0] != '\t' {
		return false // e.g., "imported_name"
	}

	// Cursor is after "import " - check if we're in the path portion
	afterImport := strings.TrimLeft(rest, " \t")

	// If we see "as" keyword, cursor is in alias section - not import context.
	// Check for "as" with whitespace (space or tab) on both sides.
	if containsAsKeyword(afterImport) {
		return false
	}

	// Check if we have a complete quoted path (supports both single and double quotes).
	// Scan for opening quote, then look for matching closing quote respecting escapes.
	if isQuotedStringComplete(afterImport) {
		return false // Path is already complete
	}

	return true
}

// isQuotedStringComplete checks if a string contains a complete quoted string literal.
// Supports both single and double quotes, and respects escape sequences.
// Returns true if there's a complete quoted string (opening and closing quote of same type).
func isQuotedStringComplete(s string) bool {
	i := 0
	for i < len(s) {
		// Find opening quote
		if s[i] == '"' || s[i] == '\'' {
			quoteChar := s[i]
			i++
			// Scan for matching closing quote
			for i < len(s) {
				if s[i] == '\\' && i+1 < len(s) {
					// Skip escaped character
					i += 2
					continue
				}
				if s[i] == quoteChar {
					// Found matching closing quote
					return true
				}
				i++
			}
			// Reached end without finding closing quote
			return false
		}
		i++
	}
	// No opening quote found
	return false
}

// containsAsKeyword checks if the string contains "as" as a keyword (with whitespace boundaries).
// Returns true if "as" is preceded by whitespace and followed by whitespace or end of string.
// This handles both spaces and tabs uniformly.
func containsAsKeyword(s string) bool {
	// Search for "as" and verify word boundaries
	idx := 0
	for {
		pos := strings.Index(s[idx:], "as")
		if pos == -1 {
			return false
		}
		absPos := idx + pos

		// Check preceding character (must be whitespace or start of string).
		// Quote characters are NOT word boundaries—"as at path start (e.g., `import "as`)
		// should not be detected as the `as` keyword.
		if absPos > 0 {
			prevChar := s[absPos-1]
			if prevChar != ' ' && prevChar != '\t' {
				// "as" is part of another word (e.g., "class") or inside a quoted path
				idx = absPos + 2
				continue
			}
		}

		// Check following character (must be whitespace or end of string)
		afterAs := absPos + 2
		if afterAs < len(s) {
			nextChar := s[afterAs]
			if nextChar != ' ' && nextChar != '\t' {
				// "as" is part of another word (e.g., "assets")
				idx = absPos + 2
				continue
			}
		}

		// Found "as" as a standalone keyword
		return true
	}
}

// isInsideTypeBody checks if cursor is inside a type body using cached brace depth (O(1)).
//
// When LineState is available and valid, this uses the cached depth at the start of the
// current line, then scans up to the cursor position to compute the exact depth.
// Falls back to direct computation if LineState is unavailable or stale.
//
// The cursorCol parameter is the byte offset within the current line.
func (s *Server) isInsideTypeBody(doc *DocumentSnapshot, lines []string, currentLine, cursorCol int) bool {
	// Use cached brace depth if available and matches current document version
	if doc != nil && doc.LineState != nil && doc.LineState.Version == doc.Version {
		if currentLine < len(doc.LineState.BraceDepth) {
			// Get depth and block comment state at start of current line
			// (these are the values at the END of the previous line)
			startDepth := 0
			startInBlockComment := false
			if currentLine > 0 && currentLine-1 < len(doc.LineState.BraceDepth) {
				startDepth = doc.LineState.BraceDepth[currentLine-1]
				if currentLine-1 < len(doc.LineState.InBlockComment) {
					startInBlockComment = doc.LineState.InBlockComment[currentLine-1]
				}
			}

			// If start depth is 0 or negative, definitely outside
			if startDepth <= 0 {
				// But check if there's an opening brace before cursor on this line
				if currentLine < len(lines) {
					return s.hasNetPositiveDepthUpToColumn(lines[currentLine], cursorCol, startInBlockComment)
				}
				return false
			}

			// Start depth > 0, so we're potentially inside.
			// Scan current line up to cursor to check if we're still inside.
			if currentLine < len(lines) {
				return s.depthAtColumn(lines[currentLine], cursorCol, startDepth, startInBlockComment) > 0
			}
			return startDepth > 0
		}
		// Line out of range - shouldn't happen but be defensive
		return false
	}

	// Fallback: compute directly (O(n) where n = currentLine)
	return s.isInsideTypeBodyDirect(lines, currentLine, cursorCol)
}

// isInsideTypeBodyDirect checks if cursor is inside a type body using token-aware brace counting.
//
// This heuristic counts '{' and '}' characters to determine nesting depth while properly
// skipping braces inside comments (// and /* */) and string literals (" and ').
// This prevents false positives from braces in comments like `// TODO: refactor { this }`.
//
// The cursorCol parameter is the byte offset within the current line.
func (s *Server) isInsideTypeBodyDirect(lines []string, currentLine, cursorCol int) bool {
	braceDepth := 0
	inBlockComment := false

	for i := 0; i <= currentLine && i < len(lines); i++ {
		line := lines[i]
		// On the current line, only scan up to cursor position
		maxCol := len(line)
		if i == currentLine && cursorCol < maxCol {
			maxCol = cursorCol
		}

		j := 0
		for j < maxCol {
			// Handle block comment continuation
			if inBlockComment {
				if j+1 < len(line) && line[j] == '*' && line[j+1] == '/' {
					j += 2
					inBlockComment = false
					continue
				}
				j++
				continue
			}

			ch := line[j]

			// Skip line comments (rest of line)
			if j+1 < len(line) && line[j] == '/' && line[j+1] == '/' {
				break
			}

			// Start block comment
			if j+1 < len(line) && line[j] == '/' && line[j+1] == '*' {
				inBlockComment = true
				j += 2
				continue
			}

			// Skip string literals
			if ch == '"' || ch == '\'' {
				quote := ch
				j++
				for j < maxCol {
					if line[j] == '\\' && j+1 < len(line) {
						j += 2 // skip escape sequence
						continue
					}
					if line[j] == quote {
						j++
						break
					}
					j++
				}
				continue
			}

			// Count braces
			switch ch {
			case '{':
				braceDepth++
			case '}':
				braceDepth--
			}
			j++
		}
	}

	return braceDepth > 0
}

// hasNetPositiveDepthUpToColumn checks if there are more opening braces than closing braces
// up to the cursor position on a single line, starting from depth 0.
// startInBlockComment indicates whether the line starts inside a multi-line block comment.
func (s *Server) hasNetPositiveDepthUpToColumn(line string, cursorCol int, startInBlockComment bool) bool {
	return s.depthAtColumn(line, cursorCol, 0, startInBlockComment) > 0
}

// depthAtColumn computes brace depth at a given column position, starting from startDepth.
// This properly skips braces in comments and string literals.
// startInBlockComment indicates whether the line starts inside a multi-line block comment.
func (s *Server) depthAtColumn(line string, cursorCol, startDepth int, startInBlockComment bool) int {
	depth := startDepth
	inBlockComment := startInBlockComment
	maxCol := min(cursorCol, len(line))

	j := 0
	for j < maxCol {
		// Handle block comment continuation from previous line
		if inBlockComment {
			if j+1 < len(line) && line[j] == '*' && line[j+1] == '/' {
				j += 2
				inBlockComment = false
				continue
			}
			j++
			continue
		}

		ch := line[j]

		// Skip line comments (rest of line)
		if j+1 < len(line) && line[j] == '/' && line[j+1] == '/' {
			break
		}

		// Start block comment
		if j+1 < len(line) && line[j] == '/' && line[j+1] == '*' {
			inBlockComment = true
			j += 2
			continue
		}

		// Skip string literals
		if ch == '"' || ch == '\'' {
			quote := ch
			j++
			for j < maxCol {
				if line[j] == '\\' && j+1 < len(line) {
					j += 2 // skip escape sequence
					continue
				}
				if line[j] == quote {
					j++
					break
				}
				j++
			}
			continue
		}

		// Count braces
		switch ch {
		case '{':
			depth++
		case '}':
			depth--
		}
		j++
	}

	return depth
}

// isIdentifier checks if a string is a valid identifier.
func isIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if !isLetter(r) && r != '_' {
				return false
			}
		} else {
			if !isLetter(r) && !isDigit(r) && r != '_' {
				return false
			}
		}
	}
	return true
}

func isLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func isDigit(r rune) bool {
	return r >= '0' && r <= '9'
}

// computeByteOffsetFromText computes a byte offset within a line from document text.
// This is used when no source registry is available (before first analysis).
// It respects the negotiated position encoding (UTF-16 or UTF-8).
func (s *Server) computeByteOffsetFromText(text string, lspLine, lspChar int) int {
	lines := strings.Split(text, "\n")
	if lspLine >= len(lines) {
		return lspChar // fallback
	}

	line := lines[lspLine]
	content := []byte(line)

	// Branch on position encoding
	enc := s.workspace.PositionEncoding()
	switch enc {
	case PositionEncodingUTF8:
		// UTF-8: character offset IS byte offset, clamp to line length
		if lspChar > len(content) {
			return len(content)
		}
		return lspChar
	default:
		// UTF-16 (default): convert from UTF-16 code units to bytes
		return utf16CharToByteOffset(content, 0, lspChar)
	}
}

// topLevelCompletions returns completions for top-level context.
func (s *Server) topLevelCompletions() []protocol.CompletionItem {
	items := []protocol.CompletionItem{
		s.keywordCompletion("schema", "schema \"${1:name}\"", "Schema declaration"),
		s.keywordCompletion("import", "import \"${1:./path}\"${2: as ${3:alias}}", "Import statement"),
		s.keywordCompletion("type", "type ${1:Name} {\n\t$0\n}", "Type declaration"),
		s.keywordCompletion("abstract type", "abstract type ${1:Name} {\n\t$0\n}", "Abstract type declaration"),
		s.keywordCompletion("part type", "part type ${1:Name} {\n\t$0\n}", "Part type declaration"),
	}

	// Add datatype completions (3.6)
	items = append(items, s.keywordCompletion("datatype", "type ${1:Name} = ${2|String,Integer,Float,Boolean,UUID,Date,Timestamp,Enum,Pattern|}", "Datatype alias"))
	items = append(items, s.keywordCompletion("datatype with constraint", "type ${1:Name} = ${2|String,Integer|}[${3:min}, ${4:max}]", "Datatype alias with numeric constraints"))

	return items
}

// typeBodyCompletions returns completions for inside a type body.
func (s *Server) typeBodyCompletions() []protocol.CompletionItem {
	items := []protocol.CompletionItem{
		// Property snippets - modifiers are space-separated per grammar (only 'primary' or 'required')
		// Format: ${N|, modifier1, modifier2|} - empty first option, space-prefixed subsequent
		s.snippetCompletion("property", "${1:name} ${2|String,Integer,Float,Boolean,UUID|}${3|, required, primary|}", "Property declaration"),
		s.snippetCompletion("property with constraint", "${1:name} ${2:String}[${3:1}, ${4:100}]${5|, required|}", "Property with constraint"),

		// Relation snippets
		s.snippetCompletion("association", "--> ${1:NAME} (${2|one,many|}) ${3:TargetType}", "Association declaration"),
		s.snippetCompletion("composition", "*-> ${1:CONTAINS} (${2|one,many|}) ${3:PartType}", "Composition declaration"),

		// Invariant snippet
		s.snippetCompletion("invariant", "! \"${1:message}\" ${2:expression}", "Invariant declaration"),
	}

	// Add built-in types for quick access
	for _, t := range builtinTypes {
		sortText := "2_" + t
		kind := protocol.CompletionItemKindTypeParameter
		items = append(items, protocol.CompletionItem{
			Label:    t,
			Kind:     &kind,
			SortText: &sortText,
			Detail:   new("Built-in type"),
		})
	}

	return items
}

// typeCompletions returns type name completions.
// sourceID should be the canonical (symlink-resolved) SourceID from the document.
func (s *Server) typeCompletions(snapshot *Snapshot, sourceID location.SourceID) []protocol.CompletionItem {
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
			Detail:   new("Type"),
		})
	}

	// Add imported types with qualifier
	idx := snapshot.SymbolIndexAt(sourceID)
	if idx != nil {
		for i := range idx.Symbols {
			sym := &idx.Symbols[i]
			if sym.Kind == SymbolImport {
				imp, ok := sym.Data.(*schema.Import)
				if !ok || imp.Schema() == nil {
					continue
				}

				alias := imp.Alias()
				for typeName := range imp.Schema().Types() {
					qualifiedName := alias + "." + typeName
					sortText := "1_" + qualifiedName // Imported types second
					kind := protocol.CompletionItemKindClass
					items = append(items, protocol.CompletionItem{
						Label:    qualifiedName,
						Kind:     &kind,
						SortText: &sortText,
						Detail:   new("Imported type from " + alias),
					})
				}
			}
		}
	}

	return items
}

// propertyTypeCompletions returns completions for property type position.
// sourceID should be the canonical (symlink-resolved) SourceID from the document.
func (s *Server) propertyTypeCompletions(snapshot *Snapshot, sourceID location.SourceID) []protocol.CompletionItem {
	var items []protocol.CompletionItem

	// Add built-in types first
	for _, t := range builtinTypes {
		sortText := "0_" + t
		kind := protocol.CompletionItemKindTypeParameter
		items = append(items, protocol.CompletionItem{
			Label:    t,
			Kind:     &kind,
			SortText: &sortText,
			Detail:   new("Built-in type"),
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
			Detail:   new("Datatype"),
		})
	}

	// Add imported datatypes with qualifier
	idx := snapshot.SymbolIndexAt(sourceID)
	if idx != nil {
		for i := range idx.Symbols {
			sym := &idx.Symbols[i]
			if sym.Kind == SymbolImport {
				imp, ok := sym.Data.(*schema.Import)
				if !ok || imp.Schema() == nil {
					continue
				}

				alias := imp.Alias()
				for dtName := range imp.Schema().DataTypes() {
					qualifiedName := alias + "." + dtName
					sortText := "2_" + qualifiedName
					kind := protocol.CompletionItemKindTypeParameter
					items = append(items, protocol.CompletionItem{
						Label:    qualifiedName,
						Kind:     &kind,
						SortText: &sortText,
						Detail:   new("Imported datatype from " + alias),
					})
				}
			}
		}
	}

	return items
}

// importCompletions returns completions for import context.
func (s *Server) importCompletions() []protocol.CompletionItem {
	// Use optional alias form: ${2: as ${3:alias}} makes the " as alias" part optional.
	// This matches the grammar (alias is optional) and topLevelCompletions().
	return []protocol.CompletionItem{
		s.snippetCompletion("import", "import \"${1:./path}\"${2: as ${3:alias}}", "Import statement"),
	}
}

// keywordCompletion creates a keyword completion item.
func (s *Server) keywordCompletion(label, insertText, detail string) protocol.CompletionItem {
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

// snippetCompletion creates a snippet completion item.
func (s *Server) snippetCompletion(label, insertText, detail string) protocol.CompletionItem {
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
