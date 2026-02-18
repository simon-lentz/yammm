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
					sep := declarationSeparator(prev, tok, pendingWS, indentLevel, true)
					writeText(&out, sep, &lineStart)
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

	return finalizeFormattedText(alignColumns(collapseBlankLines(out.String()))), nil
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
	case "Integer", "Float", "String", "Enum", "Pattern", "Timestamp", "Vector":
		return true
	default:
		return false
	}
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
