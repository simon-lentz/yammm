package parse

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/alecthomas/participle/v2/lexer"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
)

// TestRecovery_IndependentDefectsAllReport pins the property the whole
// recovery loop exists for: one broken member costs that member alone, so a
// file with several unrelated mistakes reports all of them in one pass.
func TestRecovery_IndependentDefectsAllReport(t *testing.T) {
	src := `schema "s"
type A {
	id String primary
	x @
}
type B {
	id String primary
	y @
}
type C {
	id String primary
}
`
	file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
	if len(issues) != 2 {
		t.Errorf("got %d issues, want 2", len(issues))
		for _, iss := range issues {
			t.Logf("  %s [%d,%d) %s", iss.Code(), iss.Span().Start.Byte, iss.Span().End.Byte, iss.Message())
		}
	}
	if len(file.Types) != 3 {
		t.Fatalf("got %d types, want 3 — a broken member must not lose its type", len(file.Types))
	}
	for _, want := range []string{"A", "B", "C"} {
		if !hasType(file, want) {
			t.Errorf("type %s did not survive recovery", want)
		}
	}
	if got := len(file.Types[2].Properties); got != 1 {
		t.Errorf("type C has %d properties, want 1 — a later type must be untouched", got)
	}
}

// TestRecovery_GoodMembersSurviveABadOne pins that recovery re-syncs to the
// next member rather than abandoning the body.
func TestRecovery_GoodMembersSurviveABadOne(t *testing.T) {
	src := "schema \"s\"\ntype T {\n\tid String primary\n\tbroken @\n\tname String\n\tage Integer\n}\n"
	file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
	if len(issues) != 1 {
		t.Errorf("got %d issues, want 1", len(issues))
	}
	props := file.Types[0].Properties
	var names []string
	for _, p := range props {
		names = append(names, p.Name)
	}
	if strings.Join(names, ",") != "id,name,age" {
		t.Errorf("surviving properties = %v, want id,name,age", names)
	}
}

// TestRecovery_MalformedNumberOwnsTheDiagnosis pins the override: whatever
// token the parser stopped on, a malformed numeric literal inside the failed
// construct is what gets reported.
func TestRecovery_MalformedNumberOwnsTheDiagnosis(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		wantText string
		wantAt   string
	}{
		{
			name:     "in a constraint bound, where the parser stops before the literal",
			src:      "schema \"s\"\ntype T {\n\tid String primary\n\tn Integer[0x10, 5]\n}\n",
			wantText: "0x10",
			wantAt:   "0x10",
		},
		{
			name:     "in an expression, where the parser stops on the literal",
			src:      "schema \"s\"\ntype T {\n\tid String primary\n\t! \"m\" 42abc > 1\n}\n",
			wantText: "42abc",
			wantAt:   "42abc",
		},
		{
			name:     "the first of two in one construct owns it",
			src:      "schema \"s\"\ntype T {\n\tid String primary\n\tn Integer[0x10, 0x20]\n}\n",
			wantText: "0x10",
			wantAt:   "0x10",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, issues := Parse([]byte(tc.src), location.NewSourceID("s.yammm"))
			if len(issues) != 1 {
				t.Fatalf("got %d issues, want 1: %v", len(issues), issues)
			}
			iss := issues[0]
			if want := InvalidNumberMessage(tc.wantText); iss.Message() != want {
				t.Errorf("message = %q, want %q", iss.Message(), want)
			}
			if got := tc.src[iss.Span().Start.Byte:iss.Span().End.Byte]; got != tc.wantAt {
				t.Errorf("anchored on %q, want %q", got, tc.wantAt)
			}
		})
	}
}

// TestRecovery_SpansAreNeverInverted walks a malformed corpus and asserts every
// span it produces stays inside the source. No source here reaches spanOf with
// an end behind its start, so the clamp that keeps location.RangeWithBytes from
// panicking is covered by TestRecovery_SpanOfClampsAnInvertedPair instead.
func TestRecovery_SpansAreNeverInverted(t *testing.T) {
	sources := []string{
		"", "{", "}", "schema", "schema \"s\"\ntype", "schema \"s\"\ntype T {",
		"schema \"s\"\ntype T {\n\tid\n", "schema \"s\"\ntype T {\n\t}\n\tx String\n}",
		"schema \"s\"\ntype T {\n\t! \"m\"\n}", "schema \"s\"\ntype T {\n\tn Integer[\n}",
		"schema \"s\"\nimport", "schema \"s\"\nimport \"a\" as", "@@@@", "))))",
		"schema \"s\"\ntype T {\n\tv Vector[\n}\n", "schema \"s\"\ntype T {\n\tl List<\n}\n",
		// Relation, extends, annotation-argument and alias shapes: without
		// these the walk below descends into nothing.
		"schema \"s\"\ntype T extends A, {\n\t--> r B\n}\n",
		"schema \"s\"\ntype T extends a.B {\n\t--> r C /back { w Integer }\n\t*-> c D\n}\n",
		"schema \"s\"\ntype T {\n\t--> r\n}\n",
		"schema \"s\"\ntype T {\n\t--> r B /\n}\n",
		"schema \"s\"\ntype T {\n\t*-> c\n}\n",
		"schema \"s\"\ntype T {\n\t--> r B { w Enum[\"a\",\"b\"] required }\n}\n",
		"schema \"s\"\ntype T {\n\tid String primary @a(1, \"x\") @b\n\t@@t(y)\n}\n",
		"schema \"s\"\nimport \"a.yammm\" as al\ntype C = Pattern[\"^a$\"]\ntype T {\n\tx al.Thing\n\ty List<String>[1, 2]\n\tz Timestamp[\"2006\"]\n}\n",
	}
	for _, src := range sources {
		t.Run(src, func(t *testing.T) {
			file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
			assertSpansWithinSource(t, src, issues)
			assertNodeSpansWithinSource(t, src, file)
		})
	}
}

// TestRecovery_MembersAfterAFailedOneSurvive pins the property the whole
// resync rule exists for: a member the author wrote reaches the tree unless it
// is the one that failed. Every row here lost at least one member to an earlier
// rule that keyed on token kind rather than on where the line begins.
func TestRecovery_MembersAfterAFailedOneSurvive(t *testing.T) {
	tests := []struct {
		what  string
		src   string
		props []string
		rels  []string
		anns  int
		invs  int
	}{
		{
			"a failed relation keeps every property after it",
			"schema \"s\"\ntype T {\n\tid String primary\n\t--> owner (bogus) Person\n\tname String\n\tage Integer\n}\n",
			[]string{"id", "name", "age"},
			nil, 0, 0,
		},
		{
			"a bare name keeps the properties after it",
			"schema \"s\"\ntype T {\n\tid String primary\n\tbad\n\tcolor String\n\tsize Integer\n}\n",
			[]string{"id", "color", "size"},
			nil, 0, 0,
		},
		{
			"a doc comment on the failed member costs no extra member",
			"schema \"s\"\ntype T {\n\tid String primary\n\t/* doc */ bad\n\tcolor String\n\tsize Integer\n}\n",
			[]string{"id", "color", "size"},
			nil, 0, 0,
		},
		{
			"an unterminated type argument keeps the next property",
			"schema \"s\"\ntype T {\n\tid String primary\n\tx List<String\n\tcolor String\n}\n",
			[]string{"id", "color"},
			nil, 0, 0,
		},
		{
			"a rejected bound keeps the properties after it",
			"schema \"s\"\ntype T {\n\tid String primary\n\tn Integer[1.5, 3]\n\tzz String\n\tww Integer\n}\n",
			[]string{"id", "n", "zz", "ww"},
			nil, 0, 0,
		},
		{
			"a failed member keeps an association after it",
			"schema \"s\"\ntype T {\n\tid String primary\n\tbad\n\t--> owner Person\n\tname String\n}\n",
			[]string{"id", "name"},
			[]string{"owner"},
			0, 0,
		},
		{
			"a failed member keeps a composition after it",
			"schema \"s\"\ntype T {\n\tid String primary\n\tbad\n\t*-> parts Part\n\tname String\n}\n",
			[]string{"id", "name"},
			[]string{"parts"},
			0, 0,
		},
		{
			"a failed member keeps a doc-commented member after it",
			"schema \"s\"\ntype T {\n\tid String primary\n\tbad\n\t/* d */ color String\n\tname String\n}\n",
			[]string{"id", "color", "name"},
			nil, 0, 0,
		},
		{
			"a failed member keeps a type annotation after it",
			"schema \"s\"\ntype T {\n\tid String primary\n\tbad\n\t@@audit\n\tname String\n}\n",
			[]string{"id", "name"},
			nil, 1, 0,
		},
		{
			"a failed member keeps an invariant after it",
			"schema \"s\"\ntype T {\n\tid String primary\n\tbad\n\t! \"m\" id != \"\"\n\tname String\n}\n",
			[]string{"id", "name"},
			nil, 0, 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			file, issues := Parse([]byte(tc.src), location.NewSourceID("s.yammm"))
			if len(issues) != 1 {
				t.Errorf("got %d diagnostics, want 1: %v", len(issues), issues)
			}
			if len(file.Types) != 1 {
				t.Fatalf("got %d types, want 1", len(file.Types))
			}
			var got []string
			for _, p := range file.Types[0].Properties {
				got = append(got, p.Name)
			}
			if !slices.Equal(got, tc.props) {
				t.Errorf("properties = %v, want %v", got, tc.props)
			}
			var gotRels []string
			for _, r := range file.Types[0].Relations {
				gotRels = append(gotRels, r.Name)
			}
			if !slices.Equal(gotRels, tc.rels) {
				t.Errorf("relations = %v, want %v", gotRels, tc.rels)
			}
			if got := len(file.Types[0].Annotations); got != tc.anns {
				t.Errorf("type annotations = %d, want %d", got, tc.anns)
			}
			if got := len(file.Types[0].Invariants); got != tc.invs {
				t.Errorf("invariants = %d, want %d", got, tc.invs)
			}
		})
	}
}

// TestRecovery_AModifierDoesNotRestartRecovery pins the other direction: a
// modifier inside the failed member is not a member start, so one defect still
// draws one diagnostic.
func TestRecovery_AModifierDoesNotRestartRecovery(t *testing.T) {
	for _, src := range []string{
		"schema \"s\"\ntype T {\n\tid String primary\n\tx 123 primary\n}\n",
		"schema \"s\"\ntype T {\n\tid String primary\n\tx 123 required\n}\n",
	} {
		_, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
		if len(issues) != 1 {
			t.Errorf("%q: got %d diagnostics, want 1: %v", src, len(issues), issues)
		}
	}
}

// TestRecovery_HeaderFailureOwnsItsOwnDiagnostic pins that the header's
// malformed-numeric window ends at the failure. Its recovery runs to the first
// declaration, so a literal in any construct it skips must not take over the
// diagnostic — nor stamp the header's Fatal severity on an unrelated token.
func TestRecovery_HeaderFailureOwnsItsOwnDiagnostic(t *testing.T) {
	src := "schema\n\ty Integer[0x10, 5]\n"
	_, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
	if len(issues) == 0 {
		t.Fatal("no diagnostic")
	}
	if got := issues[0].Message(); !strings.Contains(got, "schema header") {
		t.Errorf("first diagnostic = %q, want the header failure", got)
	}
	if got := issues[0].Span().Start.Byte; got != strings.Index(src, "y") {
		t.Errorf("anchored at %d, want the failing token at %d", got, strings.Index(src, "y"))
	}
}

// TestRecovery_DeclarationResyncHasAByteFloor covers the floor on the
// declaration-level resync, the twin of the one inside a type body. Without it
// recovery restarts before the failed construct and reports it twice.
func TestRecovery_DeclarationResyncHasAByteFloor(t *testing.T) {
	src := "/* d */ @\nA "
	_, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
	if len(issues) != 1 {
		t.Errorf("got %d diagnostics, want 1: %v", len(issues), issues)
	}
}

// TestRecovery_DeclarationLevelRegexIsNotADocComment is the declaration-level
// twin of the member-level rule: "/*/" lexes as REGEXP, and a text-prefix test
// halts declaration resync on it and re-reports the same construct.
func TestRecovery_DeclarationLevelRegexIsNotADocComment(t *testing.T) {
	src := "schema \"s\"\ntype A = \n/*/ \ntype B {\n\tid String primary\n}\n"
	file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
	if len(issues) != 1 {
		t.Errorf("got %d diagnostics, want 1: %v", len(issues), issues)
	}
	if len(file.Types) != 1 || file.Types[0].Name != "B" {
		t.Errorf("types = %v, want B alone", file.Types)
	}
}

// TestRecovery_ResumeFloorFallsBackToTheConstructStart covers the branch an
// error carrying no position takes. Returning 0 there collapses the floor and
// lets recovery restart before the construct that failed.
func TestRecovery_ResumeFloorFallsBackToTheConstructStart(t *testing.T) {
	const from = 42
	if got := resumeFloor(errors.New("no position"), from); got != from {
		t.Errorf("resumeFloor(plain error, %d) = %d, want %d", from, got, from)
	}
}

// TestRecovery_TokenSpanAtCollapsesInsideAToken covers the fallback for an
// offset that is not a token start. Underlining the next token instead would
// highlight a region the diagnostic is not about.
func TestRecovery_TokenSpanAtCollapsesInsideAToken(t *testing.T) {
	src := "schema \"s\"\ntype T {\n\tid String primary\n}\n"
	b := &builder{
		ps:         mustParsers(),
		src:        src,
		sourceID:   location.NewSourceID("s.yammm"),
		lineStarts: lineStarts(src),
		toks:       lexTokens(t, src),
	}
	inside := strings.Index(src, "String") + 2

	got := b.tokenSpanAt(inside)

	if got.Start.Byte != inside || got.End.Byte != inside {
		t.Errorf("tokenSpanAt(%d) = [%d,%d), want it collapsed to a point there",
			inside, got.Start.Byte, got.End.Byte)
	}
}

// TestRecovery_SpanOfClampsAnInvertedPair covers the clamp no source reaches.
// location.RangeWithBytes panics when an end precedes its start, so a clamp
// that stops working takes Parse — and an LSP server on a keystroke — down
// with it.
func TestRecovery_SpanOfClampsAnInvertedPair(t *testing.T) {
	src := "schema \"s\"\ntype T {\n\tid String primary\n}\n"
	b := &builder{
		src:        src,
		sourceID:   location.NewSourceID("s.yammm"),
		lineStarts: lineStarts(src),
	}
	const start, end = 30, 4

	got := b.spanOf(b.positionAt(start), b.positionAt(end))

	if got.Start.Byte != start || got.End.Byte != start {
		t.Errorf("spanOf(%d, %d) = [%d,%d), want it collapsed to [%d,%d)",
			start, end, got.Start.Byte, got.End.Byte, start, start)
	}
}

// TestRecovery_ErrorWithoutAPositionIsStillReported covers syntaxErr's last
// branch, which no source reaches: every failure the parsers raise implements
// participle.Error. Deleting the branch would drop the diagnostic for a foreign
// error while recovery still skipped the construct, which is silent acceptance.
func TestRecovery_ErrorWithoutAPositionIsStillReported(t *testing.T) {
	src := "schema \"s\"\ntype T {\n\tid String primary\n}\n"
	from := strings.Index(src, "id")
	b := &builder{
		ps:         mustParsers(),
		src:        src,
		sourceID:   location.NewSourceID("s.yammm"),
		lineStarts: lineStarts(src),
		toks:       lexTokens(t, src),
	}

	b.syntaxErr(diag.Error, errors.New("lexer stopped: bad rune"), "in a type body", from, len(src))

	if len(b.issues) != 1 {
		t.Fatalf("got %d issues, want 1", len(b.issues))
	}
	got := b.issues[0]
	if got.Code() != diag.E_SYNTAX || got.Severity() != diag.Error {
		t.Errorf("got %s/%v, want E_SYNTAX/error", got.Code(), got.Severity())
	}
	if got.Message() != "lexer stopped: bad rune" {
		t.Errorf("message = %q, want the error's own text", got.Message())
	}
	if covered := src[got.Span().Start.Byte:got.Span().End.Byte]; covered != "id" {
		t.Errorf("span covers %q, want the construct's first token %q", covered, "id")
	}
}

// lexTokens returns the un-elided token slice Parse builds, so a test can drive
// a builder method that reads it.
func lexTokens(t *testing.T, src string) []lexer.Token {
	t.Helper()
	toks, err := lexAll(mustParsers(), src)
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	return toks
}

// TestRecovery_PositionAtClampsAnOutOfRangeOffset covers the guard that keeps
// positionAt from slicing past the source. Every offset-derived span funnels
// through it, so an unclamped one indexes b.src out of range and panics out of
// Parse rather than returning a position.
func TestRecovery_PositionAtClampsAnOutOfRangeOffset(t *testing.T) {
	src := "schema \"s\"\ntype T {\n\tid String primary\n}\n"
	b := &builder{
		src:        src,
		sourceID:   location.NewSourceID("s.yammm"),
		lineStarts: lineStarts(src),
	}
	for _, offset := range []int{-1, len(src), len(src) + 100} {
		got := b.positionAt(offset)
		if got.Offset < 0 || got.Offset > len(src) {
			t.Errorf("positionAt(%d).Offset = %d, want it inside [0,%d]", offset, got.Offset, len(src))
		}
		if got.Line < 1 || got.Column < 1 {
			t.Errorf("positionAt(%d) = %d:%d, want a 1-based position", offset, got.Line, got.Column)
		}
	}
}

// TestRecovery_UnclosedBodyReportsAtEOF pins that an unterminated type body is
// reported rather than silently accepted.
func TestRecovery_UnclosedBodyReportsAtEOF(t *testing.T) {
	src := "schema \"s\"\ntype T {\n\tid String primary\n"
	file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
	if len(issues) != 1 || issues[0].Code() != diag.E_SYNTAX {
		t.Fatalf("got %v, want one E_SYNTAX", issues)
	}
	if issues[0].Span().Start.Byte != len(src) {
		t.Errorf("anchored at %d, want end of input %d", issues[0].Span().Start.Byte, len(src))
	}
	if len(file.Types) != 1 || len(file.Types[0].Properties) != 1 {
		t.Error("the recovered type lost its content")
	}
}

// TestRecovery_LookaheadIsNotWiderThanTwo catches a widened lookahead only.
// The source keeps its property and loses a diagnostic at UseLookahead(3), and
// this goes red there — but it stays green at 1, so it is a one-sided guard
// and not a pin on the value.
func TestRecovery_LookaheadIsNotWiderThanTwo(t *testing.T) {
	src := "schema \"s\"\ntype T {\n\tid String primary @a(x,\n}\n"
	file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
	if len(issues) != 1 {
		t.Errorf("got %d diagnostics, want 1 — the lookahead window widened", len(issues))
	}
	if len(file.Types) != 1 {
		t.Fatalf("got %d types, want 1", len(file.Types))
	}
	if got := len(file.Types[0].Properties); got != 0 {
		t.Errorf("recorded %d properties, want 0", got)
	}
}

// TestRecovery_ImportFailureIsAnError covers the second of the loop's four
// recovery sites, in order: schema header, import declaration, declaration,
// type body. A downgrade here would let a malformed import load clean, and a
// resync that did not stop on "import" would swallow the one that follows.
func TestRecovery_ImportFailureIsAnError(t *testing.T) {
	src := "schema \"s\"\nimport 123\nimport \"b.yammm\" as bee\ntype T {\n\tid String primary\n}\n"
	file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1: %v", len(issues), issues)
	}
	if issues[0].Code() != diag.E_SYNTAX || issues[0].Severity() != diag.Error {
		t.Errorf("got %s at %v, want E_SYNTAX at error", issues[0].Code(), issues[0].Severity())
	}
	if want := `unexpected token "123" in an import declaration`; issues[0].Message() != want {
		t.Errorf("message = %q, want %q", issues[0].Message(), want)
	}
	if len(file.Imports) != 1 || file.Imports[0].Path != "b.yammm" {
		t.Errorf("imports = %+v, want the well-formed b.yammm recovered", file.Imports)
	}

	// The same defect with nothing after it: recovery has no following import
	// to stop at, and must still report rather than run off the end quietly.
	_, atEOF := Parse([]byte("schema \"s\"\nimport 123\n"), location.NewSourceID("s.yammm"))
	if len(atEOF) != 1 || atEOF[0].Code() != diag.E_SYNTAX {
		t.Errorf("malformed import at end of file gave %v, want one E_SYNTAX", atEOF)
	}
}

// TestRecovery_UnterminatedGroupsAreReported pins that a delimited construct
// rejects a missing closing token. Every closer below can be made optional with
// nothing else in the package going red, and the malformed source then loads
// with no diagnostic at all. The counts, anchors and survivor tallies are
// measured behaviour rather than a target; a remedy that improves them updates
// these rows.
func TestRecovery_UnterminatedGroupsAreReported(t *testing.T) {
	tests := []struct {
		name        string
		member      string
		wantProps   int
		wantIssues  int
		wantAnchor  string
		wantBareAnn string // the annotation name a discarded argument list leaves behind
	}{
		{name: "length bounds", member: "s String[1, 2", wantIssues: 1, wantAnchor: "}"},
		{name: "numeric bounds", member: "n Integer[1, 2", wantIssues: 1, wantAnchor: "}"},
		{name: "enum values", member: "e Enum[\"a\", \"b\"", wantIssues: 1, wantAnchor: "}"},
		{name: "pattern list", member: "p Pattern[\"^a$\"", wantIssues: 1, wantAnchor: "}"},
		{name: "vector dimensions", member: "v Vector[128", wantIssues: 1, wantAnchor: "}"},
		{name: "list element", member: "l List<String", wantIssues: 1, wantAnchor: "}"},
		// The property survives carrying a bare @a the source never wrote.
		{
			name: "annotation arguments", member: "id String primary @a(x",
			wantProps: 1, wantIssues: 1, wantAnchor: "(", wantBareAnn: "a",
		},
		{
			name: "multiplicity", member: "--> wheels (many Wheel",
			wantIssues: 1, wantAnchor: "(",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := "schema \"s\"\ntype T {\n\t" + tc.member + "\n}\n"
			file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
			if len(issues) != tc.wantIssues || len(issues) == 0 {
				t.Fatalf("got %d diagnostics, want %d: %v", len(issues), tc.wantIssues, issues)
			}
			for i, iss := range issues {
				if iss.Code() != diag.E_SYNTAX {
					t.Errorf("issue %d is %s, want E_SYNTAX", i, iss.Code())
				}
			}
			sp := issues[0].Span()
			if got := src[sp.Start.Byte:sp.End.Byte]; got != tc.wantAnchor {
				t.Errorf("first diagnostic anchored on %q, want %q", got, tc.wantAnchor)
			}
			if len(file.Types) != 1 {
				t.Fatalf("got %d types, want 1", len(file.Types))
			}
			if got := len(file.Types[0].Properties); got != tc.wantProps {
				t.Fatalf("recorded %d properties, want %d", got, tc.wantProps)
			}
			if tc.wantBareAnn == "" {
				return
			}
			anns := file.Types[0].Properties[0].Annotations
			if len(anns) != 1 || anns[0].Name != tc.wantBareAnn || anns[0].HasParens || len(anns[0].Args) != 0 {
				t.Errorf("annotations = %+v, want one bare @%s — the fabricated shape this row records",
					anns, tc.wantBareAnn)
			}
		})
	}
}

// TestRecovery_HeaderFailureIsFatalAndFlagged pins the two facts a loader
// needs: the schema has no usable name, and the rest of the file still parsed.
func TestRecovery_HeaderFailureIsFatalAndFlagged(t *testing.T) {
	src := "schema\ntype T {\n\tid String primary\n}\n"
	file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1: %v", len(issues), issues)
	}
	if issues[0].Severity() != diag.Fatal {
		t.Errorf("severity = %s, want fatal", issues[0].Severity())
	}
	if !file.SchemaNameFailed {
		t.Error("SchemaNameFailed = false, want true")
	}
	if len(file.Types) != 1 {
		t.Error("the declaration after the broken header did not survive")
	}
}

func hasType(f *File, name string) bool {
	for _, ty := range f.Types {
		if ty.Name == name {
			return true
		}
	}
	return false
}

func assertSpansWithinSource(t *testing.T, src string, issues []diag.Issue) {
	t.Helper()
	for _, iss := range issues {
		s := iss.Span()
		if s.End.Byte > len(src) {
			t.Errorf("diagnostic span [%d,%d) runs past the %d-byte source", s.Start.Byte, s.End.Byte, len(src))
		}
	}
}

// assertNodeSpansWithinSource walks every span the node tree can hold. A span
// built without spanOf's clamp reaches location.RangeWithBytes and panics, so
// a branch this walk does not reach is a crash nothing is positioned to catch.
func assertNodeSpansWithinSource(t *testing.T, src string, f *File) {
	t.Helper()
	check := func(what string, s location.Span) {
		if s.IsZero() {
			return
		}
		if s.End.Byte > len(src) {
			t.Errorf("%s span [%d,%d) runs past the %d-byte source", what, s.Start.Byte, s.End.Byte, len(src))
		}
	}
	checkRef := func(what string, r *TypeRef) {
		if r == nil {
			return
		}
		check(what, r.Span)
		check(what+" name", r.NameSpan)
	}
	var checkConstraint func(what string, c *Constraint)
	checkConstraint = func(what string, c *Constraint) {
		if c == nil {
			return
		}
		check(what, c.Span)
		if c.Bounds != nil {
			check(what+" bounds", c.Bounds.Span)
			check(what+" min bound", c.Bounds.Min.Span)
			check(what+" max bound", c.Bounds.Max.Span)
		}
		for _, lit := range c.EnumLits {
			check(what+" enum value", lit.Span)
		}
		for _, lit := range c.PatternLits {
			check(what+" pattern", lit.Span)
		}
		if c.FormatLit != nil {
			check(what+" format", c.FormatLit.Span)
		}
		if c.DimsLit != nil {
			check(what+" dimensions", c.DimsLit.Span)
		}
		checkRef(what+" alias", c.Alias)
		checkConstraint(what+" element", c.Elem)
	}
	checkAnnotation := func(what string, a *Annotation) {
		check(what, a.Span)
		check(what+" name", a.NameSpan)
		for _, arg := range a.Args {
			check(what+" argument", arg.Span)
		}
	}
	checkProperty := func(what string, p *Property) {
		check(what, p.Span)
		check(what+" name", p.NameSpan)
		checkConstraint(what+" constraint", p.Constraint)
		for _, a := range p.Annotations {
			checkAnnotation(what+" annotation", a)
		}
	}

	check("file", f.Span)
	check("schema name", f.NameSpan)
	for _, imp := range f.Imports {
		check("import", imp.Span)
		check("import path", imp.PathSpan)
		check("import alias", imp.AliasSpan)
	}
	for _, dt := range f.DataTypes {
		check("datatype", dt.Span)
		check("datatype name", dt.NameSpan)
		checkConstraint("datatype constraint", dt.Constraint)
	}
	for _, ty := range f.Types {
		check("type", ty.Span)
		check("type name", ty.NameSpan)
		for _, ref := range ty.Extends {
			checkRef("supertype", ref)
		}
		for _, p := range ty.Properties {
			checkProperty("property", p)
		}
		for _, rel := range ty.Relations {
			check("relation", rel.Span)
			check("relation name", rel.NameSpan)
			checkRef("relation target", rel.Target)
			for _, p := range rel.Properties {
				checkProperty("edge property", p)
			}
		}
		for _, inv := range ty.Invariants {
			check("invariant", inv.Span)
			check("invariant message", inv.MessageSpan)
			check("invariant expression", inv.ExprSpan)
		}
		for _, ann := range ty.Annotations {
			checkAnnotation("type annotation", ann)
		}
	}
}

// TestRecovery_ContextualKeywordPropertiesSurvive pins that recovery does not
// delete a property whose name is one of the eleven contextual keywords. The
// member-start predicate has to test the six spellings that are never names,
// not the seventeen the language reserves somewhere.
func TestRecovery_ContextualKeywordPropertiesSurvive(t *testing.T) {
	for _, name := range []string{"type", "schema", "import", "extends", "primary", "required", "one", "many", "abstract", "datatype", "includes"} {
		t.Run(name, func(t *testing.T) {
			src := "schema \"s\"\ntype T {\n\tid String primary\n\tbroken @\n\t" + name + " String\n}\n"
			file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
			if len(issues) != 1 {
				t.Fatalf("got %d diagnostics, want 1: %v", len(issues), issues)
			}
			if len(file.Types) != 1 {
				t.Fatalf("got %d types, want 1", len(file.Types))
			}
			props := file.Types[0].Properties
			if len(props) != 2 || props[1].Name != name {
				var got []string
				for _, p := range props {
					got = append(got, p.Name)
				}
				t.Errorf("properties = %v, want [id %s] — recovery deleted a legal member", got, name)
			}
		})
	}
}

// TestRecovery_OneDefectDrawsOneDiagnostic pins that recovery does not restart
// inside the member it just left. Each source holds one defect; a restart in
// the middle of the failed member reports it again at a second position.
func TestRecovery_OneDefectDrawsOneDiagnostic(t *testing.T) {
	for _, tc := range []struct{ name, member string }{
		{"relation with junk after its name", "--> rel 123"},
		{"relation with a malformed multiplicity", "--> r (many:one) B"},
		{"relation with an unterminated multiplicity", "--> wheels (many Wheel"},
		{"a regex-shaped token where a datatype belongs", "x /*/"},
		{"a property with no datatype, before more members", "id primary\n\tref String"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "schema \"s\"\ntype T {\n\tid String primary\n\t" + tc.member + "\n}\n"
			_, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
			if len(issues) != 1 {
				t.Errorf("got %d diagnostics, want 1 — recovery restarted inside the failed member: %v",
					len(issues), issues)
			}
		})
	}
}

// TestRecovery_RegexIsNotADocComment pins that recovery classifies a doc
// comment by token type. The rule table lets "/*/" lex as REGEXP, so a
// text-prefix test halts recovery on a regex literal and re-parses at a token
// no production can match, costing the member that follows it.
func TestRecovery_RegexIsNotADocComment(t *testing.T) {
	src := "schema \"s\"\ntype T {\n\tid String primary\n\tx @\n\t/*/\n\ty String\n}\n"
	file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
	if len(issues) != 1 {
		t.Fatalf("got %d diagnostics, want 1: %v", len(issues), issues)
	}
	if len(file.Types) != 1 {
		t.Fatalf("got %d types, want 1", len(file.Types))
	}
	var names []string
	for _, p := range file.Types[0].Properties {
		names = append(names, p.Name)
	}
	if len(names) != 2 || names[1] != "y" {
		t.Errorf("properties = %v, want [id y] — the member after the regex was lost", names)
	}
}

// TestRecovery_HeaderFailureKeepsTheDeclarationItStopsOn pins that a failed
// schema header costs the header alone. The failing token is itself the start
// of the next declaration, so a resync that must consume one token eats it.
func TestRecovery_HeaderFailureKeepsTheDeclarationItStopsOn(t *testing.T) {
	t.Run("abstract survives", func(t *testing.T) {
		file, _ := Parse([]byte("abstract type A {\n\tid String primary\n}\n"), location.NewSourceID("s.yammm"))
		if len(file.Types) != 1 {
			t.Fatalf("got %d types, want 1", len(file.Types))
		}
		if !file.Types[0].IsAbstract {
			t.Error("the type lost its abstract modifier to recovery")
		}
	})
	t.Run("a lone type survives", func(t *testing.T) {
		file, _ := Parse([]byte("type A {\n\tid String primary\n}\n"), location.NewSourceID("s.yammm"))
		if len(file.Types) != 1 || len(file.Types[0].Properties) != 1 {
			t.Errorf("types = %d, want one carrying its property", len(file.Types))
		}
	})
	t.Run("an import survives", func(t *testing.T) {
		file, _ := Parse([]byte("import \"a.yammm\" as al\ntype A {\n\tid String primary\n}\n"), location.NewSourceID("s.yammm"))
		if len(file.Imports) != 1 || len(file.Types) != 1 {
			t.Errorf("imports = %d types = %d, want 1 and 1", len(file.Imports), len(file.Types))
		}
	})
}

// TestRecovery_MalformedNumberDoesNotSwallowAnEarlierDefect pins the window the
// malformed-numeric override searches. Measured after recovery, it covered
// everything recovery skipped, so a literal in a later declaration replaced the
// real diagnostic and the file reported one error about the wrong thing.
func TestRecovery_MalformedNumberDoesNotSwallowAnEarlierDefect(t *testing.T) {
	src := "type A {\n\ty Integer[0x10, 5]\n}\n"
	_, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
	if len(issues) != 2 {
		t.Fatalf("got %d diagnostics, want 2 — the header failure and the literal: %v", len(issues), issues)
	}
	if issues[0].Severity() != diag.Fatal || !strings.Contains(issues[0].Message(), "schema header") {
		t.Errorf("first diagnostic = %v %q, want the fatal header failure", issues[0].Severity(), issues[0].Message())
	}
	if !strings.Contains(issues[1].Message(), "0x10") {
		t.Errorf("second diagnostic = %q, want the malformed literal", issues[1].Message())
	}
}
