package format

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/parse"
	"github.com/simon-lentz/yammm/location"
)

// tokenRange is a half-open byte range of the source.
type tokenRange struct {
	start int
	end   int
}

// The token kinds the pair rules below name. They are the rule names the
// parser's lexer table spells, matched by value because that table is
// keywordless — a keyword arrives as an ordinary word and is told apart by its
// text, exactly as isKeywordWithRequiredSpaceAfter does.
const (
	kindWS          = "WS"
	kindSLComment   = "SL_COMMENT"
	kindDocComment  = "DOC_COMMENT"
	kindSTRING      = "STRING"
	kindLBRACE      = "LBRACE"
	kindRBRACE      = "RBRACE"
	kindLBRACK      = "LBRACK"
	kindRBRACK      = "RBRACK"
	kindLPAR        = "LPAR"
	kindRPAR        = "RPAR"
	kindAT          = "AT"
	kindATAT        = "ATAT"
	kindASSOC       = "ASSOC"
	kindCOMP        = "COMP"
	kindEXCLAMATION = "EXCLAMATION"
	kindSLASH       = "SLASH"
	kindEQUALS      = "EQUALS"
	kindPERIOD      = "PERIOD"
	kindCOMMA       = "COMMA"
	kindMINUS       = "MINUS"
	kindCOLON       = "COLON"
	kindLT          = "LT"
	kindGT          = "GT"
)

type spacingAction int

const (
	spacingNone spacingAction = iota
	spacingSpace
	spacingNewline
)

// lineEndingReplacer normalizes CRLF and CR to LF. Package-level for reuse
// across calls — strings.Replacer is safe for concurrent use.
var lineEndingReplacer = strings.NewReplacer("\r\n", "\n", "\r", "\n")

// TokenStream applies parse-tree-assisted token-stream formatting.
// Returns an error if lexing/parsing fails so callers can fall back.
//
// It reads the source twice on purpose. The un-elided token stream carries the
// whitespace and comments a formatter must preserve but no expression extents;
// the node tree carries the extents and the syntax verdict but elides the
// whitespace. Neither alone is enough for phase 1.
func TokenStream(text string) (string, error) {
	normalized := lineEndingReplacer.Replace(text)

	// Fail on syntax alone: a source that is semantically invalid but parses,
	// such as inverted bounds, still formats.
	file, issues := parse.Parse([]byte(normalized), location.SourceID{})
	for _, iss := range issues {
		if iss.Code().Category() == diag.CategorySyntax {
			return "", fmt.Errorf("parse failed: %s", iss.Message())
		}
	}

	ranges := invariantExpressionRanges(file)
	allTokens := parse.Lex(normalized)

	var out strings.Builder
	var pendingWS strings.Builder
	lineStart := true
	indentLevel := 0
	var prev *parse.Token
	prevInExpr := false
	// Annotation-head tracking. prevAtSigil marks that the previous non-hidden
	// token was @ / @@ (so the current token is the annotation name), and
	// prevAnnotationName marks that the previous token was that name (so a
	// following "(" opens an argument list and takes no space).
	prevAtSigil := false
	prevAnnotationName := false

	for i := range allTokens {
		tok := &allTokens[i]
		inExpr := offsetInRanges(tok.Start, ranges)

		if tok.Kind == kindWS {
			if inExpr {
				if pendingWS.Len() > 0 {
					writeExprWhitespace(&out, pendingWS.String(), &lineStart)
					pendingWS.Reset()
				}
				writeExprWhitespace(&out, tok.Value, &lineStart)
			} else {
				pendingWS.WriteString(tok.Value)
			}
			continue
		}

		pendingStr := pendingWS.String()

		if inExpr {
			if pendingWS.Len() > 0 {
				if prev != nil && !prevInExpr {
					// Bridge from declaration context to expression context.
					// When there's a newline, this is a multiline invariant
					// whose continuation indentation was set by wrapInvariantAtOps.
					// Preserve the original whitespace so that the indent level
					// (which only tracks brace depth) doesn't flatten it.
					if strings.Contains(pendingStr, "\n") {
						writeExprWhitespace(&out, pendingStr, &lineStart)
					} else {
						sep := declarationSeparator(prev, tok, pendingStr, indentLevel, true)
						writeText(&out, sep, &lineStart)
					}
				} else {
					writeExprWhitespace(&out, pendingStr, &lineStart)
				}
				pendingWS.Reset()
			} else if prev != nil && !prevInExpr {
				sep := declarationSeparator(prev, tok, "", indentLevel, true)
				writeText(&out, sep, &lineStart)
			}

			writeTokenText(&out, tok, &lineStart)
			prev = tok
			prevInExpr = true
			continue
		}

		sep := declarationSeparator(prev, tok, pendingStr, indentLevel, false)
		// An annotation name followed by "(" opens its argument list: collapse the
		// space the (name, LPAR) default would otherwise emit. Decided here from
		// loop state because that same pair is a relation multiplicity elsewhere,
		// which must keep its space.
		if prevAnnotationName && tok.Kind == kindLPAR {
			sep = ""
		}
		pendingWS.Reset()
		writeText(&out, sep, &lineStart)
		writeTokenText(&out, tok, &lineStart)

		if tok.Kind == kindLBRACE {
			indentLevel++
		} else if tok.Kind == kindRBRACE && indentLevel > 0 {
			indentLevel--
		}

		// The non-hidden token immediately after @ / @@ is always the annotation
		// name (grammar), so prevAtSigil alone identifies it.
		prevAnnotationName = prevAtSigil
		prevAtSigil = tok.Kind == kindAT || tok.Kind == kindATAT
		prev = tok
		prevInExpr = false
	}

	if pendingWS.Len() > 0 {
		writeText(&out, pendingWS.String(), &lineStart)
	}

	return finalizeFormattedText(AlignColumns(WrapLongLines(collapseBlankLines(out.String())))), nil
}

// invariantExpressionRanges returns the byte extent of every invariant
// expression, sorted and merged. Inside one, the author's own spacing is kept
// and the declaration pair rules do not apply.
func invariantExpressionRanges(file *parse.File) []tokenRange {
	var ranges []tokenRange
	for _, t := range file.Types {
		for _, inv := range t.Invariants {
			span := inv.ExprSpan
			if span.End.Byte <= span.Start.Byte {
				continue
			}
			ranges = append(ranges, tokenRange{start: span.Start.Byte, end: span.End.Byte})
		}
	}
	if len(ranges) == 0 {
		return nil
	}

	slices.SortFunc(ranges, func(a, b tokenRange) int {
		return cmp.Or(
			cmp.Compare(a.start, b.start),
			cmp.Compare(a.end, b.end),
		)
	})

	merged := make([]tokenRange, 0, len(ranges))
	current := ranges[0]
	for _, r := range ranges[1:] {
		if r.start <= current.end {
			if r.end > current.end {
				current.end = r.end
			}
			continue
		}
		merged = append(merged, current)
		current = r
	}
	return append(merged, current)
}

// offsetInRanges reports whether a byte offset falls inside a merged range. The
// ranges are half-open, which is what makes the end offset the first byte after
// the expression rather than its last.
func offsetInRanges(off int, ranges []tokenRange) bool {
	if off < 0 || len(ranges) == 0 {
		return false
	}
	i, _ := slices.BinarySearchFunc(ranges, off, func(r tokenRange, target int) int {
		return cmp.Compare(r.end-1, target)
	})
	if i >= len(ranges) {
		return false
	}
	return ranges[i].start <= off
}

func declarationSeparator(
	prev *parse.Token,
	curr *parse.Token,
	pendingWS string,
	indentLevel int,
	currInExpr bool,
) string {
	newlineCount := strings.Count(pendingWS, "\n")
	currIndent := indentLevel
	if !currInExpr && curr.Kind == kindRBRACE && currIndent > 0 {
		currIndent--
	}

	if prev == nil {
		if newlineCount > 0 {
			return newlineSeparator(newlineCount, currIndent)
		}
		return ""
	}

	if isCommentToken(curr.Kind) {
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

func declarationSpacingAction(prev *parse.Token, curr *parse.Token) spacingAction {
	if prev == nil {
		return spacingNone
	}

	prevType := prev.Kind
	currType := curr.Kind

	if currType == kindRBRACE {
		return spacingNewline
	}
	if prevType == kindLBRACE {
		return spacingNewline
	}
	if prevType == kindRBRACE {
		return spacingNewline
	}
	if prevType == kindDocComment {
		return spacingNewline
	}

	// Closing delimiters win over broad left-side rules.
	if currType == kindRBRACK || currType == kindRPAR {
		return spacingNone
	}

	// Annotation sigil: no space between @ / @@ and the annotation name that
	// always follows it. The name-to-"(" spacing is decided in TokenStream from
	// loop state, not here, because that pair is shared with a relation
	// multiplicity (`worksAt (one)`), which must keep its space.
	if prevType == kindAT || prevType == kindATAT {
		return spacingNone
	}

	// Specific pair rules.
	if prevType == kindEXCLAMATION && currType == kindSTRING {
		return spacingSpace
	}
	if prevType == kindASSOC || prevType == kindCOMP {
		return spacingSpace
	}
	if currType == kindLBRACE {
		return spacingSpace
	}
	if prevType == kindSLASH || currType == kindSLASH {
		return spacingSpace
	}
	if prevType == kindEQUALS || currType == kindEQUALS {
		return spacingSpace
	}
	if prevType == kindPERIOD || currType == kindPERIOD {
		return spacingNone
	}
	if prevType == kindCOMMA {
		return spacingSpace
	}
	if currType == kindCOMMA {
		return spacingNone
	}
	if prevType == kindMINUS {
		return spacingNone
	}
	if prevType == kindLPAR || currType == kindRPAR {
		return spacingNone
	}
	if prevType == kindCOLON || currType == kindCOLON {
		return spacingNone
	}
	if prevType == kindLBRACK {
		return spacingNone
	}
	if currType == kindLBRACK && isConstraintBracketLeft(prev.Value) {
		return spacingNone
	}
	// List type angle brackets: collapse spacing around < and > only in
	// type contexts (e.g. List<String>), not in expression comparisons.
	if currType == kindLT && isListAngleBracketLeft(prev.Value) {
		return spacingNone
	}
	if prevType == kindLT {
		return spacingNone
	}
	if currType == kindGT {
		return spacingNone
	}
	if prevType == kindGT {
		if currType == kindLBRACK {
			return spacingNone
		}
		return spacingSpace
	}
	if isKeywordWithRequiredSpaceAfter(prev.Value) {
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

func isCommentToken(kind string) bool {
	return kind == kindSLComment || kind == kindDocComment
}

func normalizeDocComment(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		trimmed := strings.TrimRight(line, " \t")
		lines[i] = NormalizeIndentation(trimmed)
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
			b.WriteString(NormalizeIndentation(seg))
		} else {
			b.WriteString(seg)
		}
		i = j
	}

	writeText(out, b.String(), lineStart)
}

func writeTokenText(out *strings.Builder, tok *parse.Token, lineStart *bool) {
	text := tok.Value
	if tok.Kind == kindDocComment {
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
