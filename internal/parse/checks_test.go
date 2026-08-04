package parse

import (
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
