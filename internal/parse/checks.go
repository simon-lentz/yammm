package parse

import (
	"fmt"
	"regexp"
	"strconv"

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
	if c.Bounds == nil {
		return out
	}
	whole := b.spanOf(c.Pos, c.EndPos)
	out.Bounds = b.boundsOf(c.Bounds)
	minV, badMin := b.intBound(&c.Bounds.MinTok)
	maxV, badMax := b.intBound(&c.Bounds.MaxTok)
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
		b.warnPointlessMinus(t)
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
	if c.Bounds == nil {
		return out
	}
	whole := b.spanOf(c.Pos, c.EndPos)
	out.Bounds = b.boundsOf(c.Bounds)
	minV, badMin := b.floatBound(&c.Bounds.MinTok)
	maxV, badMax := b.floatBound(&c.Bounds.MaxTok)
	out.FloatMin, out.FloatMax = minV, maxV
	if !badMin && !badMax && minV != nil && maxV != nil && *minV > *maxV {
		b.reportf(diag.E_INVALID_CONSTRAINT, whole,
			"float bounds inverted: min %v > max %v", *minV, *maxV)
	}
	return out
}

func (b *builder) floatBound(t *boundTok) (*float64, bool) {
	if t.Text == unbounded {
		b.warnPointlessMinus(t)
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
// anchored on the sign alone since that is the character to delete.
func (b *builder) warnPointlessMinus(t *boundTok) {
	if !t.Neg {
		return
	}
	span := b.spanFromOffsets(t.Pos.Offset, t.Pos.Offset+1)
	b.report(diag.Warning, diag.E_INVALID_CONSTRAINT, span,
		"minus sign before '_' (unbounded) has no effect")
}

func (b *builder) stringConstraint(c *strC, span location.Span) *Constraint {
	out := &Constraint{Kind: ConstraintString, Span: span}
	if c.Bounds == nil {
		return out
	}
	out.Bounds = b.lenBoundsOf(c.Bounds)
	out.LenMin, out.LenMax = b.lengthBounds(c.Bounds, b.spanOf(c.Pos, c.EndPos), "string")
	return out
}

func (b *builder) listConstraint(c *lstC, span location.Span) *Constraint {
	out := &Constraint{Kind: ConstraintList, Span: span}
	if c.Elem != nil {
		out.Elem = b.constraint(c.Elem)
	}
	if c.Bounds == nil {
		return out
	}
	out.Bounds = b.lenBoundsOf(c.Bounds)
	out.LenMin, out.LenMax = b.lengthBounds(c.Bounds, b.spanOf(c.Pos, c.EndPos), "list")
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

func (b *builder) patternConstraint(c *patC, span location.Span) *Constraint {
	out := &Constraint{Kind: ConstraintPattern, Span: span}
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
		if err != nil {
			b.reportf(diag.E_INVALID_CONSTRAINT, lit.Span,
				"invalid regex pattern %q: %v", text, err)
		} else {
			lit.Regex, lit.Kept = re, true
		}
		out.PatternLits = append(out.PatternLits, lit)
	}
	return out
}

func (b *builder) timestampConstraint(c *timC, span location.Span) *Constraint {
	out := &Constraint{Kind: ConstraintTimestamp, Span: span}
	if c.Format == nil {
		return out
	}
	lit := Literal{Raw: c.Format.Raw, Span: b.spanOf(c.Format.Pos, c.Format.EndPos)}
	text, err := unquote(c.Format.Raw)
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

// unquote strips a literal's surrounding quotes and resolves its escapes.
// Single and double quotes are both accepted; an unquoted string is returned
// unchanged.
func unquote(s string) (string, error) {
	if !isQuoted(s) {
		return s, nil
	}
	out, err := strconv.Unquote(`"` + s[1:len(s)-1] + `"`)
	if err != nil {
		return "", fmt.Errorf("unquote string: %w", err)
	}
	return out, nil
}

func isQuoted(s string) bool {
	if len(s) < 2 {
		return false
	}
	return (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'')
}
