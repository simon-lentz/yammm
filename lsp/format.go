package lsp

import (
	"fmt"
	"sort"
	"strings"

	"github.com/antlr4-go/antlr/v4"

	"github.com/simon-lentz/yammm/internal/grammar"
)

type tokenRange struct {
	start int
	end   int
}

type parseErrorListener struct {
	*antlr.DefaultErrorListener
	errs []string
}

func (l *parseErrorListener) SyntaxError(
	_ antlr.Recognizer,
	_ any,
	line, column int,
	msg string,
	_ antlr.RecognitionException,
) {
	l.errs = append(l.errs, fmt.Sprintf("%d:%d %s", line, column, msg))
}

type spacingAction int

const (
	spacingNone spacingAction = iota
	spacingSpace
	spacingNewline
)

// formatTokenStream applies parse-tree-assisted token-stream formatting.
// Returns an error if lexing/parsing fails so callers can fall back.
func formatTokenStream(text string) (string, error) {
	normalized := strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(text)

	input := antlr.NewInputStream(normalized)
	lexer := grammar.NewYammmGrammarLexer(input)
	parseErrs := &parseErrorListener{DefaultErrorListener: &antlr.DefaultErrorListener{}}
	lexer.RemoveErrorListeners()
	lexer.AddErrorListener(parseErrs)

	stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	parser := grammar.NewYammmGrammarParser(stream)
	parser.RemoveErrorListeners()
	parser.AddErrorListener(parseErrs)

	tree := parser.Schema()
	if len(parseErrs.errs) > 0 {
		return "", fmt.Errorf("parse failed: %s", parseErrs.errs[0])
	}

	ranges := collectInvariantExpressionRanges(tree)
	stream.Fill()
	allTokens := stream.GetAllTokens()

	var out strings.Builder
	lineStart := true
	indentLevel := 0
	var prev antlr.Token
	prevInExpr := false
	pendingWS := ""

	for _, tok := range allTokens {
		if tok.GetTokenType() == antlr.TokenEOF {
			continue
		}

		idx := tok.GetTokenIndex()
		inExpr := tokenInRanges(idx, ranges)
		tt := tok.GetTokenType()

		if tt == grammar.YammmGrammarLexerWS {
			if inExpr {
				if pendingWS != "" {
					writeExprWhitespace(&out, pendingWS, &lineStart)
					pendingWS = ""
				}
				writeExprWhitespace(&out, tok.GetText(), &lineStart)
			} else {
				pendingWS += tok.GetText()
			}
			continue
		}

		if inExpr {
			if pendingWS != "" {
				if prev != nil && !prevInExpr {
					// Bridge from declaration context to expression context.
					// When there's a newline, this is a multiline invariant
					// whose continuation indentation was set by wrapInvariantAtOps.
					// Preserve the original whitespace so that the indent level
					// (which only tracks brace depth) doesn't flatten it.
					if strings.Contains(pendingWS, "\n") {
						writeExprWhitespace(&out, pendingWS, &lineStart)
					} else {
						sep := declarationSeparator(prev, tok, pendingWS, indentLevel, true)
						writeText(&out, sep, &lineStart)
					}
				} else {
					writeExprWhitespace(&out, pendingWS, &lineStart)
				}
				pendingWS = ""
			} else if prev != nil && !prevInExpr {
				sep := declarationSeparator(prev, tok, "", indentLevel, true)
				writeText(&out, sep, &lineStart)
			}

			writeTokenText(&out, tok, &lineStart)
			prev = tok
			prevInExpr = true
			continue
		}

		sep := declarationSeparator(prev, tok, pendingWS, indentLevel, false)
		pendingWS = ""
		writeText(&out, sep, &lineStart)
		writeTokenText(&out, tok, &lineStart)

		if tt == grammar.YammmGrammarLexerLBRACE {
			indentLevel++
		} else if tt == grammar.YammmGrammarLexerRBRACE && indentLevel > 0 {
			indentLevel--
		}

		prev = tok
		prevInExpr = false
	}

	if pendingWS != "" {
		writeText(&out, pendingWS, &lineStart)
	}

	return finalizeFormattedText(alignColumns(wrapLongLines(collapseBlankLines(out.String())))), nil
}

type invariantRangeCollector struct {
	*grammar.BaseYammmGrammarListener
	ranges []tokenRange
}

func (c *invariantRangeCollector) ExitInvariant(ctx *grammar.InvariantContext) {
	if ctx == nil || ctx.GetConstraint() == nil {
		return
	}

	startTok := ctx.GetConstraint().GetStart()
	endTok := ctx.GetConstraint().GetStop()
	if startTok == nil || endTok == nil {
		return
	}

	start := startTok.GetTokenIndex()
	end := endTok.GetTokenIndex()
	if start < 0 || end < start {
		return
	}
	c.ranges = append(c.ranges, tokenRange{start: start, end: end})
}

func collectInvariantExpressionRanges(tree antlr.ParseTree) []tokenRange {
	collector := &invariantRangeCollector{
		BaseYammmGrammarListener: &grammar.BaseYammmGrammarListener{},
	}
	antlr.ParseTreeWalkerDefault.Walk(collector, tree)
	if len(collector.ranges) == 0 {
		return nil
	}

	sort.Slice(collector.ranges, func(i, j int) bool {
		if collector.ranges[i].start == collector.ranges[j].start {
			return collector.ranges[i].end < collector.ranges[j].end
		}
		return collector.ranges[i].start < collector.ranges[j].start
	})

	merged := make([]tokenRange, 0, len(collector.ranges))
	current := collector.ranges[0]
	for _, r := range collector.ranges[1:] {
		if r.start <= current.end+1 {
			if r.end > current.end {
				current.end = r.end
			}
			continue
		}
		merged = append(merged, current)
		current = r
	}
	merged = append(merged, current)
	return merged
}

func tokenInRanges(idx int, ranges []tokenRange) bool {
	if idx < 0 || len(ranges) == 0 {
		return false
	}
	i := sort.Search(len(ranges), func(i int) bool {
		return ranges[i].end >= idx
	})
	if i >= len(ranges) {
		return false
	}
	return ranges[i].start <= idx && idx <= ranges[i].end
}

func declarationSeparator(
	prev antlr.Token,
	curr antlr.Token,
	pendingWS string,
	indentLevel int,
	currInExpr bool,
) string {
	newlineCount := strings.Count(pendingWS, "\n")
	currIndent := indentLevel
	if !currInExpr && curr.GetTokenType() == grammar.YammmGrammarLexerRBRACE && currIndent > 0 {
		currIndent--
	}

	if prev == nil {
		if newlineCount > 0 {
			return newlineSeparator(newlineCount, currIndent)
		}
		return ""
	}

	if isCommentToken(curr.GetTokenType()) {
		if newlineCount > 0 {
			return newlineSeparator(newlineCount, currIndent)
		}
		return " "
	}

	action := declarationSpacingAction(prev, curr)
	if action == spacingNewline {
		return newlineSeparator(max(1, newlineCount), currIndent)
	}
	if newlineCount > 0 {
		return newlineSeparator(newlineCount, currIndent)
	}
	if action == spacingNone {
		return ""
	}
	return " "
}

func declarationSpacingAction(prev antlr.Token, curr antlr.Token) spacingAction {
	if prev == nil {
		return spacingNone
	}

	prevType := prev.GetTokenType()
	currType := curr.GetTokenType()

	if currType == grammar.YammmGrammarLexerRBRACE {
		return spacingNewline
	}
	if prevType == grammar.YammmGrammarLexerLBRACE {
		return spacingNewline
	}
	if prevType == grammar.YammmGrammarLexerRBRACE {
		return spacingNewline
	}
	if prevType == grammar.YammmGrammarLexerDOC_COMMENT {
		return spacingNewline
	}

	// Closing delimiters win over broad left-side rules.
	if currType == grammar.YammmGrammarLexerRBRACK || currType == grammar.YammmGrammarLexerRPAR {
		return spacingNone
	}

	// Specific pair rules.
	if prevType == grammar.YammmGrammarLexerEXCLAMATION && currType == grammar.YammmGrammarLexerSTRING {
		return spacingSpace
	}
	if prevType == grammar.YammmGrammarLexerASSOC || prevType == grammar.YammmGrammarLexerCOMP {
		return spacingSpace
	}
	if currType == grammar.YammmGrammarLexerLBRACE {
		return spacingSpace
	}
	if prevType == grammar.YammmGrammarLexerSLASH || currType == grammar.YammmGrammarLexerSLASH {
		return spacingSpace
	}
	if prevType == grammar.YammmGrammarLexerEQUALS || currType == grammar.YammmGrammarLexerEQUALS {
		return spacingSpace
	}
	if prevType == grammar.YammmGrammarLexerPERIOD || currType == grammar.YammmGrammarLexerPERIOD {
		return spacingNone
	}
	if prevType == grammar.YammmGrammarLexerCOMMA {
		return spacingSpace
	}
	if currType == grammar.YammmGrammarLexerCOMMA {
		return spacingNone
	}
	if prevType == grammar.YammmGrammarLexerMINUS {
		return spacingNone
	}
	if prevType == grammar.YammmGrammarLexerLPAR || currType == grammar.YammmGrammarLexerRPAR {
		return spacingNone
	}
	if prevType == grammar.YammmGrammarLexerCOLON || currType == grammar.YammmGrammarLexerCOLON {
		return spacingNone
	}
	if prevType == grammar.YammmGrammarLexerLBRACK {
		return spacingNone
	}
	if currType == grammar.YammmGrammarLexerLBRACK && isConstraintBracketLeft(prev.GetText()) {
		return spacingNone
	}
	// List type angle brackets: collapse spacing around < and > only in
	// type contexts (e.g. List<String>), not in expression comparisons.
	if currType == grammar.YammmGrammarLexerLT && isListAngleBracketLeft(prev.GetText()) {
		return spacingNone
	}
	if prevType == grammar.YammmGrammarLexerLT {
		return spacingNone
	}
	if currType == grammar.YammmGrammarLexerGT {
		return spacingNone
	}
	if prevType == grammar.YammmGrammarLexerGT {
		if currType == grammar.YammmGrammarLexerLBRACK {
			return spacingNone
		}
		return spacingSpace
	}
	if isKeywordWithRequiredSpaceAfter(prev.GetText()) {
		return spacingSpace
	}

	return spacingSpace
}

func isKeywordWithRequiredSpaceAfter(text string) bool {
	switch text {
	case "type", "schema", "import", "as", "extends", "abstract", "part":
		return true
	default:
		return false
	}
}

func isConstraintBracketLeft(text string) bool {
	switch text {
	case "Integer", "Float", "String", "Enum", "Pattern", "Timestamp", "Vector", "List":
		return true
	default:
		return false
	}
}

// isListAngleBracketLeft returns true if the token text can precede a `<`
// in List type syntax. Currently only "List" itself uses angle brackets.
func isListAngleBracketLeft(text string) bool {
	return text == "List"
}

func isCommentToken(tokenType int) bool {
	return tokenType == grammar.YammmGrammarLexerSL_COMMENT || tokenType == grammar.YammmGrammarLexerDOC_COMMENT
}

func normalizeDocComment(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		trimmed := strings.TrimRight(line, " \t")
		lines[i] = normalizeIndentation(trimmed)
	}
	return strings.Join(lines, "\n")
}

func writeExprWhitespace(out *strings.Builder, ws string, lineStart *bool) {
	if ws == "" {
		return
	}

	var b strings.Builder
	i := 0
	for i < len(ws) {
		if ws[i] == '\n' {
			b.WriteByte('\n')
			*lineStart = true
			i++
			continue
		}

		j := i
		for j < len(ws) && ws[j] != '\n' {
			j++
		}
		seg := ws[i:j]
		if *lineStart {
			b.WriteString(normalizeIndentation(seg))
		} else {
			b.WriteString(seg)
		}
		i = j
	}

	writeText(out, b.String(), lineStart)
}

func writeTokenText(out *strings.Builder, tok antlr.Token, lineStart *bool) {
	text := tok.GetText()
	if tok.GetTokenType() == grammar.YammmGrammarLexerDOC_COMMENT {
		text = normalizeDocComment(text)
	}
	writeText(out, text, lineStart)
}

func writeText(out *strings.Builder, text string, lineStart *bool) {
	if text == "" {
		return
	}
	out.WriteString(text)
	*lineStart = updateLineStart(*lineStart, text)
}

func updateLineStart(lineStart bool, text string) bool {
	state := lineStart
	for i := range len(text) {
		switch text[i] {
		case '\n':
			state = true
		case ' ', '\t', '\r':
			// keep current state
		default:
			state = false
		}
	}
	return state
}

func newlineSeparator(count int, indentLevel int) string {
	if count <= 0 {
		count = 1
	}
	if indentLevel < 0 {
		indentLevel = 0
	}
	return strings.Repeat("\n", count) + strings.Repeat("\t", indentLevel)
}

// lineClass classifies a source line for blank-line collapsing.
type lineClass int

const (
	lineBlank   lineClass = iota // empty or whitespace-only
	lineComment                  // only contains // or part of /* */ block
	lineContent                  // declaration tokens (possibly with trailing comment)
)

// classifyLines classifies each line as blank, comment-only, or content.
// Tracks multiline /* ... */ blocks so interior lines are lineComment.
func classifyLines(lines []string) []lineClass {
	classes := make([]lineClass, len(lines))
	inDocComment := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		if inDocComment {
			classes[i] = lineComment
			if strings.Contains(trimmed, "*/") {
				inDocComment = false
			}
			continue
		}

		if trimmed == "" {
			classes[i] = lineBlank
			continue
		}

		if strings.HasPrefix(trimmed, "/*") {
			classes[i] = lineComment
			if !strings.Contains(trimmed, "*/") {
				inDocComment = true
			}
			continue
		}

		if strings.HasPrefix(trimmed, "//") {
			classes[i] = lineComment
			continue
		}

		classes[i] = lineContent
	}
	return classes
}

// collapseBlankLines enforces the Section 10 blank line rules on Phase 1 output.
// It operates in two passes: first collapse/remove excess blank lines, then
// ensure required blank lines after schema and import declarations.
func collapseBlankLines(text string) string {
	if text == "" {
		return ""
	}

	lines := strings.Split(text, "\n")
	classes := classifyLines(lines)

	// Pass 1: collapse/remove blank lines.
	var result []string
	var resultClasses []lineClass

	for i := range lines {
		cls := classes[i]

		if cls != lineBlank {
			result = append(result, lines[i])
			resultClasses = append(resultClasses, cls)
			continue
		}

		// Rule 1: No blank lines at start of file.
		if len(result) == 0 {
			continue
		}

		// Rule 2/3/4: Max 1 blank line (collapse consecutive blanks).
		if resultEndsWithBlank(resultClasses) {
			continue
		}

		// Rule 7/9: No blank lines after '{'.
		if prevNonBlankEndsWithBrace(result, resultClasses) {
			continue
		}

		// Rule 8/9: No blank lines before '}'.
		if nextNonBlankStartsWithCloseBrace(lines, classes, i) {
			continue
		}

		// Otherwise: emit one blank line.
		result = append(result, "")
		resultClasses = append(resultClasses, lineBlank)
	}

	// Pass 2: ensure required blank lines.
	result, resultClasses = ensureBlankAfterSchema(result, resultClasses)
	result, resultClasses = ensureBlankAfterLastImport(result, resultClasses)
	_ = resultClasses // silence unused after final assignment

	return strings.Join(result, "\n")
}

// resultEndsWithBlank returns true if the last entry in result classes is blank.
func resultEndsWithBlank(classes []lineClass) bool {
	return len(classes) > 0 && classes[len(classes)-1] == lineBlank
}

// prevNonBlankEndsWithBrace returns true if the previous non-blank result line
// ends with '{'.
func prevNonBlankEndsWithBrace(result []string, classes []lineClass) bool {
	for i := len(result) - 1; i >= 0; i-- {
		if classes[i] == lineBlank {
			continue
		}
		trimmed := strings.TrimSpace(result[i])
		return strings.HasSuffix(trimmed, "{")
	}
	return false
}

// nextNonBlankStartsWithCloseBrace returns true if the next non-blank line in
// the input starts with '}'.
func nextNonBlankStartsWithCloseBrace(lines []string, classes []lineClass, startIdx int) bool {
	for i := startIdx + 1; i < len(lines); i++ {
		if classes[i] == lineBlank {
			continue
		}
		trimmed := strings.TrimSpace(lines[i])
		return strings.HasPrefix(trimmed, "}")
	}
	return false
}

// ensureBlankAfterSchema inserts a blank line after 'schema "..."' if the next
// line is non-blank.
func ensureBlankAfterSchema(lines []string, classes []lineClass) ([]string, []lineClass) {
	for i := range lines {
		if classes[i] != lineContent {
			continue
		}
		trimmed := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(trimmed, "schema ") {
			continue
		}

		// Found schema line. Check if next line is non-blank.
		if i+1 < len(lines) && classes[i+1] != lineBlank {
			// Insert blank line after schema.
			newLines := make([]string, 0, len(lines)+1)
			newClasses := make([]lineClass, 0, len(classes)+1)
			newLines = append(newLines, lines[:i+1]...)
			newClasses = append(newClasses, classes[:i+1]...)
			newLines = append(newLines, "")
			newClasses = append(newClasses, lineBlank)
			newLines = append(newLines, lines[i+1:]...)
			newClasses = append(newClasses, classes[i+1:]...)
			return newLines, newClasses
		}
		break
	}
	return lines, classes
}

// ensureBlankAfterLastImport inserts a blank line after the last import
// declaration if the following line is non-blank.
func ensureBlankAfterLastImport(lines []string, classes []lineClass) ([]string, []lineClass) {
	lastImportIdx := -1
	for i := range lines {
		if classes[i] != lineContent {
			continue
		}
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "import ") {
			lastImportIdx = i
		}
	}

	if lastImportIdx < 0 {
		return lines, classes
	}

	// Check if next line after last import is non-blank.
	if lastImportIdx+1 < len(lines) && classes[lastImportIdx+1] != lineBlank {
		newLines := make([]string, 0, len(lines)+1)
		newClasses := make([]lineClass, 0, len(classes)+1)
		newLines = append(newLines, lines[:lastImportIdx+1]...)
		newClasses = append(newClasses, classes[:lastImportIdx+1]...)
		newLines = append(newLines, "")
		newClasses = append(newClasses, lineBlank)
		newLines = append(newLines, lines[lastImportIdx+1:]...)
		newClasses = append(newClasses, classes[lastImportIdx+1:]...)
		return newLines, newClasses
	}

	return lines, classes
}

// memberKind identifies the type of an alignable declaration member.
type memberKind int

const (
	memberProperty     memberKind = iota // field_name Type [modifier]
	memberRelationship                   // --> or *-> REL_NAME [(mult)] Target
	memberAlias                          // type Name = TypeExpr
)

// alignableLine holds the parsed structure of a single-line declaration for alignment.
type alignableLine struct {
	indent  string // leading whitespace (preserved as-is)
	kind    memberKind
	arrow   string // "-->" or "*->" for relationships, empty otherwise
	name    string // the column to be padded
	rest    string // everything after name
	comment string // inline // comment (includes "//"), empty if none
	raw     string // original line text
}

// alignColumns pads the name column within alignment groups to produce
// columnar output. Groups are contiguous runs of the same member kind,
// broken by blank lines, comment-only lines, non-alignable lines, or kind changes.
func alignColumns(text string) string {
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	result := make([]string, 0, len(lines))
	var group []alignableLine

	i := 0
	for i < len(lines) {
		line := lines[i]

		if isMultilineStart(line) {
			result = flushAlignGroup(result, group)
			group = nil
			result, i = emitMultilineConstruct(result, lines, i)
			continue
		}

		parsed, ok := parseAlignableLine(line)
		if !ok {
			result = flushAlignGroup(result, group)
			group = nil
			result = append(result, line)
			i++
			continue
		}

		if len(group) > 0 && group[0].kind != parsed.kind {
			result = flushAlignGroup(result, group)
			group = nil
		}

		group = append(group, parsed)
		i++
	}
	result = flushAlignGroup(result, group)
	return strings.Join(result, "\n")
}

// parseAlignableLine classifies a line and extracts the alignable name column.
func parseAlignableLine(line string) (alignableLine, bool) {
	trimmed := strings.TrimLeft(line, "\t ")
	indent := line[:len(line)-len(trimmed)]

	if trimmed == "" {
		return alignableLine{}, false
	}

	// Split off inline comment (bracket/quote-aware).
	content := trimmed
	comment := ""
	if idx := findInlineComment(trimmed); idx >= 0 {
		content = strings.TrimRight(trimmed[:idx], " ")
		comment = trimmed[idx:]
	}

	// Relationship: starts with --> or *->
	if strings.HasPrefix(content, "-->") || strings.HasPrefix(content, "*->") {
		arrow := content[:3]
		afterArrow := content[3:]
		if len(afterArrow) == 0 || afterArrow[0] != ' ' {
			return alignableLine{}, false
		}
		afterArrow = afterArrow[1:] // skip space
		spaceIdx := strings.IndexByte(afterArrow, ' ')
		if spaceIdx < 0 {
			return alignableLine{
				indent: indent, kind: memberRelationship,
				arrow: arrow, name: afterArrow, rest: "",
				comment: comment, raw: line,
			}, true
		}
		restStart := spaceIdx + 1
		for restStart < len(afterArrow) && afterArrow[restStart] == ' ' {
			restStart++
		}
		return alignableLine{
			indent: indent, kind: memberRelationship,
			arrow: arrow, name: afterArrow[:spaceIdx], rest: afterArrow[restStart:],
			comment: comment, raw: line,
		}, true
	}

	// Alias: starts with "type " and contains " = " without "{"
	if strings.HasPrefix(content, "type ") && strings.Contains(content, " = ") && !strings.Contains(content, "{") {
		afterType := content[5:] // skip "type "
		spaceIdx := strings.IndexByte(afterType, ' ')
		if spaceIdx < 0 {
			return alignableLine{}, false
		}
		restStart := spaceIdx + 1
		for restStart < len(afterType) && afterType[restStart] == ' ' {
			restStart++
		}
		return alignableLine{
			indent: indent, kind: memberAlias,
			name: afterType[:spaceIdx], rest: afterType[restStart:],
			comment: comment, raw: line,
		}, true
	}

	// Property: first word is lowercase identifier, second word starts with uppercase.
	if len(content) > 0 && (content[0] >= 'a' && content[0] <= 'z' || content[0] == '_') {
		spaceIdx := strings.IndexByte(content, ' ')
		if spaceIdx < 0 {
			return alignableLine{}, false
		}
		firstWord := content[:spaceIdx]
		switch firstWord {
		case "schema", "import", "type", "abstract", "part", "extends":
			return alignableLine{}, false
		}
		restStart := spaceIdx + 1
		for restStart < len(content) && content[restStart] == ' ' {
			restStart++
		}
		rest := content[restStart:]
		if len(rest) > 0 && rest[0] >= 'A' && rest[0] <= 'Z' {
			return alignableLine{
				indent: indent, kind: memberProperty,
				name: firstWord, rest: rest,
				comment: comment, raw: line,
			}, true
		}
	}

	return alignableLine{}, false
}

// findInlineComment returns the byte index of "//" outside brackets and quotes, or -1.
func findInlineComment(s string) int {
	bracketDepth := 0
	inString := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inString {
			if ch == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			continue
		}
		if ch == '[' {
			bracketDepth++
			continue
		}
		if ch == ']' {
			bracketDepth--
			continue
		}
		if ch == '/' && i+1 < len(s) && s[i+1] == '/' && bracketDepth == 0 {
			return i
		}
	}
	return -1
}

// flushAlignGroup pads names to a common width and rebuilds each line.
// Groups of 0 or 1 members pass through unchanged.
func flushAlignGroup(result []string, group []alignableLine) []string {
	if len(group) <= 1 {
		for _, al := range group {
			result = append(result, al.raw)
		}
		return result
	}

	maxNameWidth := 0
	for _, al := range group {
		if len(al.name) > maxNameWidth {
			maxNameWidth = len(al.name)
		}
	}

	type rebuiltLine struct {
		content string
		comment string
	}
	rebuilt := make([]rebuiltLine, len(group))
	hasComments := false
	maxContentWidth := 0

	for i, al := range group {
		var b strings.Builder
		b.WriteString(al.indent)

		switch al.kind {
		case memberProperty:
			b.WriteString(al.name)
			b.WriteString(strings.Repeat(" ", maxNameWidth-len(al.name)))
			b.WriteByte(' ')
			b.WriteString(al.rest)

		case memberRelationship:
			b.WriteString(al.arrow)
			b.WriteByte(' ')
			b.WriteString(al.name)
			b.WriteString(strings.Repeat(" ", maxNameWidth-len(al.name)))
			b.WriteByte(' ')
			b.WriteString(al.rest)

		case memberAlias:
			b.WriteString("type ")
			b.WriteString(al.name)
			b.WriteString(strings.Repeat(" ", maxNameWidth-len(al.name)))
			b.WriteByte(' ')
			b.WriteString(al.rest)
		}

		content := b.String()
		rebuilt[i] = rebuiltLine{content: content, comment: al.comment}
		if al.comment != "" {
			hasComments = true
		}
		if len(content) > maxContentWidth {
			maxContentWidth = len(content)
		}
	}

	for _, rl := range rebuilt {
		if hasComments && rl.comment != "" {
			padding := max(maxContentWidth-len(rl.content)+1, 1)
			result = append(result, rl.content+strings.Repeat(" ", padding)+rl.comment)
		} else {
			result = append(result, rl.content)
		}
	}

	return result
}

// isMultilineStart returns true if the line has unbalanced [ brackets.
func isMultilineStart(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") {
		return false
	}

	depth := 0
	inString := false
	for i := 0; i < len(trimmed); i++ {
		ch := trimmed[i]
		if inString {
			if ch == '\\' && i+1 < len(trimmed) {
				i++
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			continue
		}
		if ch == '[' {
			depth++
		}
		if ch == ']' {
			depth--
		}
	}
	return depth > 0
}

// emitMultilineConstruct emits lines until bracket depth returns to zero.
func emitMultilineConstruct(result, lines []string, startIdx int) ([]string, int) {
	depth := 0
	i := startIdx
	for i < len(lines) {
		result = append(result, lines[i])
		inString := false
		for j := 0; j < len(lines[i]); j++ {
			ch := lines[i][j]
			if inString {
				if ch == '\\' && j+1 < len(lines[i]) {
					j++
					continue
				}
				if ch == '"' {
					inString = false
				}
				continue
			}
			if ch == '"' {
				inString = true
				continue
			}
			if ch == '[' {
				depth++
			}
			if ch == ']' {
				depth--
			}
		}
		i++
		if depth <= 0 {
			break
		}
	}
	return result, i
}

func finalizeFormattedText(text string) string {
	lines := strings.Split(text, "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		result = append(result, strings.TrimRight(line, " \t"))
	}

	for len(result) > 0 && result[len(result)-1] == "" {
		result = result[:len(result)-1]
	}

	formatted := strings.Join(result, "\n")
	if formatted != "" && !strings.HasSuffix(formatted, "\n") {
		formatted += "\n"
	}
	return formatted
}

const lineWidthThreshold = 100

// displayWidth counts the display width of a line: tabs count as 4 characters,
// all other runes count as 1.
func displayWidth(line string) int {
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

// wrapLongLines processes lines sequentially, wrapping long lines and collapsing
// multiline constructs that fit within the threshold. It handles Enum, extends,
// datatype alias Enum, and invariant constructs.
func wrapLongLines(text string) string {
	if text == "" {
		return ""
	}

	lines := strings.Split(text, "\n")
	var result []string
	i := 0

	for i < len(lines) {
		line := lines[i]

		// Category 1: Existing multiline Enum constructs
		if isMultilineEnumStart(line) {
			collected, nextIdx := collectMultilineConstruct(lines, i)
			collapsed := tryCollapseEnum(collected)
			result = append(result, collapsed...)
			i = nextIdx
			continue
		}

		// Category 1: Existing multiline extends constructs
		if isMultilineExtendsStart(line) {
			collapsed, nextIdx := collapseMultilineExtends(lines, i)
			result = append(result, collapsed...)
			i = nextIdx
			continue
		}

		// Category 1: Existing multiline datatype alias Enum
		if isMultilineDatatypeAliasEnumStart(line) {
			collected, nextIdx := collectMultilineConstruct(lines, i)
			collapsed := tryCollapseDatatypeAliasEnum(collected)
			result = append(result, collapsed...)
			i = nextIdx
			continue
		}

		// Category 1: Existing multiline invariant — pass through unchanged
		if isMultilineInvariantStart(lines, i) {
			nextIdx := advancePastMultilineInvariant(lines, i)
			for j := i; j < nextIdx; j++ {
				result = append(result, lines[j])
			}
			i = nextIdx
			continue
		}

		// Category 2: Long single lines — try wrapping
		if displayWidth(line) > lineWidthThreshold {
			if wrapped, ok := tryWrapSingleLineEnum(line); ok {
				result = append(result, wrapped...)
			} else if wrapped, ok := tryWrapSingleLineExtends(line); ok {
				result = append(result, wrapped...)
			} else if wrapped, ok := tryWrapDatatypeAliasEnum(line); ok {
				result = append(result, wrapped...)
			} else if wrapped, ok := tryWrapInvariant(line); ok {
				result = append(result, wrapped...)
			} else {
				result = append(result, line)
			}
			i++
			continue
		}

		// Category 3: Short lines — pass through unchanged
		result = append(result, line)
		i++
	}

	return strings.Join(result, "\n")
}

// =============================================================================
// Enum Wrapping / Collapsing
// =============================================================================

// isMultilineEnumStart checks if a line starts a multiline Enum (has `Enum[`
// with unbalanced brackets) but is NOT a datatype alias (no ` = Enum[`).
func isMultilineEnumStart(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") {
		return false
	}
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
			if !isInsideString(line, absPos) {
				return true
			}
		}
		idx = absPos + 5
		if idx >= len(line) {
			return false
		}
	}
}

// isInsideString checks if position pos in line is inside a quoted string.
func isInsideString(line string, pos int) bool {
	inString := false
	for i := 0; i < pos && i < len(line); i++ {
		ch := line[i]
		if inString {
			if ch == '\\' {
				i++ // skip escaped character
				continue
			}
			if ch == '"' {
				inString = false
			}
		} else if ch == '"' {
			inString = true
		}
	}
	return inString
}

// hasUnbalancedBrackets checks if a line has more [ than ] (quote-aware).
func hasUnbalancedBrackets(line string) bool {
	depth := 0
	inString := false
	for i := 0; i < len(line); i++ {
		ch := line[i]
		if inString {
			if ch == '\\' && i+1 < len(line) {
				i++
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			continue
		}
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
		if !isInsideString(line, absPos) {
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
	inString := false
	for i := bracketStart; i < len(line); i++ {
		ch := line[i]
		if inString {
			if ch == '\\' && i+1 < len(line) {
				i++
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			continue
		}
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
	inString := false
	start := 0

	for i := 0; i < len(content); i++ {
		ch := content[i]
		if inString {
			if ch == '\\' && i+1 < len(content) {
				i++
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			continue
		}
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
// If it doesn't fit, re-emit as canonical multiline.
func tryCollapseEnum(collected []string) []string {
	if len(collected) < 2 {
		return collected
	}

	firstLine := collected[0]
	indent := extractIndent(firstLine)

	// Find Enum[ in first line
	enumIdx := strings.Index(firstLine, "Enum[")
	if enumIdx < 0 {
		return collected
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
		return collected
	}

	// Try single-line form
	singleLine := buildSingleLineEnum(prefix, values, suffix)
	if displayWidth(singleLine) <= lineWidthThreshold {
		return []string{singleLine}
	}

	// Re-emit canonical multiline
	return buildWrappedEnum(indent, prefix, values, suffix)
}

// =============================================================================
// Extends Wrapping / Collapsing
// =============================================================================

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
func collapseMultilineExtends(lines []string, startIdx int) ([]string, int) {
	firstLine := lines[startIdx]
	indent := extractIndent(firstLine)
	trimmed := strings.TrimSpace(firstLine)

	extendsIdx := strings.Index(trimmed, " extends")
	if extendsIdx < 0 {
		return []string{firstLine}, startIdx + 1
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

	// Collect subsequent lines until we find `{`
	i := startIdx + 1
	braceFound := false
	for i < len(lines) {
		lt := strings.TrimSpace(lines[i])
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
		out := make([]string, i-startIdx)
		copy(out, lines[startIdx:i])
		return out, i
	}

	// Try single-line form
	singleLine := buildSingleLineExtends(indent, header, types)
	if displayWidth(singleLine) <= lineWidthThreshold {
		return []string{singleLine}, i
	}

	// Re-emit canonical multiline
	return buildWrappedExtends(indent, header, types), i
}

// =============================================================================
// Datatype Alias Enum Wrapping / Collapsing
// =============================================================================

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
func tryCollapseDatatypeAliasEnum(collected []string) []string {
	// Reuse the same logic as regular Enum collapsing
	return tryCollapseEnum(collected)
}

// isMultilineInvariantStart checks if an invariant continues on the next line.
// This detects two cases:
// 1. Line is `! "message"` with nothing after (expression on next line)
// 2. Line is `! "message" expr_start` and next line is a continuation
func isMultilineInvariantStart(lines []string, idx int) bool {
	if idx >= len(lines) {
		return false
	}
	line := lines[idx]
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
		return idx+1 < len(lines)
	}

	// Case 2: Check if the next line is a continuation (indented deeper, not a new declaration)
	if idx+1 >= len(lines) {
		return false
	}

	currentIndent := len(extractIndent(line))
	nextLine := lines[idx+1]
	nextTrimmed := strings.TrimSpace(nextLine)
	if nextTrimmed == "" {
		return false
	}
	nextIndent := len(extractIndent(nextLine))

	if nextIndent <= currentIndent {
		return false
	}

	return !isNewDeclarationStart(nextTrimmed)
}

// findEndOfInvariantMessage finds the byte position after the closing `"` of the
// invariant message string. Returns -1 if not found.
func findEndOfInvariantMessage(trimmed string) int {
	// trimmed starts with `! "`
	// Find the closing quote of the message
	inString := false
	for i := 2; i < len(trimmed); i++ { // start after "! "
		ch := trimmed[i]
		if !inString {
			if ch == '"' {
				inString = true
			}
			continue
		}
		// Inside string
		if ch == '\\' && i+1 < len(trimmed) {
			i++
			continue
		}
		if ch == '"' {
			return i + 1 // position after closing quote
		}
	}
	return -1
}

// isNewDeclarationStart checks if a trimmed line starts a new declaration.
func isNewDeclarationStart(trimmed string) bool {
	prefixes := []string{
		"type ", "abstract ", "part ", "schema ", "import ",
		"! ", "-->", "*->", "//", "/*", "}",
	}
	for _, p := range prefixes {
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
func advancePastMultilineInvariant(lines []string, idx int) int {
	if idx >= len(lines) {
		return idx
	}
	currentIndent := len(extractIndent(lines[idx]))
	i := idx + 1
	for i < len(lines) {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			break
		}
		lineIndent := len(extractIndent(lines[i]))
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
		return nil, false // no operators → leave as-is
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
func findTopLevelLogicalOps(expr string) []logicalOp {
	var ops []logicalOp
	inString := false
	inRegex := false
	parenDepth := 0
	braceDepth := 0
	bracketDepth := 0

	for i := 0; i < len(expr); i++ {
		ch := expr[i]
		if inString {
			if ch == '\\' && i+1 < len(expr) {
				i++
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		if inRegex {
			if ch == '\\' && i+1 < len(expr) {
				i++ // skip escaped char inside regex
				continue
			}
			if ch == '/' {
				inRegex = false
			}
			continue
		}
		if ch == '"' {
			inString = true
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
				i++ // skip second char
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
// returns to 0. Returns the collected lines and the next index to process.
func collectMultilineConstruct(lines []string, startIdx int) ([]string, int) {
	var collected []string
	depth := 0
	i := startIdx

	for i < len(lines) {
		collected = append(collected, lines[i])
		inString := false
		for j := 0; j < len(lines[i]); j++ {
			ch := lines[i][j]
			if inString {
				if ch == '\\' && j+1 < len(lines[i]) {
					j++
					continue
				}
				if ch == '"' {
					inString = false
				}
				continue
			}
			if ch == '"' {
				inString = true
				continue
			}
			if ch == '[' {
				depth++
			}
			if ch == ']' {
				depth--
			}
		}
		i++
		if depth <= 0 {
			break
		}
	}
	return collected, i
}
