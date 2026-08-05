package parse

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
)

// Parse reads one schema source and returns its node tree together with every
// diagnostic found, ordered by position. The tree is never nil: a source that
// fails outright yields an empty file node and the diagnostics explaining why.
// sourceID names the source that spans belong to, and the zero SourceID is
// supported for callers with no file behind the text.
func Parse(src []byte, sourceID location.SourceID) (*File, []diag.Issue) {
	ps := mustParsers()
	text := string(src)
	toks, err := lexAll(ps, text)
	if err != nil {
		panic("parse: " + err.Error())
	}
	plex, err := lexer.Upgrade(&sliceLexer{toks: toks}, ps.elide...)
	if err != nil {
		panic("parse: " + err.Error())
	}

	b := &builder{
		ps: ps, src: text, sourceID: sourceID, toks: toks,
		lineStarts: lineStarts(text), file: &File{},
	}
	b.parseFile(plex)
	slices.SortStableFunc(b.issues, compareIssues)
	return b.file, b.issues
}

// lexAll drains the lexer into a slice, EOF token included. The builder keeps
// the slice so it can look back over a failed construct's tokens, which the
// forward-only PeekingLexer cannot do.
func lexAll(ps *parsers, src string) ([]lexer.Token, error) {
	lx, err := ps.def.LexString("", src)
	if err != nil {
		return nil, fmt.Errorf("lex source: %w", err)
	}
	var toks []lexer.Token
	for {
		t, err := lx.Next()
		if err != nil {
			return nil, fmt.Errorf("lex source: %w", err)
		}
		toks = append(toks, t)
		if t.EOF() {
			return toks, nil
		}
	}
}

// sliceLexer replays an already-lexed token slice.
type sliceLexer struct {
	toks []lexer.Token
	i    int
}

func (s *sliceLexer) Next() (lexer.Token, error) {
	if s.i >= len(s.toks) {
		return lexer.Token{Type: lexer.EOF}, nil
	}
	t := s.toks[s.i]
	s.i++
	return t, nil
}

type builder struct {
	ps       *parsers
	src      string
	sourceID location.SourceID
	toks     []lexer.Token

	// lineStarts lets positionAt binary-search rather than count newlines
	// from zero on every call, which made a parse quadratic in file size.
	lineStarts []int

	file   *File
	issues []diag.Issue
}

// lineStarts returns the byte offset of each line's first byte, starting at 0.
// It breaks on "\n", on "\r\n" as one break, and on a bare "\r", which is
// internal/source's computeLineOffsets rule — the mapper every other yammm
// position is derived from, and the one the LSP reads.
func lineStarts(src string) []int {
	starts := []int{0}
	for i := 0; i < len(src); i++ {
		switch src[i] {
		case '\n':
			starts = append(starts, i+1)
		case '\r':
			if i+1 < len(src) && src[i+1] == '\n' {
				starts = append(starts, i+2)
				i++
				continue
			}
			starts = append(starts, i+1)
		}
	}
	return starts
}

// parseFile walks the file's three regions in order: the header, the imports,
// then the declarations. Each region parses one construct at a time so a
// failure costs that construct alone.
func (b *builder) parseFile(plex *lexer.PeekingLexer) {
	b.parseHeader(plex)
	b.parseImports(plex)
	b.parseDecls(plex)
}

func (b *builder) parseHeader(plex *lexer.PeekingLexer) {
	head, err := tryParse(b.ps.schemaHead, plex)
	if err != nil {
		// A schema with no name is unusable, so the loader stops before
		// completion. This failure is fatal where other syntax errors are not.
		b.file.SchemaNameFailed = true
		b.failed(plex, err, diag.Fatal, "in the schema header", func() { b.resync(plex, declStart) })
		return
	}
	b.file.NameRaw = head.Name.Raw
	b.file.NameSpan = b.spanOf(head.Name.Pos, head.Name.EndPos)
	b.file.Doc = stripDoc(head.Doc)
	b.file.Span = b.spanOf(head.Pos, head.EndPos)

	name, ok := b.unquoteAt(&head.Name, "invalid schema name")
	if !ok {
		b.file.SchemaNameFailed = true
		return
	}
	b.file.Name = name
}

func (b *builder) parseImports(plex *lexer.PeekingLexer) {
	for {
		t := plex.Peek()
		if t.EOF() || t.Value != "import" {
			return
		}
		imp, err := tryParse(b.ps.imp, plex)
		if err != nil {
			b.failed(plex, err, diag.Error, "in an import declaration", func() {
				b.resync(plex, func(t *lexer.Token) bool {
					return t.Value == "import" || declStart(t)
				})
			})
			continue
		}
		b.addImport(imp)
	}
}

func (b *builder) addImport(imp *importNode) {
	path, ok := b.unquoteAt(&imp.Path, "invalid import path")
	if !ok {
		return
	}
	n := &Import{
		Path:     path,
		PathRaw:  imp.Path.Raw,
		PathSpan: b.spanOf(imp.Path.Pos, imp.Path.EndPos),
		Span:     b.spanOf(imp.Pos, imp.EndPos),
	}
	if imp.Alias != nil {
		n.HasAlias = true
		n.Alias = imp.Alias.value()
		n.AliasSpan = b.spanOf(imp.Alias.Pos, imp.Alias.EndPos)
	}
	b.file.Imports = append(b.file.Imports, n)
}

// parseDecls tries each declaration shape in turn. When both fail it reports
// whichever attempt reached further, which is the alternative the author was
// writing.
func (b *builder) parseDecls(plex *lexer.PeekingLexer) {
	for {
		if plex.Peek().EOF() {
			return
		}
		dt, dtErr := tryParse(b.ps.dtDecl, plex)
		if dtErr == nil {
			b.addDataType(dt)
			continue
		}
		head, headErr := tryParse(b.ps.typeHead, plex)
		if headErr != nil {
			b.failed(plex, deeperErr(dtErr, headErr), diag.Error, "in a declaration", func() {
				b.resync(plex, declStart)
			})
			continue
		}
		b.parseTypeBody(plex, head)
	}
}

func (b *builder) addDataType(dt *dtDeclNode) {
	b.file.DataTypes = append(b.file.DataTypes, &DataTypeDecl{
		Name:       dt.Name.Value,
		NameSpan:   b.spanOf(dt.Name.Pos, dt.Name.EndPos),
		Constraint: b.builtin(&dt.C, b.spanOf(dt.C.Pos, dt.C.EndPos)),
		Doc:        stripDoc(dt.Doc),
		Span:       b.spanOf(dt.Pos, dt.EndPos),
	})
}

func (b *builder) parseTypeBody(plex *lexer.PeekingLexer, head *typeHeadNode) {
	nt := &TypeDecl{
		Name:       head.Name.Value,
		NameSpan:   b.spanOf(head.Name.Pos, head.Name.EndPos),
		IsAbstract: head.Abstract,
		IsPart:     head.Part,
		Doc:        stripDoc(head.Doc),
	}
	if head.Extends != nil {
		for i := range head.Extends.Refs {
			nt.Extends = append(nt.Extends, b.typeRef(&head.Extends.Refs[i]))
		}
	}

	end := b.parseMembers(plex, nt)
	nt.Span = b.spanFromOffsets(head.Pos.Offset, end)
	b.file.Types = append(b.file.Types, nt)
}

// parseMembers consumes the type body up to its closing brace and returns the
// body's end offset. A member that fails costs only itself: the loop reports
// one diagnostic and re-syncs to the next member start.
func (b *builder) parseMembers(plex *lexer.PeekingLexer, nt *TypeDecl) int {
	for {
		t := plex.Peek()
		if t.EOF() {
			b.report(diag.Error, diag.E_SYNTAX, b.pointAt(t.Pos), "unexpected end of input in type body")
			return len(b.src)
		}
		if t.Type == b.ps.tok.rbrace {
			plex.Next()
			return t.Pos.Offset + 1
		}
		m, err := tryParse(b.ps.member, plex)
		if err != nil {
			b.failed(plex, err, diag.Error, "in a type body", func() { b.resyncMember(plex) })
			continue
		}
		b.addMember(nt, m)
	}
}

func (b *builder) addMember(nt *TypeDecl, m *memberNode) {
	switch {
	case m.Inv != nil:
		b.addInvariant(nt, m.Inv)
	case m.TAnn != nil:
		nt.Annotations = append(nt.Annotations, b.annotation(
			m.TAnn.Name, m.TAnn.Args, stripDoc(m.TAnn.Doc),
			b.spanOf(m.TAnn.Pos, m.TAnn.EndPos), 0,
		))
	case m.Assoc != nil:
		b.addAssociation(nt, m.Assoc)
	case m.Comp != nil:
		b.addComposition(nt, m.Comp)
	case m.Prop != nil:
		b.addProp(nt, m.Prop)
	}
}

func (b *builder) addAssociation(nt *TypeDecl, a *assocNode) {
	rel := &Relation{
		Kind:     RelationAssociation,
		Name:     a.Name.value(),
		NameSpan: b.spanOf(a.Name.Pos, a.Name.EndPos),
		Target:   b.typeRef(&a.Target),
		Doc:      stripDoc(a.Doc),
		Span:     b.spanOf(a.Pos, a.EndPos),
	}
	rel.Optional, rel.Many = multiplicityOf(a.Mult)
	b.applyReverse(rel, a.Reverse)
	if a.Body != nil {
		for i := range a.Body.Props {
			rel.Properties = append(rel.Properties, b.edgeProp(&a.Body.Props[i]))
		}
	}
	nt.Relations = append(nt.Relations, rel)
}

func (b *builder) addComposition(nt *TypeDecl, c *compNode) {
	name := c.Name.value()
	if name == "" {
		// A composition's name is what encodes its edge, so an unnamed one has
		// nothing to record and is dropped rather than half-built.
		b.report(diag.Error, diag.E_SYNTAX, b.spanOf(c.Pos, c.EndPos), "composition must have a name")
		return
	}
	rel := &Relation{
		Kind:     RelationComposition,
		Name:     name,
		NameSpan: b.spanOf(c.Name.Pos, c.Name.EndPos),
		Target:   b.typeRef(&c.Target),
		Doc:      stripDoc(c.Doc),
		Span:     b.spanOf(c.Pos, c.EndPos),
	}
	rel.Optional, rel.Many = multiplicityOf(c.Mult)
	b.applyReverse(rel, c.Reverse)
	nt.Relations = append(nt.Relations, rel)
}

// applyReverse fills the backward direction. With no reverse written, the edge
// is reachable optionally and singly from the far side.
func (b *builder) applyReverse(rel *Relation, rev *reverseNode) {
	if rev == nil {
		rel.ReverseOptional, rel.ReverseMany = true, false
		return
	}
	rel.Backref = rev.Name.value()
	rel.BackrefSpan = b.spanOf(rev.Name.Pos, rev.Name.EndPos)
	rel.ReverseOptional, rel.ReverseMany = multiplicityOf(rev.Mult)
}

func (b *builder) edgeProp(p *relPropNode) *Property {
	return &Property{
		Name:       p.Name.Value,
		NameSpan:   b.spanOf(p.Name.Pos, p.Name.EndPos),
		Constraint: b.constraint(&p.Type),
		IsRequired: p.Required,
		Doc:        stripDoc(p.Doc),
		Span:       b.spanOf(p.Pos, p.EndPos),
	}
}

// multiplicityOf reads the seven spellings the grammar admits. An omitted
// multiplicity means optional and single. The final return is unreachable —
// Head is "_" or "one" whenever Many is false — and exists because Go requires
// a return there.
func multiplicityOf(m *multNode) (optional, many bool) {
	if m == nil {
		return true, false
	}
	if m.Many {
		return true, true
	}
	tail := ""
	if m.Tail != nil {
		tail = *m.Tail
	}
	switch {
	case m.Head == "_" && tail == "many":
		return true, true
	case m.Head == "_":
		return true, false
	case m.Head == "one" && tail == "many":
		return false, true
	case m.Head == "one":
		return false, false
	}
	return true, false
}

func (b *builder) addProp(nt *TypeDecl, p *propNode) {
	np := &Property{
		Name:         p.Name.Value,
		NameSpan:     b.spanOf(p.Name.Pos, p.Name.EndPos),
		Constraint:   b.constraint(&p.Type),
		IsPrimaryKey: p.Primary,
		IsRequired:   p.Required,
		Doc:          stripDoc(p.Doc),
		Span:         b.spanOf(p.Pos, p.EndPos),
	}
	nameLine := b.positionAt(p.Name.Pos.Offset).Line
	for i := range p.Anns {
		a := &p.Anns[i]
		detached := 0
		if b.positionAt(a.Pos.Offset).Line > nameLine {
			detached = nameLine
		}
		np.Annotations = append(np.Annotations, b.annotation(
			a.Name, a.Args, "", b.spanOf(a.Pos, a.EndPos), detached,
		))
	}
	nt.Properties = append(nt.Properties, np)
}

func (b *builder) annotation(name lcWordNode, args *argsNode, doc string, span location.Span, detached int) *Annotation {
	na := &Annotation{
		Name:             name.Value,
		NameSpan:         b.spanOf(name.Pos, name.EndPos),
		Doc:              doc,
		DetachedFromLine: detached,
		Span:             span,
	}
	if args == nil {
		return na
	}
	na.HasParens = true
	for i := range args.Args {
		na.Args = append(na.Args, b.arg(&args.Args[i]))
	}
	return na
}

// arg classifies one annotation argument. A quoted string that will not
// unquote degrades to a literal carrying its raw spelling as Text, with Raw
// empty, and draws no diagnostic at all — no check owns an annotation
// argument. The value is preserved as written so a later phase can decide.
func (b *builder) arg(a *argNode) Arg {
	out := Arg{Span: b.spanOf(a.Pos, a.EndPos)}
	switch {
	case a.Ident != nil:
		out.Kind, out.Text = ArgIdentifier, *a.Ident
	case a.UC != nil:
		out.Kind, out.Text = ArgIdentifier, *a.UC
	case a.Lit != nil:
		out.Kind, out.Text = ArgLiteral, *a.Lit
		if isQuoted(*a.Lit) {
			if text, err := unquote(*a.Lit); err == nil {
				out.Kind, out.Text, out.Raw = ArgString, text, *a.Lit
			}
		}
	}
	return out
}

func (b *builder) addInvariant(nt *TypeDecl, inv *invNode) {
	// Unreachable: ParseTypeWith fails the whole member parse instead. The
	// two-value form is what errcheck's check-type-assertions requires.
	res, ok := inv.Expr.(exprResult)
	if !ok {
		b.report(diag.Error, diag.E_SYNTAX, b.pointAt(inv.Pos), "invariant missing expression")
		return
	}
	for _, d := range res.Diags {
		b.report(diag.Error, diag.E_INVALID_INVARIANT, b.spanFromOffsets(d.Start, d.End), d.Msg)
	}
	ni := &Invariant{
		MessageRaw:  inv.Msg.Raw,
		MessageSpan: b.spanOf(inv.Msg.Pos, inv.Msg.EndPos),
		Expr:        res.E,
		ExprSpan:    b.spanFromOffsets(res.Start, res.End),
		Doc:         stripDoc(inv.Doc),
		Span:        b.spanOf(inv.Pos, inv.EndPos),
	}
	if msg, ok := b.unquoteAt(&inv.Msg, "invalid invariant message"); ok {
		ni.Message = msg
	}
	nt.Invariants = append(nt.Invariants, ni)
}

func (b *builder) typeRef(r *typeRefNode) *TypeRef {
	out := &TypeRef{
		Name:     r.Name.Value,
		NameSpan: b.spanOf(r.Name.Pos, r.Name.EndPos),
		Span:     b.spanOf(r.Pos, r.EndPos),
	}
	if r.Qual != nil {
		out.Qualifier = *r.Qual
	}
	return out
}

// ---- recovery ----

// tryParse attempts one construct, restoring the lexer on failure so re-sync
// starts from the failed construct's first token rather than wherever the
// attempt stopped.
func tryParse[G any](p *participle.Parser[G], plex *lexer.PeekingLexer) (*G, error) {
	cp := plex.MakeCheckpoint()
	v, err := p.ParseFromLexer(plex, participle.AllowTrailing(true))
	if err != nil {
		plex.LoadCheckpoint(cp)
		return nil, err //nolint:wrapcheck // the caller inspects participle's error type to place the diagnostic
	}
	return v, nil
}

// resync consumes at least one token, then skips forward — stepping over whole
// brace-delimited bodies — until EOF or a token that starts a new declaration.
func (b *builder) resync(plex *lexer.PeekingLexer, start func(*lexer.Token) bool) {
	lb, rb := b.ps.tok.lbrace, b.ps.tok.rbrace
	depth, consumed := 0, 0
	for {
		t := plex.Peek()
		if t.EOF() {
			return
		}
		if consumed > 0 && depth <= 0 && start(t) {
			return
		}
		plex.Next()
		consumed++
		switch t.Type {
		case lb:
			depth++
		case rb:
			depth--
		}
	}
}

// resyncMember re-syncs inside a type body. It stops at the body's closing
// brace without consuming it, so the member loop is the one that closes the
// type, or at the next member start.
func (b *builder) resyncMember(plex *lexer.PeekingLexer) {
	lb, rb := b.ps.tok.lbrace, b.ps.tok.rbrace
	depth, consumed := 0, 0
	for {
		t := plex.Peek()
		if t.EOF() {
			return
		}
		if depth == 0 && t.Type == rb {
			return
		}
		if consumed > 0 && depth == 0 && b.memberStart(t) {
			return
		}
		plex.Next()
		consumed++
		switch t.Type {
		case lb:
			depth++
		case rb:
			depth--
		}
	}
}

func declStart(t *lexer.Token) bool {
	switch t.Value {
	case "type", "abstract", "part":
		return true
	}
	return strings.HasPrefix(t.Value, "/*")
}

func (b *builder) memberStart(t *lexer.Token) bool {
	switch {
	case strings.HasPrefix(t.Value, "/*"), t.Value == "@@", t.Value == "!",
		t.Value == "-->", t.Value == "*->":
		return true
	}
	return t.Type == b.ps.tok.lcWord && !reservedLC[t.Value]
}

// deeperErr picks whichever error reached further into the input, which is the
// declaration shape the author was most likely writing.
func deeperErr(a, c error) error {
	var pa, pc participle.Error
	okA, okC := errors.As(a, &pa), errors.As(c, &pc)
	switch {
	case okA && okC && pa.Position().Offset >= pc.Position().Offset:
		return a
	case okA && okC:
		return c
	case okA:
		return a
	default:
		return c
	}
}

// ---- diagnostics ----

// failed reports one diagnostic for a construct that did not parse. It
// re-syncs first, because the extent recovery skipped is the extent the failed
// construct covers, and only then does the whole of a malformed construct
// become visible. what names the construct, for the diagnostic.
func (b *builder) failed(plex *lexer.PeekingLexer, err error, sev diag.Severity, what string, resync func()) {
	from := plex.Peek().Pos.Offset
	resync()
	b.syntaxErr(sev, err, what, from, plex.Peek().Pos.Offset)
}

// syntaxErr turns a participle failure into one diagnostic. A malformed
// numeric literal anywhere in [from, to) owns the diagnosis; see the package
// doc for the construct rule and for why what replaces participle's own
// expected-set.
func (b *builder) syntaxErr(sev diag.Severity, err error, what string, from, to int) {
	if bad := b.firstInvalidNumber(from, to); bad != nil {
		b.report(sev, diag.E_SYNTAX, b.tokenSpan(bad), InvalidNumberMessage(bad.Value))
		return
	}
	var ut *participle.UnexpectedTokenError
	if errors.As(err, &ut) {
		start := ut.Unexpected.Pos.Offset
		span := b.spanFromOffsets(start, start+len(ut.Unexpected.Value))
		b.report(sev, diag.E_SYNTAX, span, fmt.Sprintf("unexpected token %q %s", ut.Unexpected, what))
		return
	}
	var pe participle.Error
	if errors.As(err, &pe) {
		at := pe.Position().Offset
		b.report(sev, diag.E_SYNTAX, b.spanFromOffsets(at, at), pe.Message())
		return
	}
	b.report(sev, diag.E_SYNTAX, location.Span{Source: b.sourceID}, err.Error())
}

// firstInvalidNumber returns the earliest malformed-numeric token starting in
// [from, to), or nil. Ties go to the first, so a construct holding more than
// one names the earliest cause.
func (b *builder) firstInvalidNumber(from, to int) *lexer.Token {
	want := b.ps.tok.invalidNumber
	i, _ := slices.BinarySearchFunc(b.toks, from, func(t lexer.Token, target int) int {
		return t.Pos.Offset - target
	})
	for ; i < len(b.toks); i++ {
		t := &b.toks[i]
		if t.EOF() || t.Pos.Offset >= to {
			return nil
		}
		if t.Type == want {
			return t
		}
	}
	return nil
}

func (b *builder) report(sev diag.Severity, code diag.Code, span location.Span, msg string) {
	b.issues = append(b.issues, diag.NewIssue(sev, code, msg).WithSpan(span).Build())
}

// reportf reports an error-severity diagnostic. Every formatted diagnostic
// this package emits is an error; the warning and fatal cases go through
// report, which is also where any computed text reaches a fatal.
func (b *builder) reportf(code diag.Code, span location.Span, format string, args ...any) {
	b.report(diag.Error, code, span, fmt.Sprintf(format, args...))
}

// compareIssues orders diagnostics by position, then by code, so a parse of
// the same source always reports them in the same order.
func compareIssues(a, c diag.Issue) int {
	if d := location.Compare(a.Span(), c.Span()); d != 0 {
		return d
	}
	return strings.Compare(a.Code().String(), c.Code().String())
}

// ---- spans ----

// spanOf builds a span between two lexer positions. Only their byte offsets are
// read: the lexer counts lines on "\n" alone, so its own line and column
// disagree with every other yammm position in a source holding a bare "\r".
func (b *builder) spanOf(start, end lexer.Position) location.Span {
	return b.spanFromOffsets(start.Offset, end.Offset)
}

// spanFromOffsets builds a span from byte offsets, deriving line and column
// from the source text. It forces start before end because
// location.RangeWithBytes panics on an inverted range and recovery can leave an
// end behind its start.
func (b *builder) spanFromOffsets(start, end int) location.Span {
	s, e := b.positionAt(start), b.positionAt(end)
	if e.Offset < s.Offset {
		e = s
	}
	return location.RangeWithBytes(b.sourceID,
		s.Line, s.Column, s.Offset,
		e.Line, e.Column, e.Offset)
}

func (b *builder) pointAt(pos lexer.Position) location.Span {
	return b.spanOf(pos, pos)
}

func (b *builder) tokenSpan(t *lexer.Token) location.Span {
	return b.spanFromOffsets(t.Pos.Offset, t.Pos.Offset+len(t.Value))
}

// positionAt derives a lexer position from a byte offset, counting columns in
// runes to match what the lexer records for a real token.
func (b *builder) positionAt(offset int) lexer.Position {
	offset = min(max(offset, 0), len(b.src))
	line, atStart := slices.BinarySearch(b.lineStarts, offset)
	if !atStart {
		line--
	}
	return lexer.Position{
		Offset: offset,
		Line:   line + 1,
		Column: 1 + utf8.RuneCountInString(b.src[b.lineStarts[line]:offset]),
	}
}

// ---- small helpers ----

// stripDoc removes a doc comment's delimiters and trims the inner content.
func stripDoc(d *string) string {
	if d == nil {
		return ""
	}
	s := *d
	if len(s) >= 4 && strings.HasPrefix(s, "/*") && strings.HasSuffix(s, "*/") {
		return strings.TrimSpace(s[2 : len(s)-2])
	}
	return s
}

// value returns the alias's spelling, whichever branch matched.
func (a *anyNameNode) value() string {
	switch {
	case a.LC != nil:
		return *a.LC
	case a.UC != nil:
		return *a.UC
	}
	return ""
}
