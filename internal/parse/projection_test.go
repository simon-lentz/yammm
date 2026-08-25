package parse

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema/expr"
)

// The projection is deliberately syntax-level: names, modifiers, raw bound
// spellings, raw literal spellings, byte spans, and compiled invariant
// expressions rendered to a canonical S-expression string. No semantic
// resolution and no unquoting, so a golden diff is a fact about this parser
// and never about a later phase.
//
// It carries the declarations and the diagnostics in one artifact, which is
// what lets one frozen golden answer both halves of the parity contract:
// what the parser accepts, and how it behaves when it does not.

// projSpan is a byte range, start inclusive, end exclusive. The zero value
// mirrors [location.Span]'s zero for positionless artifacts.
type projSpan struct {
	Start int `json:"s"`
	End   int `json:"e"`
}

type projSchema struct {
	Name      string         `json:"name"` // raw STRING spelling, quotes included
	Doc       string         `json:"doc,omitempty"`
	Span      projSpan       `json:"span"`
	Imports   []projImport   `json:"imports,omitempty"`
	DataTypes []projDataType `json:"datatypes,omitempty"`
	Types     []projType     `json:"types,omitempty"`
	Diags     []projDiag     `json:"diags,omitempty"`
}

type projImport struct {
	Path  string   `json:"path"`
	Alias string   `json:"alias,omitempty"` // empty when derived; derivation is loader semantics, not syntax
	Span  projSpan `json:"span"`
}

type projDataType struct {
	Name       string         `json:"name"`
	Doc        string         `json:"doc,omitempty"`
	Constraint projConstraint `json:"constraint"`
	Span       projSpan       `json:"span"`
}

type projType struct {
	Name       string        `json:"name"`
	NameSpan   projSpan      `json:"name_span"`
	Abstract   bool          `json:"abstract,omitempty"`
	Part       bool          `json:"part,omitempty"`
	Doc        string        `json:"doc,omitempty"`
	Extends    []projTypeRef `json:"extends,omitempty"`
	Props      []projProp    `json:"props,omitempty"`
	Rels       []projRel     `json:"rels,omitempty"`
	TypeAnns   []projAnn     `json:"type_anns,omitempty"`
	Invariants []projInv     `json:"invariants,omitempty"`
	Span       projSpan      `json:"span"`
}

// projRel is one association or composition. Omitted multiplicity is
// optional-and-single, so Optional is true far more often than false.
type projRel struct {
	Kind     string      `json:"kind"` // "assoc" | "comp"
	Name     string      `json:"name"`
	NameSpan projSpan    `json:"name_span"`
	Doc      string      `json:"doc,omitempty"`
	Target   projTypeRef `json:"target"`
	Optional bool        `json:"optional,omitempty"`
	Many     bool        `json:"many,omitempty"`
	Props    []projProp  `json:"props,omitempty"` // edge properties; a composition can carry none
	Span     projSpan    `json:"span"`
}

type projTypeRef struct {
	Qualifier string   `json:"qualifier,omitempty"`
	Name      string   `json:"name"`
	Span      projSpan `json:"span"`
}

type projProp struct {
	Name       string         `json:"name"`
	Doc        string         `json:"doc,omitempty"`
	Constraint projConstraint `json:"constraint"`
	Primary    bool           `json:"primary,omitempty"`
	Required   bool           `json:"required,omitempty"` // as written; primary implies required at the semantic layer, not here
	Anns       []projAnn      `json:"anns,omitempty"`
	Span       projSpan       `json:"span"`
}

type projAnn struct {
	Name             string    `json:"name"`
	Args             []projArg `json:"args,omitempty"`
	HasParens        bool      `json:"has_parens,omitempty"`
	Doc              string    `json:"doc,omitempty"`
	DetachedFromLine int       `json:"detached_from_line,omitempty"`
	Span             projSpan  `json:"span"`
}

type projArg struct {
	Kind string   `json:"kind"` // "ident" | "literal"
	Text string   `json:"text"` // raw source spelling
	Span projSpan `json:"span"`
}

type projInv struct {
	Msg      string   `json:"msg"` // raw STRING spelling
	Doc      string   `json:"doc,omitempty"`
	Expr     string   `json:"expr"`      // canonical S-expression rendering
	ExprSpan projSpan `json:"expr_span"` // the constraint expression's own extent
	Span     projSpan `json:"span"`
}

// projConstraint is the syntax-level shape of a data type reference: the kind
// and the raw spellings of its arguments, plus the element constraint for
// List. Alias carries the qualified name as written.
type projConstraint struct {
	Kind string          `json:"kind"`
	Args []string        `json:"args,omitempty"`
	Elem *projConstraint `json:"elem,omitempty"`
	Span projSpan        `json:"span"`
}

// projDiag is one diagnostic. Severity is the canonical [diag.Severity] label
// because a code alone cannot tell a rejection from a note: the
// minus-before-'_' constraint check is a Warning under the same code that
// carries Errors.
type projDiag struct {
	Code     string   `json:"code"`
	Severity string   `json:"severity,omitempty"`
	Msg      string   `json:"msg"`
	Span     projSpan `json:"span"`
	Spanless bool     `json:"spanless,omitempty"`
	Line     int      `json:"line,omitempty"` // 1-based start line, for casebook anchoring
	EndLine  int      `json:"end_line,omitempty"`
}

// project parses one source and projects the result, adding no diagnostic of
// its own, so a golden difference is a difference in the parser. The issues
// come back alongside because the projection flattens severity to a label.
func project(name, src string) (*projSchema, []diag.Issue) {
	file, issues := Parse([]byte(src), location.NewSourceID(name))

	p := projector{src: src}
	out := &projSchema{
		Name: file.NameRaw,
		Doc:  file.Doc,
		Span: p.span(file.Span),
	}
	for _, imp := range file.Imports {
		n := projImport{Path: imp.PathRaw, Span: p.span(imp.Span)}
		if imp.HasAlias {
			n.Alias = imp.Alias
		}
		out.Imports = append(out.Imports, n)
	}
	for _, dt := range file.DataTypes {
		out.DataTypes = append(out.DataTypes, projDataType{
			Name:       dt.Name,
			Doc:        dt.Doc,
			Constraint: p.constraint(dt.Constraint),
			Span:       p.span(dt.Span),
		})
	}
	for _, ty := range file.Types {
		out.Types = append(out.Types, p.typeDecl(ty))
	}
	for _, iss := range issues {
		out.Diags = append(out.Diags, p.diag(iss))
	}
	sortDiags(out.Diags)
	return out, issues
}

type projector struct{ src string }

func (p projector) typeDecl(ty *TypeDecl) projType {
	nt := projType{
		Name:     ty.Name,
		NameSpan: p.span(ty.NameSpan),
		Abstract: ty.IsAbstract,
		Part:     ty.IsPart,
		Doc:      ty.Doc,
		Span:     p.span(ty.Span),
	}
	for _, ext := range ty.Extends {
		nt.Extends = append(nt.Extends, p.typeRef(ext))
	}
	for _, prop := range ty.Properties {
		nt.Props = append(nt.Props, p.prop(prop))
	}
	for _, rel := range ty.Relations {
		nt.Rels = append(nt.Rels, p.rel(rel))
	}
	for _, ann := range ty.Annotations {
		nt.TypeAnns = append(nt.TypeAnns, p.ann(ann))
	}
	for _, inv := range ty.Invariants {
		nt.Invariants = append(nt.Invariants, projInv{
			Msg:      inv.MessageRaw,
			Doc:      inv.Doc,
			Expr:     renderExpr(inv.Expr),
			ExprSpan: p.span(inv.ExprSpan),
			Span:     p.span(inv.Span),
		})
	}
	return nt
}

func (p projector) rel(r *Relation) projRel {
	kind := "assoc"
	if r.Kind == RelationComposition {
		kind = "comp"
	}
	nr := projRel{
		Kind:     kind,
		Name:     r.Name,
		NameSpan: p.span(r.NameSpan),
		Doc:      r.Doc,
		Optional: r.Optional,
		Many:     r.Many,
		Span:     p.span(r.Span),
	}
	if r.Target != nil {
		nr.Target = p.typeRef(r.Target)
	}
	for _, ep := range r.Properties {
		nr.Props = append(nr.Props, p.prop(ep))
	}
	return nr
}

func (p projector) prop(pr *Property) projProp {
	np := projProp{
		Name:       pr.Name,
		Doc:        pr.Doc,
		Constraint: p.constraint(pr.Constraint),
		Primary:    pr.IsPrimaryKey,
		Required:   pr.IsRequired,
		Span:       p.span(pr.Span),
	}
	for _, a := range pr.Annotations {
		np.Anns = append(np.Anns, p.ann(a))
	}
	return np
}

func (p projector) ann(a *Annotation) projAnn {
	na := projAnn{
		Name:             a.Name,
		HasParens:        a.HasParens,
		Doc:              a.Doc,
		DetachedFromLine: a.DetachedFromLine,
		Span:             p.span(a.Span),
	}
	for _, arg := range a.Args {
		na.Args = append(na.Args, projArg{
			Kind: argKind(arg.Kind),
			Text: argText(arg),
			Span: p.span(arg.Span),
		})
	}
	return na
}

func argKind(k ArgKind) string {
	if k == ArgIdentifier {
		return "ident"
	}
	return "literal"
}

// argText returns the source spelling. A quoted string that unquoted cleanly
// keeps its value in Text and its spelling in Raw; every other kind carries
// the spelling in Text already.
func argText(a Arg) string {
	if a.Kind == ArgString {
		return a.Raw
	}
	return a.Text
}

func (p projector) typeRef(r *TypeRef) projTypeRef {
	return projTypeRef{
		Qualifier: r.Qualifier,
		Name:      r.Name,
		Span:      p.span(r.Span),
	}
}

// constraint projects the written arguments, never the computed values, so a
// golden cannot turn on a number being parsed differently.
func (p projector) constraint(c *Constraint) projConstraint {
	if c == nil {
		return projConstraint{Kind: "?"}
	}
	n := projConstraint{Kind: projConstraintKinds[c.Kind], Span: p.span(c.Span)}
	switch c.Kind {
	case ConstraintInteger, ConstraintFloat:
		if c.Bounds != nil {
			n.Args = []string{boundText(c.Bounds.Min), boundText(c.Bounds.Max)}
		}
	case ConstraintString:
		if c.Bounds != nil {
			n.Args = []string{c.Bounds.Min.Text, c.Bounds.Max.Text}
		}
	case ConstraintList:
		if c.Elem != nil {
			elem := p.constraint(c.Elem)
			n.Elem = &elem
		}
		if c.Bounds != nil {
			n.Args = []string{c.Bounds.Min.Text, c.Bounds.Max.Text}
		}
	case ConstraintEnum:
		n.Args = rawLits(c.EnumLits)
	case ConstraintPattern:
		n.Args = rawLits(c.PatternLits)
	case ConstraintTimestamp:
		if c.FormatLit != nil {
			n.Args = []string{c.FormatLit.Raw}
		}
	case ConstraintVector:
		if c.DimsLit != nil {
			n.Args = []string{c.DimsLit.Text}
		}
	case ConstraintAlias:
		if c.Alias != nil {
			name := c.Alias.Name
			if c.Alias.Qualifier != "" {
				name = c.Alias.Qualifier + "." + name
			}
			n.Args = []string{name}
		}
	case ConstraintBoolean, ConstraintDate, ConstraintUUID:
		// No written arguments.
	}
	return n
}

var projConstraintKinds = map[ConstraintKind]string{
	ConstraintInteger:   "Integer",
	ConstraintFloat:     "Float",
	ConstraintBoolean:   "Boolean",
	ConstraintString:    "String",
	ConstraintEnum:      "Enum",
	ConstraintPattern:   "Pattern",
	ConstraintTimestamp: "Timestamp",
	ConstraintDate:      "Date",
	ConstraintUUID:      "UUID",
	ConstraintVector:    "Vector",
	ConstraintList:      "List",
	ConstraintAlias:     "Alias",
}

func boundText(b Bound) string {
	if b.Neg {
		return "-" + b.Text
	}
	return b.Text
}

func rawLits(lits []Literal) []string {
	if len(lits) == 0 {
		return nil
	}
	out := make([]string, len(lits))
	for i, lit := range lits {
		out[i] = lit.Raw
	}
	return out
}

func (p projector) diag(iss diag.Issue) projDiag {
	sp := iss.Span()
	return projDiag{
		Code:     iss.Code().String(),
		Severity: iss.Severity().String(),
		Msg:      iss.Message(),
		Span:     projSpan{Start: sp.Start.Byte, End: sp.End.Byte},
		Line:     lineOf(p.src, sp.Start.Byte),
		EndLine:  lineOf(p.src, sp.End.Byte),
	}
}

func (p projector) span(s location.Span) projSpan {
	return projSpan{Start: s.Start.Byte, End: s.End.Byte}
}

// lineOf returns the 1-based line of a byte offset in src.
func lineOf(src string, offset int) int {
	if offset > len(src) {
		offset = len(src)
	}
	return 1 + strings.Count(src[:offset], "\n")
}

// sortDiags orders diagnostics by start byte, then end byte, then code, so a
// golden records a set rather than an emission order.
func sortDiags(ds []projDiag) {
	sort.SliceStable(ds, func(i, j int) bool {
		if ds[i].Span.Start != ds[j].Span.Start {
			return ds[i].Span.Start < ds[j].Span.Start
		}
		if ds[i].Span.End != ds[j].Span.End {
			return ds[i].Span.End < ds[j].Span.End
		}
		return ds[i].Code < ds[j].Code
	})
}

// renderExpr renders a compiled expression to a canonical, deterministic
// S-expression string, so string equality means AST equality.
func renderExpr(e expr.Expression) string {
	if e == nil {
		return "<nil>"
	}
	switch v := e.(type) {
	case expr.SExpr:
		parts := make([]string, 0, len(v))
		parts = append(parts, v.Op())
		for _, c := range v.Children() {
			parts = append(parts, renderExpr(c))
		}
		return "(" + strings.Join(parts, " ") + ")"
	case expr.Op:
		return string(v)
	case expr.DatatypeLiteral:
		return "(dt " + string(v) + ")"
	default:
		return renderLiteral(e.Literal())
	}
}

func renderLiteral(val any) string {
	switch v := val.(type) {
	case nil:
		return "nil"
	case string:
		return strconv.Quote(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	case *regexp.Regexp:
		return "/" + v.String() + "/"
	case []expr.Expression:
		parts := make([]string, len(v))
		for i, e := range v {
			parts[i] = renderExpr(e)
		}
		return "(args " + strings.Join(parts, " ") + ")"
	case []string:
		return "(params " + strings.Join(v, " ") + ")"
	default:
		return fmt.Sprintf("<?%T>", val)
	}
}
