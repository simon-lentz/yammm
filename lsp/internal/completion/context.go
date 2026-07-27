package completion

import (
	"strings"

	"github.com/simon-lentz/yammm/lsp/internal/docstate"
	"github.com/simon-lentz/yammm/schema"
)

// Context represents the context for completion.
type Context int

const (
	Unknown        Context = iota
	TopLevel               // Top-level (schema, type, import declarations)
	TypeBody               // Inside a type body
	Extends                // After "extends" keyword
	PropertyType           // At property type position
	RelationTarget         // After --> or *-> relation arrow
	ImportPath             // In an import statement's path portion
	AnnotationName         // After @ / @@ — an annotation name is being typed
	AnnotationArgs         // Inside a recognized annotation's argument list
)

// AnnotationContext is the parsed annotation head DetectContext recovers when it
// classifies an AnnotationName / AnnotationArgs context. It is threaded out to
// Complete so the head is parsed once per completion request instead of being
// re-parsed there; it is the zero value for every non-annotation context.
type AnnotationContext struct {
	placement schema.AnnotationPlacement
	name      string
}

// DetectContext analyzes text around cursor to determine context, returning the
// context and — for an annotation context — the parsed annotation head so the
// caller need not re-parse it. It accepts a documentSnapshot to leverage cached
// lineState for O(1) type body detection.
func DetectContext(doc *docstate.Snapshot, line, character int) (Context, AnnotationContext) {
	lines := strings.Split(doc.Text, "\n")
	if line < 0 || line >= len(lines) {
		return Unknown, AnnotationContext{}
	}

	currentLine := lines[line]

	// Get text before cursor on current line
	if character > len(currentLine) {
		character = len(currentLine)
	}
	beforeCursor := currentLine[:character]

	// Check for import path context (before property type to avoid "import " matching)
	if isImportContext(beforeCursor) {
		return ImportPath, AnnotationContext{}
	}

	// Check for extends context
	if isExtendsContext(beforeCursor, lines, line) {
		return Extends, AnnotationContext{}
	}

	// Check for relation target context (after --> or *->)
	if isRelationTargetContext(beforeCursor) {
		return RelationTarget, AnnotationContext{}
	}

	// Check for annotation contexts (after @ / @@, or inside a recognized
	// annotation's argument list). Before the property-type check because
	// @-prefixed text is not a bare identifier and would otherwise fall through
	// to the generic type-body member completions.
	if placement, name, inArgs, ok := annotationHead(beforeCursor, doc.LineStartsInBlockComment(line)); ok {
		ann := AnnotationContext{placement: placement, name: name}
		if inArgs {
			return AnnotationArgs, ann
		}
		return AnnotationName, ann
	}

	// Check for property type context (identifier followed by space)
	if isPropertyTypeContext(beforeCursor) {
		return PropertyType, AnnotationContext{}
	}

	// Check if we're inside a type body using cached lineState (O(1))
	// Falls back to direct computation if lineState is unavailable.
	// Pass character offset to handle cursor before closing brace on same line.
	if IsInsideTypeBody(doc, lines, line, character) {
		return TypeBody, AnnotationContext{}
	}

	// Default to top-level
	return TopLevel, AnnotationContext{}
}

// isExtendsContext checks if cursor is after "extends" keyword.
func isExtendsContext(beforeCursor string, lines []string, currentLine int) bool {
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
func isRelationTargetContext(beforeCursor string) bool {
	trimmed := strings.TrimSpace(beforeCursor)

	// Check for patterns like "--> NAME (one)" or "--> NAME (many)"
	// Also check for incomplete patterns
	if strings.Contains(trimmed, "-->") || strings.Contains(trimmed, "*->") {
		// Check if we're past the relation name and multiplicity
		if strings.Contains(trimmed, ")") {
			// After closing paren of multiplicity, we're at the target position.
			// Allow completion while the user is typing the target name (0 or 1 words after paren).
			afterParen := strings.TrimSpace(trimmed[strings.LastIndex(trimmed, ")")+1:])
			words := strings.Fields(afterParen)
			return len(words) <= 1
		}
	}

	return false
}

// isPropertyTypeContext checks if cursor is at property type position.
func isPropertyTypeContext(beforeCursor string) bool {
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

// annotationHead parses the annotation being typed from the text before the
// cursor: the sigil placement, the partial name, and whether the cursor is
// inside the argument list. startInBlockComment carries the block-comment state
// at the start of the line. ok is false when the cursor is not on an annotation
// head.
//
// It anchors on the LAST '@' that starts a sigil. Two conditions define that:
//
//   - It is not preceded by another '@', so the second '@' of a '@@' cannot
//     anchor and mis-report a type-level annotation as property-level.
//   - It is not inside a string literal or a comment, so an '@' in a value
//     (`Pattern[" @x"]`) or in commented-out text is ignored. This is the whole
//     of the protection: yammm's WS is on the hidden channel, so an annotation
//     needs no whitespace before it — `state String@index` is valid — and
//     requiring whitespace would silently withhold completions from it while
//     anchoring `@a@b` on the wrong sigil.
//
// An argument list containing spaces ("@vector( ") is still recognized, because
// the "cursor moved past the head" test only applies once the list is closed.
func annotationHead(beforeCursor string, startInBlockComment bool) (placement schema.AnnotationPlacement, name string, inArgs, ok bool) {
	at := -1
	for i := range len(beforeCursor) {
		if beforeCursor[i] == '@' &&
			(i == 0 || beforeCursor[i-1] != '@') &&
			!docstate.InStringOrComment(beforeCursor, i, startInBlockComment) {
			at = i
		}
	}
	if at < 0 {
		return 0, "", false, false
	}
	placement = schema.PlacementProperty
	body := beforeCursor[at+1:]
	if strings.HasPrefix(body, "@") {
		placement = schema.PlacementType
		body = body[1:]
	}
	// An open paren with no matching close means the cursor is inside the
	// argument list; the annotation name is the text before the paren.
	if open := strings.IndexByte(body, '('); open >= 0 {
		if strings.IndexByte(body[open:], ')') >= 0 {
			return 0, "", false, false // the argument list is already closed
		}
		return placement, body[:open], true, true
	}
	// Otherwise the name is the remaining run; whitespace after it means the
	// cursor has moved past the annotation head.
	if strings.ContainsAny(body, " \t") {
		return 0, "", false, false
	}
	return placement, body, false, true
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
func isImportContext(beforeCursor string) bool {
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

// IsInsideTypeBody checks if cursor is inside a type body using cached brace depth (O(1)).
//
// When lineState is available and valid, this uses the cached depth at the start of the
// current line, then scans up to the cursor position to compute the exact depth.
// Falls back to direct computation if lineState is unavailable or stale.
//
// The cursorCol parameter is the byte offset within the current line.
func IsInsideTypeBody(doc *docstate.Snapshot, lines []string, currentLine, cursorCol int) bool {
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
					return hasNetPositiveDepthUpToColumn(lines[currentLine], cursorCol, startInBlockComment)
				}
				return false
			}

			// Start depth > 0, so we're potentially inside.
			// Scan current line up to cursor to check if we're still inside.
			if currentLine < len(lines) {
				return depthAtColumn(lines[currentLine], cursorCol, startDepth, startInBlockComment) > 0
			}
			return startDepth > 0
		}
		// Line out of range - shouldn't happen but be defensive
		return false
	}

	// Fallback: compute directly (O(n) where n = currentLine)
	return isInsideTypeBodyDirect(lines, currentLine, cursorCol)
}

// isInsideTypeBodyDirect checks if cursor is inside a type body using token-aware brace counting.
//
// This heuristic counts '{' and '}' characters to determine nesting depth while properly
// skipping braces inside comments (// and /* */) and string literals (" and ').
// This prevents false positives from braces in comments like `// TODO: refactor { this }`.
//
// The cursorCol parameter is the byte offset within the current line.
func isInsideTypeBodyDirect(lines []string, currentLine, cursorCol int) bool {
	var bs docstate.BraceScanner
	for i := range min(currentLine+1, len(lines)) {
		maxCol := len(lines[i])
		if i == currentLine && cursorCol < maxCol {
			maxCol = cursorCol
		}
		bs.ScanLine(lines[i], maxCol)
	}
	return bs.Depth > 0
}

// hasNetPositiveDepthUpToColumn checks if there are more opening braces than closing braces
// up to the cursor position on a single line, starting from depth 0.
// startInBlockComment indicates whether the line starts inside a multi-line block comment.
func hasNetPositiveDepthUpToColumn(line string, cursorCol int, startInBlockComment bool) bool {
	return depthAtColumn(line, cursorCol, 0, startInBlockComment) > 0
}

// depthAtColumn computes brace depth at a given column position, starting from startDepth.
// This properly skips braces in comments and string literals.
// startInBlockComment indicates whether the line starts inside a multi-line block comment.
func depthAtColumn(line string, cursorCol, startDepth int, startInBlockComment bool) int {
	bs := docstate.BraceScanner{Depth: startDepth, InBlockComment: startInBlockComment}
	return bs.ScanLine(line, cursorCol)
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
