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

	return finalizeFormattedText(out.String()), nil
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
