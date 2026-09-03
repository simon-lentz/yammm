package parse

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
)

// Bounds on a Vector's dimension count. Below the first is meaningless and
// above the second is refused rather than allowed to exhaust memory downstream.
const (
	minVectorDimensions = 1
	maxVectorDimensions = 65536
)

// unbounded is the spelling that leaves one side of a bound pair open.
const unbounded = "_"

// relationNameRE is the UPPER_SNAKE production for a relation name. Its
// lower-case spelling is the relation's field name, so every layer that
// compares the two case-insensitively is right by construction.
var relationNameRE = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// checkRelationName reports a relation name that is not UPPER_SNAKE at the
// name's own span; the declaration still projects, so recovery is unaffected.
func (b *builder) checkRelationName(name string, span location.Span) {
	if relationNameRE.MatchString(name) {
		return
	}
	b.reportf(diag.E_INVALID_NAME, span,
		"relation name %q is not UPPER_SNAKE: an uppercase letter, then uppercase letters, digits or underscores", name)
}

// constraint maps a parsed datatype reference and runs the checks that read
// only the constraint's own written arguments. span is the enclosing
// reference's extent, so a datatype declaration and a property that uses it
// report the same region.
func (b *builder) constraint(c *constraintNode) *Constraint {
	span := b.spanOf(c.Pos, c.EndPos)
	switch {
	case c.B != nil:
		return b.builtin(c.B, span)
	case c.Ali != nil:
		return b.aliasConstraint(c.Ali, span)
	}
	return &Constraint{Span: span}
}

func (b *builder) aliasConstraint(a *aliasC, span location.Span) *Constraint {
	ref := &TypeRef{
		Name:     a.Name.Value,
		NameSpan: b.spanOf(a.Name.Pos, a.Name.EndPos),
		Span:     b.spanOf(a.Pos, a.EndPos),
	}
	if a.Qual != nil {
		ref.Qualifier = *a.Qual
	}
	return &Constraint{Kind: ConstraintAlias, Span: span, Alias: ref}
}

func (b *builder) builtin(c *builtinNode, span location.Span) *Constraint {
	switch {
	case c.Int != nil:
		return b.intConstraint(c.Int, span)
	case c.Flt != nil:
		return b.floatConstraint(c.Flt, span)
	case c.Boo != nil:
		return &Constraint{Kind: ConstraintBoolean, Span: span}
	case c.Str != nil:
		return b.stringConstraint(c.Str, span)
	case c.Enu != nil:
		return b.enumConstraint(c.Enu, span)
	case c.Pat != nil:
		return b.patternConstraint(c.Pat, span)
	case c.Tim != nil:
		return b.timestampConstraint(c.Tim, span)
	case c.Dat != nil:
		return &Constraint{Kind: ConstraintDate, Span: span}
	case c.UUID != nil:
		return &Constraint{Kind: ConstraintUUID, Span: span}
	case c.Vec != nil:
		return b.vectorConstraint(c.Vec, span)
	case c.Lst != nil:
		return b.listConstraint(c.Lst, span)
	}
	return &Constraint{Span: span}
}

func (b *builder) intConstraint(c *intC, span location.Span) *Constraint {
	out := &Constraint{Kind: ConstraintInteger, Span: span}
	bounds := c.Bounds
	if bounds == nil {
		return out
	}
	whole := b.spanOf(c.Pos, c.EndPos)
	out.Bounds = b.boundsOf(bounds)
	minV, badMin := b.intBound(&bounds.MinTok)
	maxV, badMax := b.intBound(&bounds.MaxTok)
	out.IntMin, out.IntMax = minV, maxV
	if !badMin && !badMax && minV != nil && maxV != nil && *minV > *maxV {
		b.reportf(diag.E_INVALID_CONSTRAINT, whole,
			"integer bounds inverted: min %d > max %d", *minV, *maxV)
	}
	return out
}

// intBound parses one signed integer bound. It reports whether parsing failed
// separately from whether a value exists, because an unparseable bound must
// suppress the inverted-bounds check while an unbounded one must not.
func (b *builder) intBound(t *boundTok) (*int64, bool) {
	if t.Text == unbounded {
		b.warnPointlessMinus(t.Neg, t.Pos.Offset)
		return nil, false
	}
	text := t.Text
	if t.Neg {
		text = "-" + text
	}
	v, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		b.reportf(diag.E_INVALID_CONSTRAINT, b.spanOf(t.Pos, t.EndPos),
			"invalid integer bound: %v", err)
		return nil, true
	}
	return &v, false
}

func (b *builder) floatConstraint(c *fltC, span location.Span) *Constraint {
	out := &Constraint{Kind: ConstraintFloat, Span: span}
	bounds := c.Bounds
	if bounds == nil {
		return out
	}
	whole := b.spanOf(c.Pos, c.EndPos)
	out.Bounds = b.fltBoundsOf(bounds)
	minV, badMin := b.floatBound(&bounds.MinTok)
	maxV, badMax := b.floatBound(&bounds.MaxTok)
	out.FloatMin, out.FloatMax = minV, maxV
	if !badMin && !badMax && minV != nil && maxV != nil && *minV > *maxV {
		b.reportf(diag.E_INVALID_CONSTRAINT, whole,
			"float bounds inverted: min %v > max %v", *minV, *maxV)
	}
	return out
}

func (b *builder) floatBound(t *fltBoundTok) (*float64, bool) {
	if t.Text == unbounded {
		b.warnPointlessMinus(t.Neg, t.Pos.Offset)
		return nil, false
	}
	text := t.Text
	if t.Neg {
		text = "-" + text
	}
	v, err := strconv.ParseFloat(text, 64)
	if err != nil {
		b.reportf(diag.E_INVALID_CONSTRAINT, b.spanOf(t.Pos, t.EndPos),
			"invalid float bound: %v", err)
		return nil, true
	}
	return &v, false
}

// warnPointlessMinus reports a minus sign written before the unbounded marker,
// anchored on the sign alone since that is the character to delete. It takes
// the sign and its position rather than a bound node, because Integer and Float
// bounds are different types.
func (b *builder) warnPointlessMinus(neg bool, at int) {
	if !neg {
		return
	}
	span := b.spanFromOffsets(at, at+1)
	b.report(diag.Warning, diag.E_INVALID_CONSTRAINT, span,
		"minus sign before '_' (unbounded) has no effect")
}

func (b *builder) stringConstraint(c *strC, span location.Span) *Constraint {
	out := &Constraint{Kind: ConstraintString, Span: span}
	bounds := c.Bounds
	if bounds == nil {
		return out
	}
	out.Bounds = b.lenBoundsOf(bounds)
	out.LenMin, out.LenMax = b.lengthBounds(bounds, b.spanOf(c.Pos, c.EndPos), "string")
	return out
}

func (b *builder) listConstraint(c *lstC, span location.Span) *Constraint {
	out := &Constraint{Kind: ConstraintList, Span: span}
	if c.Elem != nil {
		out.Elem = b.constraint(c.Elem)
	}
	bounds := c.Bounds
	if bounds == nil {
		return out
	}
	out.Bounds = b.lenBoundsOf(bounds)
	out.LenMin, out.LenMax = b.lengthBounds(bounds, b.spanOf(c.Pos, c.EndPos), "list")
	return out
}

// lengthBounds parses an unsigned length pair. kind names the constraint in
// the diagnostics, which say "string length bound" or "list length bound".
func (b *builder) lengthBounds(bd *lenBoundsC, whole location.Span, kind string) (minOut, maxOut *int64) {
	minV, badMin := b.lengthBound(&bd.MinTok, kind, "minimum")
	maxV, badMax := b.lengthBound(&bd.MaxTok, kind, "maximum")
	if !badMin && !badMax && minV != nil && maxV != nil && *minV > *maxV {
		b.reportf(diag.E_INVALID_CONSTRAINT, whole,
			"%s length bounds inverted: min %d > max %d", kind, *minV, *maxV)
	}
	return minV, maxV
}

// lengthBound parses one length bound. The negative case cannot be written —
// the lexer gives no minus sign to an unsigned bound position — but it is
// checked anyway so a future grammar change cannot make it silent.
func (b *builder) lengthBound(t *lenTok, kind, side string) (*int64, bool) {
	if t.Text == unbounded {
		return nil, false
	}
	span := b.spanOf(t.Pos, t.EndPos)
	v, err := strconv.ParseInt(t.Text, 10, 64)
	if err != nil {
		b.reportf(diag.E_INVALID_CONSTRAINT, span,
			"invalid %s length bound: %v", kind, err)
		return nil, true
	}
	if v < 0 {
		b.reportf(diag.E_INVALID_CONSTRAINT, span,
			"%s %s length cannot be negative: %d", kind, side, v)
		return nil, true
	}
	return &v, false
}

// enumConstraint collects the enum's values, dropping any that will not
// unquote, are empty, or repeat an earlier value. The two-value minimum counts
// survivors rather than written values, so "one good and one duplicate" is
// reported as the single value it really is.
func (b *builder) enumConstraint(c *enuC, span location.Span) *Constraint {
	out := &Constraint{Kind: ConstraintEnum, Span: span}
	seen := make(map[string]bool, len(c.Values))
	kept := 0
	for i := range c.Values {
		v := &c.Values[i]
		lit := Literal{Raw: v.Raw, Span: b.spanOf(v.Pos, v.EndPos)}
		text, err := unquote(v.Raw)
		switch {
		case err != nil:
			b.reportf(diag.E_SYNTAX, lit.Span, "invalid enum value: %v", err)
		case text == "":
			b.report(diag.Error, diag.E_INVALID_CONSTRAINT, lit.Span, "enum value cannot be empty")
		case seen[text]:
			b.reportf(diag.E_INVALID_CONSTRAINT, lit.Span, "duplicate enum value %q", text)
		default:
			seen[text] = true
			lit.Text, lit.Kept = text, true
			kept++
		}
		out.EnumLits = append(out.EnumLits, lit)
	}
	if kept < 2 {
		b.reportf(diag.E_INVALID_CONSTRAINT, b.spanOf(c.Pos, c.EndPos),
			"enum must have at least two values (got %d)", kept)
	}
	return out
}

// maxPatterns is the largest pattern list a Pattern constraint accepts. The
// count is of patterns that compile, which is what the generated parser counts;
// every written literal stays in PatternLits, and only the first maxPatterns
// survivors carry a Regex, so nothing downstream enforces a pattern past the cap.
const maxPatterns = 2

func (b *builder) patternConstraint(c *patC, span location.Span) *Constraint {
	out := &Constraint{Kind: ConstraintPattern, Span: span}
	kept := 0
	for i := range c.Patterns {
		p := &c.Patterns[i]
		lit := Literal{Raw: p.Raw, Span: b.spanOf(p.Pos, p.EndPos)}
		text, err := unquote(p.Raw)
		if err != nil {
			b.reportf(diag.E_SYNTAX, lit.Span, "invalid pattern: %v", err)
			out.PatternLits = append(out.PatternLits, lit)
			continue
		}
		lit.Text = text
		re, err := regexp.Compile(text)
		switch {
		case err != nil:
			b.reportf(diag.E_INVALID_CONSTRAINT, lit.Span,
				"invalid regex pattern %q: %v", text, err)
		case kept < maxPatterns:
			lit.Regex, lit.Kept = re, true
			kept++
		default:
			kept++
		}
		out.PatternLits = append(out.PatternLits, lit)
	}
	if kept > maxPatterns {
		b.reportf(diag.E_INVALID_CONSTRAINT, span,
			"pattern constraint exceeds maximum of %d patterns (got %d)", maxPatterns, kept)
	}
	return out
}

func (b *builder) timestampConstraint(c *timC, span location.Span) *Constraint {
	out := &Constraint{Kind: ConstraintTimestamp, Span: span}
	if c.Format == nil {
		return out
	}
	f := c.Format
	lit := Literal{Raw: f.Raw, Span: b.spanOf(f.Pos, f.EndPos)}
	text, err := unquote(f.Raw)
	if err != nil {
		b.reportf(diag.E_SYNTAX, lit.Span, "invalid timestamp format: %v", err)
	} else {
		lit.Text, lit.Kept = text, true
	}
	out.FormatLit = &lit
	return out
}

func (b *builder) vectorConstraint(c *vecC, span location.Span) *Constraint {
	out := &Constraint{Kind: ConstraintVector, Span: span}
	dimSpan := b.spanOf(c.Dims.Pos, c.Dims.EndPos)
	out.DimsLit = &IntLit{Text: c.Dims.Text, Span: dimSpan}

	dim, err := strconv.Atoi(c.Dims.Text)
	if err != nil {
		b.reportf(diag.E_INVALID_CONSTRAINT, b.spanOf(c.Pos, c.EndPos),
			"invalid vector dimensions: %v", err)
		return out
	}
	switch {
	case dim < minVectorDimensions:
		b.reportf(diag.E_INVALID_CONSTRAINT, dimSpan,
			"vector dimensions must be at least %d (got %d)", minVectorDimensions, dim)
	case dim > maxVectorDimensions:
		b.reportf(diag.E_INVALID_CONSTRAINT, dimSpan,
			"vector dimensions exceed maximum of %d (got %d)", maxVectorDimensions, dim)
	default:
		out.VectorDims = &dim
	}
	return out
}

// ---- written-argument capture ----

// fltBoundsOf mirrors boundsOf for Float's own bound pair, which carries a
// different token type because the two admit different spellings.
func (b *builder) fltBoundsOf(bd *fltBoundsC) *Bounds {
	return &Bounds{
		Min:  Bound{Text: bd.MinTok.Text, Neg: bd.MinTok.Neg, Span: b.spanOf(bd.MinTok.Pos, bd.MinTok.EndPos)},
		Max:  Bound{Text: bd.MaxTok.Text, Neg: bd.MaxTok.Neg, Span: b.spanOf(bd.MaxTok.Pos, bd.MaxTok.EndPos)},
		Span: b.spanOf(bd.Pos, bd.EndPos),
	}
}

func (b *builder) boundsOf(bd *numBoundsC) *Bounds {
	return &Bounds{
		Min:  Bound{Text: bd.MinTok.Text, Neg: bd.MinTok.Neg, Span: b.spanOf(bd.MinTok.Pos, bd.MinTok.EndPos)},
		Max:  Bound{Text: bd.MaxTok.Text, Neg: bd.MaxTok.Neg, Span: b.spanOf(bd.MaxTok.Pos, bd.MaxTok.EndPos)},
		Span: b.spanOf(bd.Pos, bd.EndPos),
	}
}

func (b *builder) lenBoundsOf(bd *lenBoundsC) *Bounds {
	return &Bounds{
		Min:  Bound{Text: bd.MinTok.Text, Span: b.spanOf(bd.MinTok.Pos, bd.MinTok.EndPos)},
		Max:  Bound{Text: bd.MaxTok.Text, Span: b.spanOf(bd.MaxTok.Pos, bd.MaxTok.EndPos)},
		Span: b.spanOf(bd.Pos, bd.EndPos),
	}
}

// ---- string literals ----

// unquoteAt unquotes a literal and reports a syntax error on the literal
// itself when it will not unquote, returning whether the value is usable.
func (b *builder) unquoteAt(t *strTok, what string) (string, bool) {
	text, err := unquote(t.Raw)
	if err != nil {
		b.reportf(diag.E_SYNTAX, b.spanOf(t.Pos, t.EndPos), "%s: %v", what, err)
		return "", false
	}
	return text, true
}

// errUnquoteSyntax keeps the exact message the strconv-backed unquoter
// produced, so consumers matching diagnostic text see no change.
// unquoteSyntaxCause is the same message without the prefix, for the one
// reporting site that supplies its own.
const unquoteSyntaxCause = "invalid syntax"

var errUnquoteSyntax = errors.New("unquote string: " + unquoteSyntaxCause)

// unquote strips a literal's surrounding quotes and resolves exactly the
// lexer's STRING escape vocabulary: \b \t \n \f \r \0 \xHH \uHHHH \" \' \\.
// The quote character not delimiting the literal appears literally unescaped,
// and an invalid UTF-8 byte becomes U+FFFD, as strconv.Unquote resolved it.
func unquote(s string) (string, error) {
	if !isQuoted(s) {
		return s, nil
	}
	body := s[1 : len(s)-1]
	var out strings.Builder
	out.Grow(len(body))
	for i := 0; i < len(body); {
		c := body[i]
		if c != '\\' {
			if c < utf8.RuneSelf {
				out.WriteByte(c)
				i++
				continue
			}
			// DecodeRuneInString yields (RuneError, 1) on an invalid byte, so
			// WriteRune substitutes U+FFFD one byte at a time.
			r, size := utf8.DecodeRuneInString(body[i:])
			out.WriteRune(r)
			i += size
			continue
		}
		if i+1 >= len(body) {
			return "", errUnquoteSyntax
		}
		esc := body[i+1]
		i += 2
		switch esc {
		case 'b':
			out.WriteByte('\b')
		case 't':
			out.WriteByte('\t')
		case 'n':
			out.WriteByte('\n')
		case 'f':
			out.WriteByte('\f')
		case 'r':
			out.WriteByte('\r')
		case '0':
			out.WriteByte(0)
		case '"':
			out.WriteByte('"')
		case '\'':
			out.WriteByte('\'')
		case '\\':
			out.WriteByte('\\')
		case 'x':
			if i+2 > len(body) {
				return "", errUnquoteSyntax
			}
			v, err := strconv.ParseUint(body[i:i+2], 16, 8)
			if err != nil {
				return "", errUnquoteSyntax
			}
			out.WriteByte(byte(v))
			i += 2
		case 'u':
			if i+4 > len(body) {
				return "", errUnquoteSyntax
			}
			v, err := strconv.ParseUint(body[i:i+4], 16, 16)
			if err != nil {
				return "", errUnquoteSyntax
			}
			r := rune(uint16(v))
			if !utf8.ValidRune(r) {
				return "", errUnquoteSyntax
			}
			out.WriteRune(r)
			i += 4
		default:
			return "", errUnquoteSyntax
		}
	}
	return out.String(), nil
}

func isQuoted(s string) bool {
	if len(s) < 2 {
		return false
	}
	return (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'')
}
