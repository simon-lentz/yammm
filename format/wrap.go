package format

import "strings"

const LineWidthThreshold = 100

// DisplayWidth counts the display width of a line: tabs count as 4 characters,
// all other runes count as 1.
//
// Note: CJK ideographs and most emoji actually occupy 2 terminal columns, but
// are counted as 1 here. This is acceptable because yammm schema identifiers
// are ASCII-only; CJK/emoji only appear in string literals and comments where
// precise column measurement is not critical for formatting decisions.
func DisplayWidth(line string) int {
	w := 0
	for _, r := range line {
		if r == '\t' {
			w += 4
		} else {
			w++
		}
	}
	return w
}

// WrapLongLines processes lines sequentially, wrapping long lines and collapsing
// multiline constructs that fit within the threshold. It handles Enum, extends,
// datatype alias Enum, and invariant constructs.
func WrapLongLines(text string) string {
	if text == "" {
		return ""
	}
	return joinLines(wrapLongLines(classifyText(text)))
}

// wrapLongLines is WrapLongLines over classified lines. Only a content line
// starts a construct or is wrapped; a blank or comment line passes through
// untouched, whatever its text looks like.
func wrapLongLines(ls []line) []line {
	result := make([]line, 0, len(ls))
	i := 0

	for i < len(ls) {
		ln := ls[i]
		if ln.class != lineContent {
			result = append(result, ln)
			i++
			continue
		}
		line := ln.text

		// Category 1: Existing multiline Enum constructs
		if isMultilineEnumStart(line) {
			collected, nextIdx := collectMultilineConstruct(ls, i)
			result = append(result, tryCollapseEnum(collected)...)
			i = nextIdx
			continue
		}

		// Category 1: Existing multiline extends constructs
		if isMultilineExtendsStart(line) {
			collapsed, nextIdx := collapseMultilineExtends(ls, i)
			result = append(result, collapsed...)
			i = nextIdx
			continue
		}

		// Category 1: Existing multiline datatype alias Enum
		if isMultilineDatatypeAliasEnumStart(line) {
			collected, nextIdx := collectMultilineConstruct(ls, i)
			result = append(result, tryCollapseDatatypeAliasEnum(collected)...)
			i = nextIdx
			continue
		}

		// Category 1: Existing multiline invariant — pass through unchanged
		if isMultilineInvariantStart(ls, i) {
			nextIdx := advancePastMultilineInvariant(ls, i)
			result = append(result, ls[i:nextIdx]...)
			i = nextIdx
			continue
		}

		// Category 2: Long single lines — try wrapping
		if DisplayWidth(line) > LineWidthThreshold {
			if wrapped, ok := tryWrapSingleLineEnum(line); ok {
				result = append(result, contentLines(wrapped)...)
			} else if wrapped, ok := tryWrapSingleLineExtends(line); ok {
				result = append(result, contentLines(wrapped)...)
			} else if wrapped, ok := tryWrapDatatypeAliasEnum(line); ok {
				result = append(result, contentLines(wrapped)...)
			} else if wrapped, ok := tryWrapInvariant(line); ok {
				result = append(result, contentLines(wrapped)...)
			} else {
				result = append(result, ln)
			}
			i++
			continue
		}

		// Category 3: Short lines — pass through unchanged
		result = append(result, ln)
		i++
	}

	return result
}

// allContent reports whether every line is a content line.
func allContent(ls []line) bool {
	for _, ln := range ls {
		if ln.class != lineContent {
			return false
		}
	}
	return true
}

// reindentConstruct re-emits a multiline bracket or extends construct that
// holds a comment line in its canonical shape: the first line as it is,
// interior lines one level deeper than it, and a closing "]" or "{" line
// at its indentation. Comment lines keep their text and take their place.
func reindentConstruct(ls []line) []line {
	indent := extractIndent(ls[0].text)
	out := make([]line, 0, len(ls))
	out = append(out, ls[0])
	for i, ln := range ls[1:] {
		if ln.class == lineBlank {
			out = append(out, ln)
			continue
		}
		trimmed := strings.TrimSpace(ln.text)
		last := i == len(ls)-2
		if last && (strings.HasPrefix(trimmed, "]") || trimmed == "{") {
			out = append(out, line{text: indent + trimmed, class: ln.class})
			continue
		}
		out = append(out, line{text: indent + "\t" + trimmed, class: ln.class})
	}
	return out
}

// contentLines classifies the lines a wrapper emitted in place of one
// declaration line.
func contentLines(texts []string) []line {
	out := make([]line, len(texts))
	for i, t := range texts {
		out[i] = contentLine(t)
	}
	return out
}

// isMultilineEnumStart checks if a line starts a multiline Enum (has `Enum[`
// with unbalanced brackets) but is NOT a datatype alias (no ` = Enum[`).
func isMultilineEnumStart(line string) bool {
	if !containsEnumBracket(line) {
		return false
	}
	// Exclude datatype alias form: "type Name = Enum["
	if isDatatypeAliasEnumLine(line) {
		return false
	}
	return hasUnbalancedBrackets(line)
}

// isMultilineDatatypeAliasEnumStart checks if a line starts a multiline datatype alias Enum.
func isMultilineDatatypeAliasEnumStart(line string) bool {
	if !isDatatypeAliasEnumLine(line) {
		return false
	}
	return hasUnbalancedBrackets(line)
}

// isDatatypeAliasEnumLine checks if a line matches the `type Name = Enum[` pattern.
func isDatatypeAliasEnumLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "type ") {
		return false
	}
	return strings.Contains(trimmed, " = Enum[")
}

// containsEnumBracket checks if a line contains `Enum[` preceded by a space
// (as a type keyword), not confused with Pattern content.
func containsEnumBracket(line string) bool {
	// Look for " Enum[" or "\tEnum[" or line starting with "Enum["
	idx := 0
	for {
		pos := strings.Index(line[idx:], "Enum[")
		if pos < 0 {
			return false
		}
		absPos := idx + pos
		// Must be preceded by space, tab, or be at start of trimmed content
		if absPos == 0 || line[absPos-1] == ' ' || line[absPos-1] == '\t' || line[absPos-1] == '=' {
			// Check it's not inside a string
			if !isInsideStringAt(line, absPos) {
				return true
			}
		}
		idx = absPos + 5
		if idx >= len(line) {
			return false
		}
	}
}

// hasUnbalancedBrackets checks if a line has more [ than ] (quote-aware).
func hasUnbalancedBrackets(line string) bool {
	depth := 0
	sc := newQuoteAwareScanner(line)
	for i, ch := sc.next(); i >= 0; i, ch = sc.next() {
		if ch == '[' {
			depth++
		}
		if ch == ']' {
			depth--
		}
	}
	return depth > 0
}

// extractEnumBracketContent finds the Enum[...] in a line and splits it into:
// beforeBracket (everything including "Enum["), content (inside brackets), afterBracket (after "]").
// Returns ok=false if the line doesn't contain a complete single-line Enum[...].
func extractEnumBracketContent(line string) (beforeBracket, content, afterBracket string, ok bool) {
	// Find "Enum[" that's not inside a string
	idx := 0
	enumStart := -1
	for {
		pos := strings.Index(line[idx:], "Enum[")
		if pos < 0 {
			return "", "", "", false
		}
		absPos := idx + pos
		if !isInsideStringAt(line, absPos) {
			enumStart = absPos
			break
		}
		idx = absPos + 5
		if idx >= len(line) {
			return "", "", "", false
		}
	}

	bracketStart := enumStart + 5 // position after "Enum["

	// Find matching close bracket (quote-aware)
	depth := 1
	sc := newQuoteAwareScannerFrom(line, bracketStart)
	for i, ch := sc.next(); i >= 0; i, ch = sc.next() {
		if ch == '[' {
			depth++
		}
		if ch == ']' {
			depth--
			if depth == 0 {
				return line[:bracketStart], line[bracketStart:i], line[i+1:], true
			}
		}
	}
	return "", "", "", false // unbalanced — multiline
}

// splitEnumValues is a quote-aware comma splitter for Enum bracket content.
func splitEnumValues(content string) []string {
	var values []string
	start := 0

	sc := newQuoteAwareScanner(content)
	for i, ch := sc.next(); i >= 0; i, ch = sc.next() {
		if ch == ',' {
			val := strings.TrimSpace(content[start:i])
			if val != "" {
				values = append(values, val)
			}
			start = i + 1
		}
	}
	// Handle last value (no trailing comma)
	val := strings.TrimSpace(content[start:])
	if val != "" {
		values = append(values, val)
	}
	return values
}

// buildWrappedEnum emits a multiline Enum.
// indent is the property's indent, prefix is everything up to and including "Enum[",
// values are the individual enum values, suffix is everything after "]" (modifier, comment).
func buildWrappedEnum(indent, prefix string, values []string, suffix string) []string {
	var out []string
	// First line: prefix (includes "Enum[")
	out = append(out, prefix)
	// Value lines: indented one deeper
	deeperIndent := indent + "\t"
	for _, v := range values {
		out = append(out, deeperIndent+v+",")
	}
	// Closing line: ] at property indent + suffix
	closingLine := indent + "]"
	suffix = strings.TrimSpace(suffix)
	if suffix != "" {
		closingLine += " " + suffix
	}
	out = append(out, closingLine)
	return out
}

// buildSingleLineEnum emits a single-line Enum. No trailing comma in single-line form.
func buildSingleLineEnum(prefix string, values []string, suffix string) string {
	line := prefix + strings.Join(values, ", ") + "]"
	suffix = strings.TrimSpace(suffix)
	if suffix != "" {
		line += " " + suffix
	}
	return line
}

// tryWrapSingleLineEnum attempts to wrap a long single-line Enum property.
func tryWrapSingleLineEnum(line string) ([]string, bool) {
	if !containsEnumBracket(line) || isDatatypeAliasEnumLine(line) {
		return nil, false
	}

	beforeBracket, content, afterBracket, ok := extractEnumBracketContent(line)
	if !ok {
		return nil, false
	}

	values := splitEnumValues(content)
	if len(values) == 0 {
		return nil, false
	}

	indent := extractIndent(line)

	// Split off inline comment from afterBracket
	comment := ""
	modifier := afterBracket
	if idx := findInlineComment(afterBracket); idx >= 0 {
		comment = strings.TrimSpace(afterBracket[idx:])
		modifier = strings.TrimSpace(afterBracket[:idx])
	} else {
		modifier = strings.TrimSpace(modifier)
	}

	suffix := modifier
	if comment != "" {
		if suffix != "" {
			suffix += " " + comment
		} else {
			suffix = comment
		}
	}

	return buildWrappedEnum(indent, beforeBracket, values, suffix), true
}

// tryCollapseEnum attempts to collapse a multiline Enum into a single line.
// If it doesn't fit, re-emit as canonical multiline. A construct holding a
// comment line is not a list of values: it keeps its lines, re-indented to
// the canonical multiline shape.
func tryCollapseEnum(collectedLines []line) []line {
	if len(collectedLines) < 2 {
		return collectedLines
	}
	if !allContent(collectedLines) {
		return reindentConstruct(collectedLines)
	}
	collected := textsOf(collectedLines)

	firstLine := collected[0]
	indent := extractIndent(firstLine)

	// Find Enum[ in first line
	enumIdx := strings.Index(firstLine, "Enum[")
	if enumIdx < 0 {
		return collectedLines
	}
	prefix := firstLine[:enumIdx+5] // up to and including "Enum["

	// Extract all values from the value lines (lines between first and last)
	// and the suffix from the closing line
	var values []string
	suffix := ""
	lastLine := collected[len(collected)-1]

	for _, vline := range collected[1 : len(collected)-1] {
		trimmed := strings.TrimSpace(vline)
		// Strip trailing comma
		trimmed = strings.TrimRight(trimmed, ",")
		trimmed = strings.TrimSpace(trimmed)
		if trimmed != "" {
			values = append(values, trimmed)
		}
	}

	// Parse closing line: may have "]" followed by modifier/comment
	trimmedLast := strings.TrimSpace(lastLine)
	if strings.HasPrefix(trimmedLast, "]") {
		suffix = strings.TrimSpace(trimmedLast[1:])
	} else if beforeClose, afterClose, found := strings.Cut(trimmedLast, "]"); found {
		// Closing line might have values before ]
		val := strings.TrimSpace(strings.TrimRight(strings.TrimSpace(beforeClose), ","))
		if val != "" {
			values = append(values, val)
		}
		suffix = strings.TrimSpace(afterClose)
	}

	if len(values) == 0 {
		return collectedLines
	}

	// Try single-line form
	singleLine := buildSingleLineEnum(prefix, values, suffix)
	if DisplayWidth(singleLine) <= LineWidthThreshold {
		return []line{contentLine(singleLine)}
	}

	// Re-emit canonical multiline
	return contentLines(buildWrappedEnum(indent, prefix, values, suffix))
}

// isMultilineExtendsStart checks if a line is an extends header without `{` on the same line.
func isMultilineExtendsStart(line string) bool {
	trimmed := strings.TrimSpace(line)
	// Match: (abstract |part )?type \w+ extends with no { on the line
	rest := trimmed
	// Strip optional abstract/part prefix
	rest = strings.TrimPrefix(rest, "abstract ")
	rest = strings.TrimPrefix(rest, "part ")
	if !strings.HasPrefix(rest, "type ") {
		return false
	}
	if !strings.Contains(rest, " extends") {
		return false
	}
	// Must NOT contain `{` — that's the multiline indicator
	if strings.Contains(trimmed, "{") {
		return false
	}
	// Must end with either the extends keyword or a type list (no `{`)
	return true
}

// extractExtendsInfo parses a single-line extends declaration into components.
// Returns indent, header ("type Name extends"), types list, and ok.
func extractExtendsInfo(line string) (indent, header string, types []string, ok bool) {
	indent = extractIndent(line)
	trimmed := strings.TrimSpace(line)

	// Strip trailing " {"
	if !strings.HasSuffix(trimmed, " {") && !strings.HasSuffix(trimmed, "{") {
		return "", "", nil, false
	}
	withoutBrace := strings.TrimSuffix(trimmed, "{")
	withoutBrace = strings.TrimRight(withoutBrace, " ")

	// Find "extends " keyword
	extendsIdx := strings.Index(withoutBrace, " extends ")
	if extendsIdx < 0 {
		return "", "", nil, false
	}

	header = withoutBrace[:extendsIdx+len(" extends")]
	typeList := strings.TrimSpace(withoutBrace[extendsIdx+len(" extends "):])

	// Split types on ","
	for t := range strings.SplitSeq(typeList, ",") {
		t = strings.TrimSpace(t)
		if t != "" {
			types = append(types, t)
		}
	}

	if len(types) < 2 {
		return "", "", nil, false
	}

	return indent, header, types, true
}

// tryWrapSingleLineExtends attempts to wrap a long single-line extends declaration.
func tryWrapSingleLineExtends(line string) ([]string, bool) {
	indent, header, types, ok := extractExtendsInfo(line)
	if !ok {
		return nil, false
	}

	return buildWrappedExtends(indent, header, types), true
}

// buildWrappedExtends emits a multiline extends declaration.
func buildWrappedExtends(indent, header string, types []string) []string {
	var out []string
	out = append(out, indent+header)
	deeperIndent := indent + "\t"
	for _, t := range types {
		out = append(out, deeperIndent+t+",")
	}
	out = append(out, indent+"{")
	return out
}

// buildSingleLineExtends emits a single-line extends declaration.
func buildSingleLineExtends(indent, header string, types []string) string {
	return indent + header + " " + strings.Join(types, ", ") + " {"
}

// collapseMultilineExtends collects a multiline extends and attempts to collapse it.
func collapseMultilineExtends(ls []line, startIdx int) ([]line, int) {
	firstLine := ls[startIdx].text
	indent := extractIndent(firstLine)
	trimmed := strings.TrimSpace(firstLine)

	extendsIdx := strings.Index(trimmed, " extends")
	if extendsIdx < 0 {
		return ls[startIdx : startIdx+1], startIdx + 1
	}
	header := trimmed[:extendsIdx+len(" extends")]

	// Types may be partially on the first line after "extends"
	afterExtends := strings.TrimSpace(trimmed[extendsIdx+len(" extends"):])
	var types []string
	if afterExtends != "" {
		for t := range strings.SplitSeq(afterExtends, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				types = append(types, t)
			}
		}
	}

	// Collect subsequent lines until we find `{`. A comment line inside the
	// list is not a type name: the construct is re-indented, not collapsed.
	i := startIdx + 1
	braceFound := false
	sawComment := false
	for i < len(ls) {
		if ls[i].class != lineContent {
			sawComment = true
			i++
			continue
		}
		lt := strings.TrimSpace(ls[i].text)
		if lt == "{" {
			braceFound = true
			i++
			break
		}
		// Line might be a type name with trailing comma, or type + "{"
		if typePart, hasBrace := strings.CutSuffix(lt, "{"); hasBrace {
			typePart = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(typePart), ","))
			if typePart != "" {
				types = append(types, typePart)
			}
			braceFound = true
			i++
			break
		}
		// Regular type name line
		typeName := strings.TrimRight(lt, ",")
		typeName = strings.TrimSpace(typeName)
		if typeName != "" {
			types = append(types, typeName)
		}
		i++
	}

	if !braceFound || len(types) == 0 {
		// Can't parse, pass through
		return ls[startIdx:i], i
	}
	if sawComment {
		return reindentConstruct(ls[startIdx:i]), i
	}

	// Try single-line form
	singleLine := buildSingleLineExtends(indent, header, types)
	if DisplayWidth(singleLine) <= LineWidthThreshold {
		return []line{contentLine(singleLine)}, i
	}

	// Re-emit canonical multiline
	return contentLines(buildWrappedExtends(indent, header, types)), i
}

// tryWrapDatatypeAliasEnum attempts to wrap a long single-line datatype alias Enum.
func tryWrapDatatypeAliasEnum(line string) ([]string, bool) {
	if !isDatatypeAliasEnumLine(line) {
		return nil, false
	}

	beforeBracket, content, afterBracket, ok := extractEnumBracketContent(line)
	if !ok {
		return nil, false
	}

	values := splitEnumValues(content)
	if len(values) == 0 {
		return nil, false
	}

	indent := extractIndent(line)

	// Aliases don't have modifiers, but may have inline comments
	comment := ""
	rest := afterBracket
	if idx := findInlineComment(rest); idx >= 0 {
		comment = strings.TrimSpace(rest[idx:])
		rest = strings.TrimSpace(rest[:idx])
	} else {
		rest = strings.TrimSpace(rest)
	}

	suffix := rest
	if comment != "" {
		if suffix != "" {
			suffix += " " + comment
		} else {
			suffix = comment
		}
	}

	return buildWrappedEnum(indent, beforeBracket, values, suffix), true
}

// tryCollapseDatatypeAliasEnum attempts to collapse a multiline datatype alias Enum.
func tryCollapseDatatypeAliasEnum(collected []line) []line {
	// Reuse the same logic as regular Enum collapsing
	return tryCollapseEnum(collected)
}

// isMultilineInvariantStart checks if an invariant continues on the next line.
// This detects two cases:
// 1. Line is `! "message"` with nothing after (expression on next line)
// 2. Line is `! "message" expr_start` and next line is a continuation
func isMultilineInvariantStart(ls []line, idx int) bool {
	if idx >= len(ls) {
		return false
	}
	line := ls[idx].text
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "! \"") {
		return false
	}

	// Find the end of the message string
	msgEnd := findEndOfInvariantMessage(trimmed)
	if msgEnd < 0 {
		return false
	}

	afterMsg := strings.TrimSpace(trimmed[msgEnd:])

	// Case 1: nothing after message (expression entirely on next lines)
	if afterMsg == "" {
		return idx+1 < len(ls)
	}

	// Case 2: Check if the next line is a content continuation (indented
	// deeper, not a new declaration)
	if idx+1 >= len(ls) || ls[idx+1].class != lineContent {
		return false
	}

	currentIndent := len(extractIndent(line))
	nextLine := ls[idx+1].text
	nextTrimmed := strings.TrimSpace(nextLine)
	nextIndent := len(extractIndent(nextLine))

	if nextIndent <= currentIndent {
		return false
	}

	return !isNewDeclarationStart(nextTrimmed)
}

// findEndOfInvariantMessage finds the byte position after the closing `"` of the
// invariant message string. Returns -1 if not found.
func findEndOfInvariantMessage(trimmed string) int {
	// trimmed starts with `! "` — scan from offset 2 to skip the `! ` prefix.
	// The scanner will enter the string at the opening `"` and skip through
	// to the closing `"`. The first yielded index is the position right after
	// the closing quote.
	sc := newQuoteAwareScannerFrom(trimmed, 2)
	if i, _ := sc.next(); i >= 0 {
		return i
	}
	// The scanner exhausted without yielding a non-string character.
	// If we entered and exited a string, pos is right after the closing quote.
	pos, inStr := sc.Done()
	if !inStr && pos > 2 {
		return pos
	}
	return -1
}

// declarationPrefixes lists the prefixes that indicate the start of a new
// declaration. Hoisted to package level to avoid per-call slice allocation.
var declarationPrefixes = []string{
	"type ", "abstract ", "part ", "schema ", "import ",
	"! ", "-->", "*->", "}", "@@", "@",
}

// isNewDeclarationStart checks if a trimmed line starts a new declaration.
func isNewDeclarationStart(trimmed string) bool {
	for _, p := range declarationPrefixes {
		if strings.HasPrefix(trimmed, p) {
			return true
		}
	}
	// Also check for property lines (lowercase identifier followed by uppercase type)
	if len(trimmed) > 0 && (trimmed[0] >= 'a' && trimmed[0] <= 'z' || trimmed[0] == '_') {
		spaceIdx := strings.IndexByte(trimmed, ' ')
		if spaceIdx > 0 && spaceIdx+1 < len(trimmed) {
			afterSpace := trimmed[spaceIdx+1:]
			if len(afterSpace) > 0 && afterSpace[0] >= 'A' && afterSpace[0] <= 'Z' {
				return true
			}
		}
	}
	return false
}

// advancePastMultilineInvariant returns the index after the last continuation line
// of a multiline invariant starting at idx.
func advancePastMultilineInvariant(ls []line, idx int) int {
	if idx >= len(ls) {
		return idx
	}
	currentIndent := len(extractIndent(ls[idx].text))
	i := idx + 1
	for i < len(ls) {
		if ls[i].class != lineContent {
			break
		}
		trimmed := strings.TrimSpace(ls[i].text)
		lineIndent := len(extractIndent(ls[i].text))
		if lineIndent <= currentIndent {
			break
		}
		if isNewDeclarationStart(trimmed) {
			break
		}
		i++
	}
	return i
}

// tryWrapInvariant attempts to wrap a long single-line invariant at top-level logical operators.
func tryWrapInvariant(line string) ([]string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "! \"") {
		return nil, false
	}

	msgEnd := findEndOfInvariantMessage(trimmed)
	if msgEnd < 0 {
		return nil, false
	}

	afterMsg := strings.TrimSpace(trimmed[msgEnd:])
	if afterMsg == "" {
		return nil, false // no expression — not wrappable
	}

	// Find top-level logical operators in the expression
	ops := findTopLevelLogicalOps(afterMsg)
	if len(ops) == 0 {
		return nil, false // no operators -> leave as-is
	}

	indent := extractIndent(line)

	// Build the prefix: indent + `! "message"`
	prefix := indent + trimmed[:msgEnd]

	// Wrap expression at operator positions
	return wrapInvariantAtOps(indent, prefix, afterMsg, ops), true
}

// logicalOp records a top-level logical operator's position in an expression.
type logicalOp struct {
	offset int    // byte offset in expression
	length int    // length of operator ("&&" = 2, "||" = 2)
	op     string // "&&" or "||"
}

// findTopLevelLogicalOps finds byte offsets of top-level `&&` and `||` in an expression.
// Respects string literals, regex literals, parentheses, braces, and brackets.
// The quoteAwareScanner handles string tracking; regex, parens, braces, and
// brackets are tracked as additional state in this function.
func findTopLevelLogicalOps(expr string) []logicalOp {
	var ops []logicalOp
	inRegex := false
	parenDepth := 0
	braceDepth := 0
	bracketDepth := 0

	sc := newQuoteAwareScanner(expr)
	for i, ch := sc.next(); i >= 0; i, ch = sc.next() {
		if inRegex {
			if ch == '\\' && i+1 < len(expr) {
				sc.pos++ // skip escaped char inside regex
				continue
			}
			if ch == '/' {
				inRegex = false
			}
			continue
		}
		if ch == '/' {
			inRegex = true
			continue
		}
		if ch == '(' {
			parenDepth++
			continue
		}
		if ch == ')' {
			if parenDepth > 0 {
				parenDepth--
			}
			continue
		}
		if ch == '{' {
			braceDepth++
			continue
		}
		if ch == '}' {
			if braceDepth > 0 {
				braceDepth--
			}
			continue
		}
		if ch == '[' {
			bracketDepth++
			continue
		}
		if ch == ']' {
			if bracketDepth > 0 {
				bracketDepth--
			}
			continue
		}

		if parenDepth == 0 && braceDepth == 0 && bracketDepth == 0 && !inRegex && i+1 < len(expr) {
			two := expr[i : i+2]
			if two == "&&" || two == "||" {
				ops = append(ops, logicalOp{offset: i, length: 2, op: two})
				sc.pos++ // skip second char of operator
			}
		}
	}
	return ops
}

// wrapInvariantAtOps splits an invariant expression at operator positions.
// The operator stays at the END of the line (break AFTER operator).
func wrapInvariantAtOps(indent, prefix, expr string, ops []logicalOp) []string {
	var out []string
	// First line: just the prefix (! "message")
	out = append(out, prefix)

	deeperIndent := indent + "\t"
	prevEnd := 0

	for _, op := range ops {
		segEnd := op.offset + op.length
		segment := strings.TrimSpace(expr[prevEnd:segEnd])
		out = append(out, deeperIndent+segment)
		prevEnd = segEnd
	}

	// Emit the remainder after the last operator
	if prevEnd < len(expr) {
		remainder := strings.TrimSpace(expr[prevEnd:])
		if remainder != "" {
			out = append(out, indent+"\t"+remainder)
		}
	}

	return out
}

// extractIndent returns the leading whitespace of a line.
func extractIndent(line string) string {
	trimmed := strings.TrimLeft(line, "\t ")
	return line[:len(line)-len(trimmed)]
}

// collectMultilineConstruct gathers lines starting at startIdx until bracket depth
// returns to 0. Only content lines move the depth. Returns the collected lines
// and the next index to process.
func collectMultilineConstruct(ls []line, startIdx int) ([]line, int) {
	depth := 0
	i := startIdx

	for i < len(ls) {
		if ls[i].class == lineContent {
			depth += bracketDelta(ls[i].text)
		}
		i++
		if depth <= 0 {
			break
		}
	}
	return ls[startIdx:i], i
}
