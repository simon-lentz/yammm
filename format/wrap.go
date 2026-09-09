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
	return joinLines(wrapLongLines(classifyLexed(text)))
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
		// Category 1: Existing multiline Enum constructs
		if isMultilineEnumStart(ln) {
			collected, nextIdx := collectMultilineConstruct(ls, i)
			result = append(result, tryCollapseEnum(collected)...)
			i = nextIdx
			continue
		}

		// Category 1: Existing multiline extends constructs
		if isMultilineExtendsStart(ln) {
			collapsed, nextIdx := collapseMultilineExtends(ls, i)
			result = append(result, collapsed...)
			i = nextIdx
			continue
		}

		// Category 1: Existing multiline datatype alias Enum
		if isMultilineDatatypeAliasEnumStart(ln) {
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
		if DisplayWidth(ln.text) > LineWidthThreshold {
			if wrapped, ok := tryWrapSingleLineEnum(ln); ok {
				result = append(result, contentLines(wrapped)...)
			} else if wrapped, ok := tryWrapSingleLineExtends(ln); ok {
				result = append(result, contentLines(wrapped)...)
			} else if wrapped, ok := tryWrapDatatypeAliasEnum(ln); ok {
				result = append(result, contentLines(wrapped)...)
			} else if wrapped, ok := tryWrapInvariant(ln); ok {
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
func isMultilineEnumStart(ln line) bool {
	if !containsEnumBracket(ln) {
		return false
	}
	// Exclude datatype alias form: "type Name = Enum["
	if isDatatypeAliasEnumLine(ln) {
		return false
	}
	return hasUnbalancedBrackets(ln)
}

// isMultilineDatatypeAliasEnumStart checks if a line starts a multiline datatype alias Enum.
func isMultilineDatatypeAliasEnumStart(ln line) bool {
	if !isDatatypeAliasEnumLine(ln) {
		return false
	}
	return hasUnbalancedBrackets(ln)
}

// isDatatypeAliasEnumLine checks the code for the `type Name = Enum[` pattern.
func isDatatypeAliasEnumLine(ln line) bool {
	trimmed := strings.TrimSpace(ln.mask())
	if !strings.HasPrefix(trimmed, "type ") {
		return false
	}
	return strings.Contains(trimmed, " = Enum[")
}

// containsEnumBracket reports an Enum[ in the line's code, in type position.
// The mask is what keeps a literal's or a comment's "Enum[" from counting.
func containsEnumBracket(ln line) bool {
	return enumBracketIndex(ln) >= 0
}

// enumBracketIndex returns the byte offset of the code's Enum[, or -1.
func enumBracketIndex(ln line) int {
	m := ln.mask()
	idx := 0
	for {
		pos := strings.Index(m[idx:], "Enum[")
		if pos < 0 {
			return -1
		}
		absPos := idx + pos
		if absPos == 0 || m[absPos-1] == ' ' || m[absPos-1] == '\t' || m[absPos-1] == '=' {
			return absPos
		}
		idx = absPos + 5
		if idx >= len(m) {
			return -1
		}
	}
}

// hasUnbalancedBrackets reports more [ than ] in the line's code.
func hasUnbalancedBrackets(ln line) bool {
	return bracketDelta(ln) > 0
}

// extractEnumBracketContent finds the Enum[...] in a line and splits it into:
// beforeBracket (everything including "Enum["), content (inside brackets), afterBracket (after "]").
// Returns ok=false if the line doesn't contain a complete single-line Enum[...].
func extractEnumBracketContent(ln line) (beforeBracket, content, afterBracket string, ok bool) {
	enumStart := enumBracketIndex(ln)
	if enumStart < 0 {
		return "", "", "", false
	}
	bracketStart := enumStart + 5 // position after "Enum["

	m := ln.mask()
	depth := 1
	for i := bracketStart; i < len(m); i++ {
		switch m[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return ln.text[:bracketStart], ln.text[bracketStart:i], ln.text[i+1:], true
			}
		}
	}
	return "", "", "", false // unbalanced — multiline
}

// splitEnumValues splits bracket content at the commas that are code. masked is
// content with its literals blanked, so a comma inside a value is not a
// separator; the values themselves are sliced from content.
func splitEnumValues(content, masked string) []string {
	var values []string
	start := 0
	for i := 0; i < len(masked) && i < len(content); i++ {
		if masked[i] != ',' {
			continue
		}
		if val := strings.TrimSpace(content[start:i]); val != "" {
			values = append(values, val)
		}
		start = i + 1
	}
	if val := strings.TrimSpace(content[start:]); val != "" {
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
func tryWrapSingleLineEnum(ln line) ([]string, bool) {
	if !containsEnumBracket(ln) || isDatatypeAliasEnumLine(ln) {
		return nil, false
	}

	beforeBracket, content, afterBracket, ok := extractEnumBracketContent(ln)
	if !ok {
		return nil, false
	}

	values := splitEnumValues(content, ln.maskedSub(len(beforeBracket), len(beforeBracket)+len(content)))
	if len(values) == 0 {
		return nil, false
	}

	indent := extractIndent(ln.text)

	// A modifier on the closing "]" line is legal; an ANNOTATION there is not —
	// it parses and then fails to load, because it attaches to nothing. There is
	// no legal multiline placement for it, so the only correct answer is to
	// leave the line long. Declining costs a wide line; wrapping costs a schema
	// that no longer loads.
	if ln.hasAnnotationFrom(len(ln.text) - len(afterBracket)) {
		return nil, false
	}

	// The record says where the comment starts; afterBracket is the tail of the
	// line, so the offset rebases by the length of everything before it.
	comment := ""
	modifier := strings.TrimSpace(afterBracket)
	if idx := ln.lex.commentAt - (len(ln.text) - len(afterBracket)); ln.hasComment() && idx >= 0 && idx <= len(afterBracket) {
		comment = strings.TrimSpace(afterBracket[idx:])
		modifier = strings.TrimSpace(afterBracket[:idx])
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
	if !allContent(collectedLines) || anyTrailingComment(collectedLines) {
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

	// Parse closing line: may have "]" followed by modifier/comment. The
	// bracket is the one in the code — a "]" inside a value is data.
	lastLn := collectedLines[len(collectedLines)-1]
	trimmedLast := strings.TrimSpace(lastLine)
	closeAt := closingBracketIndex(lastLn)
	if strings.HasPrefix(trimmedLast, "]") {
		suffix = strings.TrimSpace(trimmedLast[1:])
	} else if beforeClose, afterClose, found := cutAt(lastLn.text, closeAt); found {
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

// anyTrailingComment reports whether any line in the construct carries a
// trailing comment. Folding such a construct onto one line would put the
// comment in front of everything that followed it.
func anyTrailingComment(ls []line) bool {
	for _, ln := range ls {
		if ln.hasComment() {
			return true
		}
	}
	return false
}

// closingBracketIndex returns the offset of the first "]" in the line's code.
func closingBracketIndex(ln line) int {
	return strings.IndexByte(ln.mask(), ']')
}

// cutAt splits text at i, reporting whether i is a position in it.
func cutAt(text string, i int) (before, after string, ok bool) {
	if i < 0 || i >= len(text) {
		return "", "", false
	}
	return text[:i], text[i+1:], true
}

// isMultilineExtendsStart checks if a line is an extends header without `{` on the same line.
func isMultilineExtendsStart(ln line) bool {
	trimmed := strings.TrimSpace(ln.mask())
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
func extractExtendsInfo(ln line) (indent, header string, types []string, ok bool) {
	indent = extractIndent(ln.text)
	trimmed := strings.TrimSpace(ln.text)

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
func tryWrapSingleLineExtends(ln line) ([]string, bool) {
	indent, header, types, ok := extractExtendsInfo(ln)
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
	first := ls[startIdx]
	firstLine := first.text
	indent := extractIndent(firstLine)
	// Read the header from the code: a trailing comment on an extends line is
	// not part of the parent list, and treating it as one folds both away.
	trimmed := strings.TrimSpace(first.mask()[:first.codeEnd()])

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
	sawComment := first.hasComment()
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
func tryWrapDatatypeAliasEnum(ln line) ([]string, bool) {
	if !isDatatypeAliasEnumLine(ln) {
		return nil, false
	}

	beforeBracket, content, afterBracket, ok := extractEnumBracketContent(ln)
	if !ok {
		return nil, false
	}

	values := splitEnumValues(content, ln.maskedSub(len(beforeBracket), len(beforeBracket)+len(content)))
	if len(values) == 0 {
		return nil, false
	}

	indent := extractIndent(ln.text)

	// No refusal is needed here: an annotated datatype alias is a syntax error
	// (measured at both @ and @@), and the formatter only ever sees input that
	// parsed, so the shape cannot reach this function.
	// Aliases carry no modifier, but may carry a comment.
	comment := ""
	rest := strings.TrimSpace(afterBracket)
	if idx := ln.lex.commentAt - (len(ln.text) - len(afterBracket)); ln.hasComment() && idx >= 0 && idx <= len(afterBracket) {
		comment = strings.TrimSpace(afterBracket[idx:])
		rest = strings.TrimSpace(afterBracket[:idx])
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
	msgEnd, ok := invariantMessageEnd(ls[idx])
	if !ok {
		return false
	}
	rel := msgEnd - (len(line) - len(strings.TrimLeft(line, "\t ")))
	if rel < 0 || rel > len(trimmed) {
		return false
	}

	afterMsg := strings.TrimSpace(trimmed[rel:])

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
func tryWrapInvariant(ln line) ([]string, bool) {
	msgEnd, ok := invariantMessageEnd(ln)
	if !ok {
		return nil, false
	}
	indent := extractIndent(ln.text)
	trimmed := strings.TrimSpace(ln.text)
	rel := msgEnd - len(indent)
	if rel < 0 || rel > len(trimmed) {
		return nil, false
	}

	afterMsg := strings.TrimSpace(trimmed[rel:])
	if afterMsg == "" {
		return nil, false // no expression — not wrappable
	}

	// The operators must be found in the expression's CODE: a && inside a
	// trailing comment is prose, and breaking there destroys the comment.
	exprStart := len(ln.text) - len(afterMsg) - (len(trimmed) - rel - len(afterMsg))
	ops := findTopLevelLogicalOps(afterMsg, ln.maskedSub(exprStart, exprStart+len(afterMsg)))
	if len(ops) == 0 {
		return nil, false // no operators -> leave as-is
	}

	return wrapInvariantAtOps(indent, indent+trimmed[:rel], afterMsg, ops), true
}

// invariantMessageEnd returns the offset just past an invariant's message
// literal. An invariant is "!" followed by a string, and the record says where
// that string ends whatever quote spells it.
func invariantMessageEnd(ln line) (int, bool) {
	m := ln.mask()
	trimmed := strings.TrimSpace(m)
	if !strings.HasPrefix(trimmed, "!") {
		return 0, false
	}
	bang := strings.IndexByte(m, '!')
	for _, lit := range ln.lex.literals {
		if lit.start > bang && strings.TrimSpace(m[bang+1:lit.start]) == "" {
			return lit.end, true
		}
	}
	return 0, false
}

// logicalOp records a top-level logical operator's position in an expression.
type logicalOp struct {
	offset int    // byte offset in expression
	length int    // length of operator ("&&" = 2, "||" = 2)
	op     string // "&&" or "||"
}

// findTopLevelLogicalOps finds the offsets of top-level && and || in expr.
// masked is expr with its literals and any comment blanked, so an operator
// inside either is not an operator; depth is counted on the mask for the same
// reason.
func findTopLevelLogicalOps(expr, masked string) []logicalOp {
	var ops []logicalOp
	parenDepth, braceDepth, bracketDepth := 0, 0, 0

	for i := 0; i+1 < len(masked) && i+1 < len(expr); i++ {
		switch masked[i] {
		case '(':
			parenDepth++
			continue
		case ')':
			parenDepth = max(parenDepth-1, 0)
			continue
		case '{':
			braceDepth++
			continue
		case '}':
			braceDepth = max(braceDepth-1, 0)
			continue
		case '[':
			bracketDepth++
			continue
		case ']':
			bracketDepth = max(bracketDepth-1, 0)
			continue
		}
		if parenDepth != 0 || braceDepth != 0 || bracketDepth != 0 {
			continue
		}
		if two := masked[i : i+2]; two == "&&" || two == "||" {
			ops = append(ops, logicalOp{offset: i, length: 2, op: expr[i : i+2]})
			i++
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
			depth += bracketDelta(ls[i])
		}
		i++
		if depth <= 0 {
			break
		}
	}
	return ls[startIdx:i], i
}
