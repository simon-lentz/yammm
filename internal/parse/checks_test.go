package parse

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
)

// wantIssue is one expected diagnostic, stated exactly: code, severity,
// message, the byte range it anchors on and the text that range covers. start
// and end are relative to the case's base, so editing checkPrelude moves no
// expectation.
type wantIssue struct {
	code     diag.Code
	severity diag.Severity
	start    int
	end      int
	covers   string
	message  string
}

// checkPrelude wraps a property declaration in the smallest schema that holds
// it. Its length is the base every checkSource case's offsets are stated
// against, never a constant written into an expectation.
const checkPrelude = "schema \"s\"\ntype T {\n\tid String primary\n\t"

func checkSource(decl string) string { return checkPrelude + decl + "\n}\n" }

func TestChecks_ConstraintDiagnostics(t *testing.T) {
	tests := []struct {
		name string
		decl string
		want []wantIssue
	}{
		{
			name: "integer bounds inverted",
			decl: "n Integer[9, 2]",
			want: []wantIssue{{
				diag.E_INVALID_CONSTRAINT, diag.Error, 2, 15, "Integer[9, 2]",
				"integer bounds inverted: min 9 > max 2",
			}},
		},
		{
			name: "integer minus before unbounded warns on the sign alone",
			decl: "n Integer[-_, 5]",
			want: []wantIssue{{
				diag.E_INVALID_CONSTRAINT, diag.Warning, 10, 11, "-",
				"minus sign before '_' (unbounded) has no effect",
			}},
		},
		{
			name: "float minus before unbounded warns too",
			decl: "n Float[-_, 5.0]",
			want: []wantIssue{{
				diag.E_INVALID_CONSTRAINT, diag.Warning, 8, 9, "-",
				"minus sign before '_' (unbounded) has no effect",
			}},
		},
		{
			name: "float bounds inverted",
			decl: "n Float[2.5, 1.5]",
			want: []wantIssue{{
				diag.E_INVALID_CONSTRAINT, diag.Error, 2, 17, "Float[2.5, 1.5]",
				"float bounds inverted: min 2.5 > max 1.5",
			}},
		},
		{
			name: "string length bounds inverted",
			decl: "s String[9, 2]",
			want: []wantIssue{{
				diag.E_INVALID_CONSTRAINT, diag.Error, 2, 14, "String[9, 2]",
				"string length bounds inverted: min 9 > max 2",
			}},
		},
		{
			name: "string length bound out of range",
			decl: "s String[99999999999999999999, 2]",
			want: []wantIssue{{
				diag.E_INVALID_CONSTRAINT, diag.Error, 9, 29, "99999999999999999999",
				`invalid string length bound: strconv.ParseInt: parsing "99999999999999999999": value out of range`,
			}},
		},
		{
			name: "list length bounds inverted",
			decl: "l List<String>[9, 2]",
			want: []wantIssue{{
				diag.E_INVALID_CONSTRAINT, diag.Error, 2, 20, "List<String>[9, 2]",
				"list length bounds inverted: min 9 > max 2",
			}},
		},
		{
			name: "unparseable signed integer bound anchors on the sign too",
			decl: "n Integer[-99999999999999999999, 5]",
			want: []wantIssue{{
				diag.E_INVALID_CONSTRAINT, diag.Error, 10, 31, "-99999999999999999999",
				`invalid integer bound: strconv.ParseInt: parsing "-99999999999999999999": value out of range`,
			}},
		},
		{
			name: "unparseable float bound",
			decl: "n Float[1.0e999999, 5.0]",
			want: []wantIssue{{
				diag.E_INVALID_CONSTRAINT, diag.Error, 8, 18, "1.0e999999",
				`invalid float bound: strconv.ParseFloat: parsing "1.0e999999": value out of range`,
			}},
		},
		{
			name: "unparseable list length bound names the list, not the string",
			decl: "l List<String>[99999999999999999999, 2]",
			want: []wantIssue{{
				diag.E_INVALID_CONSTRAINT, diag.Error, 15, 35, "99999999999999999999",
				`invalid list length bound: strconv.ParseInt: parsing "99999999999999999999": value out of range`,
			}},
		},
		{
			name: "unparseable vector dimensions anchor on the whole constraint",
			decl: "v Vector[99999999999999999999]",
			want: []wantIssue{{
				diag.E_INVALID_CONSTRAINT, diag.Error, 2, 30, "Vector[99999999999999999999]",
				`invalid vector dimensions: strconv.Atoi: parsing "99999999999999999999": value out of range`,
			}},
		},
		{
			name: "every duplicate after the first is reported",
			decl: `e Enum["a", "a", "a"]`,
			want: []wantIssue{
				{diag.E_INVALID_CONSTRAINT, diag.Error, 2, 21, `Enum["a", "a", "a"]`, "enum must have at least two values (got 1)"},
				{diag.E_INVALID_CONSTRAINT, diag.Error, 12, 15, `"a"`, `duplicate enum value "a"`},
				{diag.E_INVALID_CONSTRAINT, diag.Error, 17, 20, `"a"`, `duplicate enum value "a"`},
			},
		},
		{
			name: "duplicate enum value is dropped and the survivor count reported",
			decl: `e Enum["a", "a"]`,
			want: []wantIssue{
				{diag.E_INVALID_CONSTRAINT, diag.Error, 2, 16, `Enum["a", "a"]`, "enum must have at least two values (got 1)"},
				{diag.E_INVALID_CONSTRAINT, diag.Error, 12, 15, `"a"`, `duplicate enum value "a"`},
			},
		},
		{
			name: "empty enum value is dropped",
			decl: `e Enum["", "b"]`,
			want: []wantIssue{
				{diag.E_INVALID_CONSTRAINT, diag.Error, 2, 15, `Enum["", "b"]`, "enum must have at least two values (got 1)"},
				{diag.E_INVALID_CONSTRAINT, diag.Error, 7, 9, `""`, "enum value cannot be empty"},
			},
		},
		{
			name: "uncompilable pattern",
			decl: `p Pattern["["]`,
			want: []wantIssue{{
				diag.E_INVALID_CONSTRAINT, diag.Error, 10, 13, `"["`,
				"invalid regex pattern \"[\": error parsing regexp: missing closing ]: `[`",
			}},
		},
		{
			name: "vector dimensions below the minimum",
			decl: "v Vector[0]",
			want: []wantIssue{{
				diag.E_INVALID_CONSTRAINT, diag.Error, 9, 10, "0",
				"vector dimensions must be at least 1 (got 0)",
			}},
		},
		{
			name: "vector dimensions above the maximum",
			decl: "v Vector[65537]",
			want: []wantIssue{{
				diag.E_INVALID_CONSTRAINT, diag.Error, 9, 14, "65537",
				"vector dimensions exceed maximum of 65536 (got 65537)",
			}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := checkSource(tc.decl)
			_, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
			assertIssues(t, src, len(checkPrelude), issues, tc.want)
		})
	}
}

// TestChecks_VectorDimensionBoundariesAreAccepted pins the accepting side of
// both caps. Rejecting cases alone leave the comparisons unpinned: turning '<'
// into '<=' and '>' into '>=' keeps every rejecting case green while the same
// schema becomes valid under one parser and invalid under the other.
func TestChecks_VectorDimensionBoundariesAreAccepted(t *testing.T) {
	tests := []struct {
		dims string
		want int
	}{
		{"1", minVectorDimensions},
		{"65536", maxVectorDimensions},
	}
	for _, tc := range tests {
		t.Run(tc.dims, func(t *testing.T) {
			src := checkSource("v Vector[" + tc.dims + "]")
			file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
			if len(issues) != 0 {
				t.Fatalf("Vector[%s] reported %v, want nothing", tc.dims, issues)
			}
			got := file.Types[0].Properties[1].Constraint.VectorDims
			if got == nil {
				t.Fatalf("Vector[%s] recorded no dimensions", tc.dims)
			}
			if *got != tc.want {
				t.Errorf("Vector[%s] recorded %d dimensions, want %d", tc.dims, *got, tc.want)
			}
		})
	}
}

// TestChecks_UnquoteFailuresAreSyntaxErrors pins the three unquote sites that
// report a syntax error rather than a constraint error: the value is not a
// string the lexer's rules can resolve, so the fault is in the text.
func TestChecks_UnquoteFailuresAreSyntaxErrors(t *testing.T) {
	tests := []struct {
		name string
		src  string
		base int
		want []wantIssue
	}{
		{
			name: "schema name",
			src:  "schema \"\\x\"\ntype T {\n\tid String primary\n}\n",
			want: []wantIssue{{
				diag.E_SYNTAX, diag.Error, 7, 11, `"\x"`,
				"invalid schema name: unquote string: invalid syntax",
			}},
		},
		{
			name: "import path",
			src:  "schema \"s\"\nimport \"\\x\"\ntype T {\n\tid String primary\n}\n",
			want: []wantIssue{{
				diag.E_SYNTAX, diag.Error, 18, 22, `"\x"`,
				"invalid import path: unquote string: invalid syntax",
			}},
		},
		{
			name: "enum value",
			src:  checkSource(`e Enum["\x", "b"]`),
			base: len(checkPrelude),
			want: []wantIssue{
				{diag.E_INVALID_CONSTRAINT, diag.Error, 2, 17, `Enum["\x", "b"]`, "enum must have at least two values (got 1)"},
				{diag.E_SYNTAX, diag.Error, 7, 11, `"\x"`, "invalid enum value: unquote string: invalid syntax"},
			},
		},
		{
			name: "pattern value",
			src:  checkSource(`p Pattern["\x"]`),
			base: len(checkPrelude),
			want: []wantIssue{{
				diag.E_SYNTAX, diag.Error, 10, 14, `"\x"`,
				"invalid pattern: unquote string: invalid syntax",
			}},
		},
		{
			name: "timestamp format",
			src:  checkSource(`t Timestamp["\x"]`),
			base: len(checkPrelude),
			want: []wantIssue{{
				diag.E_SYNTAX, diag.Error, 12, 16, `"\x"`,
				"invalid timestamp format: unquote string: invalid syntax",
			}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, issues := Parse([]byte(tc.src), location.NewSourceID("s.yammm"))
			assertIssues(t, tc.src, tc.base, issues, tc.want)
		})
	}
}

// TestChecks_UnquoteResolvesLexerEscapeVocabulary pins unquote to exactly the
// lexer's STRING escape set. The lexer admits every spelling below, so each
// must resolve — the strconv-backed predecessor rejected an unescaped '"' in
// single quotes, every \' escape, and bare \0, leaving no way to write an
// apostrophe in a single-quoted literal.
func TestChecks_UnquoteResolvesLexerEscapeVocabulary(t *testing.T) {
	accepted := []struct{ name, in, want string }{
		{"unquoted passthrough", `abc`, `abc`},
		{"double quoted", `"a"`, `a`},
		{"single quoted", `'a'`, `a`},
		{"named escapes", `"\b\t\n\f\r\0"`, "\b\t\n\f\r\x00"},
		{"hex escape", `"\x41"`, `A`},
		{"unicode escape", `"\u00e9"`, "é"},
		{"escaped double quote", `"\""`, `"`},
		{"escaped single quote in double quotes", `"\'"`, `'`},
		{"escaped single quote in single quotes", `'\''`, `'`},
		{"literal double quote in single quotes", `'a"b'`, `a"b`},
		{"literal single quote in double quotes", `"a'b"`, `a'b`},
		{"zero escape is not octal", `'\012'`, "\x0012"},
		{"valid multi-byte rune", "\"café\"", "café"},
		{"invalid utf-8 byte", "\"a\xffb\"", "a�b"},
		{"truncated utf-8 sequence", "\"a\xe2\x82b\"", "a��b"},
	}
	for _, tc := range accepted {
		t.Run(tc.name, func(t *testing.T) {
			got, err := unquote(tc.in)
			if err != nil {
				t.Fatalf("unquote(%q) = error %v, want %q", tc.in, err, tc.want)
			}
			if got != tc.want {
				t.Errorf("unquote(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}

	rejected := []struct{ name, in string }{
		{"bare x escape", `"\x"`},
		{"one hex digit", `"\x4"`},
		{"bad hex digits", `"\xZZ"`},
		{"short unicode escape", `"\u12"`},
		{"surrogate rune", `"\uD800"`},
		{"unknown escape", `"\q"`},
		{"trailing backslash", `"\"`},
	}
	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := unquote(tc.in); err == nil {
				t.Errorf("unquote(%q) succeeded, want error", tc.in)
			} else if err.Error() != "unquote string: invalid syntax" {
				t.Errorf("unquote(%q) error = %q, want the pinned message", tc.in, err)
			}
		})
	}
}

// TestChecks_SingleQuotedLiteralsLoadAtEveryUnquoteSite pins the loosening at
// the parse level: a single-quoted literal carrying an unescaped '"' or a \'
// escape reaches every unquote site and loads without a diagnostic.
func TestChecks_SingleQuotedLiteralsLoadAtEveryUnquoteSite(t *testing.T) {
	src := "schema 'sc\"hema'\n" +
		"type T {\n" +
		"\tid String primary\n" +
		"\tkind Enum['a\"b', 'don\\'t']\n" +
		"\tcode Pattern['^x\"?$']\n" +
		"\tts Timestamp['2006-01-02T15:04:05Z07:00']\n" +
		"}\n"
	_, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
	if len(issues) != 0 {
		t.Fatalf("single-quoted spellings drew %d diagnostics, want none: %v", len(issues), issues)
	}
}

// TestChecks_SchemaNameUnquoteFailureMarksTheFileFailed pins the shape a later
// phase depends on: a header that parses but whose literal will not unquote
// leaves no usable schema name, exactly as a header that did not parse at all.
func TestChecks_SchemaNameUnquoteFailureMarksTheFileFailed(t *testing.T) {
	file, _ := Parse([]byte("schema \"\\x\"\ntype T {\n\tid String primary\n}\n"), location.SourceID{})
	if !file.SchemaNameFailed {
		t.Error("SchemaNameFailed = false, want true")
	}
	if file.Name != "" {
		t.Errorf("Name = %q, want empty", file.Name)
	}
	if file.NameRaw != `"\x"` {
		t.Errorf("NameRaw = %q, want the source spelling", file.NameRaw)
	}
	if len(file.Types) != 1 {
		t.Errorf("Types = %d, want 1 — the rest of the file must still parse", len(file.Types))
	}
}

// TestChecks_CarriedValuesNeedNoReparse pins that a constraint reaches its
// consumer with the values already computed, so nothing downstream re-reads a
// spelling.
func TestChecks_CarriedValuesNeedNoReparse(t *testing.T) {
	src := checkSource("n Integer[-3, 7]") +
		"type U {\n\tid String primary\n\tf Float[-1.5, _]\n\ts String[2, _]\n\tt Timestamp[\"2006\"]\n}\n"
	file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
	if len(issues) != 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}

	n := file.Types[0].Properties[1].Constraint
	if n.IntMin == nil || *n.IntMin != -3 || n.IntMax == nil || *n.IntMax != 7 {
		t.Errorf("integer bounds = %v..%v, want -3..7", n.IntMin, n.IntMax)
	}
	if n.Bounds.Min.Text != "3" || !n.Bounds.Min.Neg {
		t.Errorf("min bound spelling = %q neg=%v, want 3 and true", n.Bounds.Min.Text, n.Bounds.Min.Neg)
	}

	u := file.Types[1]
	f := u.Properties[1].Constraint
	if f.FloatMin == nil || *f.FloatMin != -1.5 || f.FloatMax != nil {
		t.Errorf("float bounds = %v..%v, want -1.5..unbounded", f.FloatMin, f.FloatMax)
	}
	s := u.Properties[2].Constraint
	if s.LenMin == nil || *s.LenMin != 2 || s.LenMax != nil {
		t.Errorf("string length = %v..%v, want 2..unbounded", s.LenMin, s.LenMax)
	}
	if got, ok := u.Properties[3].Constraint.Format(); !ok || got != "2006" {
		t.Errorf("timestamp format = %q ok=%v, want 2006", got, ok)
	}
}

// TestChecks_MinusBoundSpanCoversTheSign pins the anchor an unparseable signed
// bound reports on: the sign belongs to the bound, so a reader is pointed at
// the whole thing.
func TestChecks_MinusBoundSpanCoversTheSign(t *testing.T) {
	src := checkSource("n Integer[-9, -2]")
	file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
	if len(issues) != 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	b := file.Types[0].Properties[1].Constraint.Bounds
	if got := src[b.Min.Span.Start.Byte:b.Min.Span.End.Byte]; got != "-9" {
		t.Errorf("min bound span covers %q, want %q", got, "-9")
	}
	if got := src[b.Max.Span.Start.Byte:b.Max.Span.End.Byte]; got != "-2" {
		t.Errorf("max bound span covers %q, want %q", got, "-2")
	}
}

// assertIssues compares against expectations stated relative to base, and
// checks the text each span covers as well as its extent, so a case that is
// off by a constant fails on the text rather than reading as correct.
func assertIssues(t *testing.T, src string, base int, got []diag.Issue, want []wantIssue) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("got %d issues, want %d", len(got), len(want))
		for _, iss := range got {
			t.Logf("  got: %s %s [%d,%d) %s", iss.Code(), iss.Severity(),
				iss.Span().Start.Byte-base, iss.Span().End.Byte-base, iss.Message())
		}
		return
	}
	for i, w := range want {
		g, wantStart, wantEnd := got[i], base+w.start, base+w.end
		if g.Code() != w.code {
			t.Errorf("issue %d code = %s, want %s", i, g.Code(), w.code)
		}
		if g.Severity() != w.severity {
			t.Errorf("issue %d severity = %s, want %s", i, g.Severity(), w.severity)
		}
		if g.Message() != w.message {
			t.Errorf("issue %d message = %q, want %q", i, g.Message(), w.message)
		}
		if g.Span().Start.Byte != wantStart || g.Span().End.Byte != wantEnd {
			t.Errorf("issue %d span = [%d,%d), want [%d,%d) — offsets relative to base %d",
				i, g.Span().Start.Byte-base, g.Span().End.Byte-base, w.start, w.end, base)
			continue
		}
		if covered := src[wantStart:wantEnd]; covered != w.covers {
			t.Errorf("issue %d span covers %q, want %q", i, covered, w.covers)
		}
	}
}

// TestChecks_ArityRulesAreChecksNotGrammar pins that the two arity rules report
// the rule rather than a punctuation complaint. The grammar admits the shape so
// the promoted check is reachable by the spelling a user actually writes;
// rejecting it in the grammar left only "unexpected token".
func TestChecks_ArityRulesAreChecksNotGrammar(t *testing.T) {
	for _, tc := range []struct{ name, prop, want string }{
		{"one enum value", "e Enum[\"a\"]", "enum must have at least two values (got 1)"},
		{"no enum values", "e Enum[]", "enum must have at least two values (got 0)"},
		{"three patterns", "p Pattern[\"a\", \"b\", \"c\"]", "pattern constraint exceeds maximum of 2 patterns (got 3)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "schema \"s\"\ntype T {\n\tid String primary\n\t" + tc.prop + "\n}\n"
			_, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
			if len(issues) != 1 {
				t.Fatalf("got %d issues, want 1: %v", len(issues), issues)
			}
			if issues[0].Code() != diag.E_INVALID_CONSTRAINT || issues[0].Message() != tc.want {
				t.Errorf("got %s %q, want E_INVALID_CONSTRAINT %q",
					issues[0].Code(), issues[0].Message(), tc.want)
			}
			// The oracle anchors both rules on the whole constraint; an anchor
			// on one value underlines a region the rule is not about.
			want := tc.prop[strings.Index(tc.prop, " ")+1:]
			if got := src[issues[0].Span().Start.Byte:issues[0].Span().End.Byte]; got != want {
				t.Errorf("anchored on %q, want the whole constraint %q", got, want)
			}
		})
	}
}

// TestChecks_FloatBoundsCarryTheWrittenSpelling covers fltBoundsOf, whose whole
// output no other test reads. Bounds is the only record of what the author
// wrote and of the extent the minus sign covers, so a consumer that reads it
// gets nil — or a swapped pair — with every other assertion green.
func TestChecks_FloatBoundsCarryTheWrittenSpelling(t *testing.T) {
	src := checkSource("f Float[-1.5, 2.5]")
	file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
	if len(issues) != 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	bounds := file.Types[0].Properties[1].Constraint.Bounds
	if bounds == nil {
		t.Fatal("Float constraint carries no written bounds")
	}
	if bounds.Min.Text != "1.5" || !bounds.Min.Neg {
		t.Errorf("min = %+v, want the written 1.5 with its sign", bounds.Min)
	}
	if bounds.Max.Text != "2.5" || bounds.Max.Neg {
		t.Errorf("max = %+v, want the written 2.5 unsigned", bounds.Max)
	}
	if got := src[bounds.Min.Span.Start.Byte:bounds.Min.Span.End.Byte]; got != "-1.5" {
		t.Errorf("min span covers %q, want the sign too", got)
	}
}

// TestChecks_PatternListRejectsATrailingComma pins the grammar against the
// generated one. patternT carries no trailing COMMA, so admitting one accepts
// source production refuses — a schema that loads here and not there.
func TestChecks_PatternListRejectsATrailingComma(t *testing.T) {
	for _, prop := range []string{`p Pattern["a", "b",]`, `p Pattern["a",]`} {
		src := "schema \"s\"\ntype T {\n\tid String primary\n\t" + prop + "\n}\n"
		_, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
		if len(issues) != 1 || issues[0].Code() != diag.E_SYNTAX {
			t.Errorf("%s: got %v, want one E_SYNTAX", prop, issues)
		}
	}
}

// TestChecks_PatternArityCountsWhatCompiles pins the count against the
// generated parser's, which appends only patterns that unquote and compile. A
// count of written literals reports an arity error production never raises, and
// a pattern past the cap keeps its own diagnostic either way.
func TestChecks_PatternArityCountsWhatCompiles(t *testing.T) {
	for _, tc := range []struct {
		name, prop string
		wantCodes  []diag.Code
	}{
		{
			"an uncompilable third pattern leaves two survivors",
			`p Pattern["a", "b", "("]`,
			[]diag.Code{diag.E_INVALID_CONSTRAINT},
		},
		{
			"an unquotable middle pattern leaves two survivors",
			`p Pattern["a", "\\x", "b"]`,
			[]diag.Code{diag.E_INVALID_CONSTRAINT},
		},
		{
			"three that compile is three",
			`p Pattern["a", "b", "c"]`,
			[]diag.Code{diag.E_INVALID_CONSTRAINT},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "schema \"s\"\ntype T {\n\tid String primary\n\t" + tc.prop + "\n}\n"
			_, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
			if len(issues) != len(tc.wantCodes) {
				t.Fatalf("got %d issues, want %d: %v", len(issues), len(tc.wantCodes), issues)
			}
			for i, want := range tc.wantCodes {
				if issues[i].Code() != want {
					t.Errorf("issue %d = %s, want %s", i, issues[i].Code(), want)
				}
			}
		})
	}
}

// TestChecks_EveryWrittenPatternKeepsItsLiteral pins node.go's Kept contract at
// the cap: a consumer reading PatternLits sees everything the author wrote, and
// Kept is what says which of them the constraint enforces.
func TestChecks_EveryWrittenPatternKeepsItsLiteral(t *testing.T) {
	src := "schema \"s\"\ntype T {\n\tid String primary\n\tp Pattern[\"^a$\", \"^b$\", \".*\"]\n}\n"
	file, _ := Parse([]byte(src), location.NewSourceID("s.yammm"))
	c := file.Types[0].Properties[1].Constraint
	if got := len(c.PatternLits); got != 3 {
		t.Fatalf("PatternLits = %d, want all 3 written literals", got)
	}
	for i, want := range []bool{true, true, false} {
		if c.PatternLits[i].Kept != want {
			t.Errorf("literal %d (%s) Kept = %v, want %v", i, c.PatternLits[i].Raw, c.PatternLits[i].Kept, want)
		}
	}
	res := c.PatternRegexps()
	if len(res) != 2 || res[0].String() != "^a$" || res[1].String() != "^b$" {
		t.Errorf("enforced patterns = %v, want the first two written", res)
	}
}

// TestChecks_PatternListIsTruncatedAtTheCap pins that a rejected pattern list
// does not reach the constraint. The grammar now admits any number, so without
// the truncation a caller compiling PatternRegexps would enforce patterns the
// constraint reported as too many.
func TestChecks_PatternListIsTruncatedAtTheCap(t *testing.T) {
	src := "schema \"s\"\ntype T {\n\tid String primary\n\tp Pattern[\"a\", \"b\", \"c\", \"d\"]\n}\n"
	file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1: %v", len(issues), issues)
	}
	c := file.Types[0].Properties[1].Constraint
	if got := len(c.PatternRegexps()); got != maxPatterns {
		t.Errorf("PatternRegexps() = %d, want %d — the list past the cap must not reach the constraint",
			got, maxPatterns)
	}
}
