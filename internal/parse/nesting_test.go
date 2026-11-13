package parse

import (
	"errors"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/alecthomas/participle/v2/lexer"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/raceskip"
	"github.com/simon-lentz/yammm/location"
)

// nestingSchema is a schema with one property of the given type and one
// invariant over the given expression.
func nestingSchema(typ, expression string) string {
	return "schema \"s\"\n\ntype T {\n\tid String primary\n\tv " + typ + "\n\t! \"m\" " + expression + "\n}\n"
}

func nestedList(levels int) string {
	return strings.Repeat("List<", levels) + "String" + strings.Repeat(">", levels)
}

// expressionOfHeight builds, per shape, an expression whose tree is n levels.
var expressionOfHeight = map[string]func(n int) string{
	"pipeline":    func(n int) string { return "v" + strings.Repeat(" -> Compact", n-1) },
	"member":      func(n int) string { return "v" + strings.Repeat(".v", n-1) },
	"index":       func(n int) string { return "v" + strings.Repeat("[0]", n-1) },
	"binary left": func(n int) string { return "v" + strings.Repeat(" + v", n-1) },
	"binary right": func(n int) string {
		return strings.Repeat("v == (", n/2-1) + "v == v" + strings.Repeat(")", n/2-1)
	},
	"parentheses": func(n int) string { return strings.Repeat("(", n-1) + "v" + strings.Repeat(")", n-1) },
	"not":         func(n int) string { return strings.Repeat("!", n-1) + "v" },
	"minus":       func(n int) string { return strings.Repeat("-", n-1) + "v" },
	"list":        func(n int) string { return strings.Repeat("[", n-1) + "v" + strings.Repeat("]", n-1) },
	"argument": func(n int) string {
		return strings.Repeat("v -> Default(", n-1) + "v" + strings.Repeat(")", n-1)
	},
	"body": func(n int) string {
		return strings.Repeat("v -> Then { ", n-1) + "v" + strings.Repeat(" }", n-1)
	},
	"conditional": func(n int) string {
		return strings.Repeat("v ? { v : ", n-1) + "v" + strings.Repeat(" }", n-1)
	},
	"index operand": func(n int) string {
		return strings.Repeat("v[", n-1) + "0" + strings.Repeat("]", n-1)
	},
}

func syntaxIssues(t *testing.T, text string) []diag.Issue {
	t.Helper()
	_, issues := Parse([]byte(text), location.NewSourceID("nesting.yammm"))
	var out []diag.Issue
	for _, issue := range issues {
		if issue.Code() == diag.E_SYNTAX {
			out = append(out, issue)
		}
	}
	return out
}

func TestParse_ExpressionNestsAtMostMaxNestingLevels(t *testing.T) {
	t.Parallel()
	const refusal = "expression nests deeper than 1000 levels"
	for shape, build := range expressionOfHeight {
		t.Run(shape, func(t *testing.T) {
			t.Parallel()
			if got := syntaxIssues(t, nestingSchema("String", build(MaxNesting))); len(got) != 0 {
				t.Errorf("height %d: got %v, want no syntax error", MaxNesting, got)
			}
			// The binary right shape builds even heights alone, so the next height is two over.
			got := syntaxIssues(t, nestingSchema("String", build(MaxNesting+2)))
			if len(got) != 1 || got[0].Message() != refusal {
				t.Errorf("height %d: got %v, want one %q", MaxNesting+2, got, refusal)
			}
		})
	}
}

// An expression far over the bound draws the one refusal that an expression
// two levels over it draws.
func TestParse_AnExpressionFarOverTheBoundIsRefusedOnce(t *testing.T) {
	t.Parallel()
	// The text is read on one goroutine, and the detector multiplies each lex.
	raceskip.Skip(t)
	const refusal = "expression nests deeper than 1000 levels"
	for shape, build := range expressionOfHeight {
		t.Run(shape, func(t *testing.T) {
			t.Parallel()
			got := syntaxIssues(t, nestingSchema("String", build(100_000)))
			if len(got) != 1 || got[0].Message() != refusal {
				t.Errorf("got %v, want one %q", got, refusal)
			}
		})
	}
}

// refusalAnchor gives, per shape whose height is exact, the offset in an
// expression one level past the bound of the token its refusal stands at. A
// chain is refused at the operator that adds the level. A nest is refused
// before any operand is read, at the first token nested past the bound.
var refusalAnchor = map[string]func(e string) int{
	"pipeline":      func(e string) int { return strings.LastIndex(e, "->") },
	"member":        func(e string) int { return strings.LastIndex(e, ".") },
	"index":         func(e string) int { return strings.LastIndex(e, "[") },
	"binary left":   func(e string) int { return strings.LastIndex(e, "+") },
	"parentheses":   func(e string) int { return strings.Index(e, "v") },
	"not":           func(e string) int { return strings.Index(e, "v") },
	"minus":         func(e string) int { return strings.Index(e, "v") },
	"list":          func(e string) int { return strings.Index(e, "v") },
	"argument":      func(e string) int { return strings.LastIndex(e, "v") },
	"body":          func(e string) int { return strings.LastIndex(e, "v") },
	"conditional":   func(e string) int { return strings.LastIndex(e, "v : v }") },
	"index operand": func(e string) int { return strings.Index(e, "0") },
}

func TestParse_ExpressionHeightCountsEachLevelOnce(t *testing.T) {
	t.Parallel()
	const refusal = "expression nests deeper than 1000 levels"
	for shape, anchor := range refusalAnchor {
		t.Run(shape, func(t *testing.T) {
			t.Parallel()
			e := expressionOfHeight[shape](MaxNesting + 1)
			text := nestingSchema("String", e)
			got := syntaxIssues(t, text)
			if len(got) != 1 || got[0].Message() != refusal {
				t.Fatalf("height %d: got %v, want one %q", MaxNesting+1, got, refusal)
			}
			if at, want := got[0].Span().Start.Byte-strings.Index(text, e), anchor(e); at != want {
				t.Errorf("the refusal stands at byte %d of the expression, want %d", at, want)
			}
		})
	}
}

// Each wrapper adds the stated levels to its highest operand, whichever
// operand that is: a chain one level under the bound is refused in it, and the
// chain a level shorter is not.
func TestParse_ExpressionHeightReadsEveryOperand(t *testing.T) {
	t.Parallel()
	const refusal = "expression nests deeper than 1000 levels"
	chain := expressionOfHeight["member"]
	for _, w := range []struct {
		name string
		adds int
		wrap func(c string) string
	}{
		{"an index", 1, func(c string) string { return "v[" + c + "]" }},
		{"an indexed operand", 1, func(c string) string { return c + "[0]" }},
		{"an argument", 1, func(c string) string { return "v -> Default(" + c + ")" }},
		{"the first of two arguments", 1, func(c string) string { return "v -> Default(" + c + ", v)" }},
		{"an argument before a body", 1, func(c string) string { return "v -> Default(" + c + ") { v }" }},
		{"a body", 1, func(c string) string { return "v -> Then { " + c + " }" }},
		{"a body after an argument", 1, func(c string) string { return "v -> Default(v) { " + c + " }" }},
		{"a condition", 1, func(c string) string { return c + " ? { v : v }" }},
		{"a then branch", 1, func(c string) string { return "v ? { " + c + " : v }" }},
		{"an else branch", 1, func(c string) string { return "v ? { v : " + c + " }" }},
		{"a right operand", 1, func(c string) string { return "v == " + c }},
		{"a negated operand", 1, func(c string) string { return "!" + c }},
		{"a minus operand", 2, func(c string) string { return "-(" + c + ")" }},
		{"parentheses", 1, func(c string) string { return "(" + c + ")" }},
		{"a member of parentheses", 2, func(c string) string { return "(" + c + ").v" }},
		{"a list", 1, func(c string) string { return "[" + c + "]" }},
		{"the first of two elements", 1, func(c string) string { return "[" + c + ", v]" }},
	} {
		t.Run(w.name, func(t *testing.T) {
			t.Parallel()
			at := MaxNesting - w.adds
			if got := syntaxIssues(t, nestingSchema("String", w.wrap(chain(at)))); len(got) != 0 {
				t.Errorf("over a chain of %d: got %v, want no syntax error", at, got)
			}
			got := syntaxIssues(t, nestingSchema("String", w.wrap(chain(at+1))))
			if len(got) != 1 || got[0].Message() != refusal {
				t.Errorf("over a chain of %d: got %v, want one %q", at+1, got, refusal)
			}
		})
	}
}

// A signed literal is one level, as a literal with no sign is.
func TestParse_ASignedLiteralIsOneLevel(t *testing.T) {
	t.Parallel()
	if got := syntaxIssues(t, nestingSchema("String", "-5"+strings.Repeat(" + v", MaxNesting-1))); len(got) != 0 {
		t.Errorf("got %v, want no syntax error", got)
	}
}

// An expression refused for its depth is one issue however it is laid out, and
// the member after it is read: no token of the expression is taken for a
// member, and no token of the next member for the expression.
func TestParse_ATooDeepExpressionIsOneIssueAndTheNextMemberIsRead(t *testing.T) {
	t.Parallel()
	const refusal = "expression nests deeper than 1000 levels"
	const over = MaxNesting + 200
	nest := func(open, operand, closer string) string {
		return strings.Repeat(open, over) + operand + strings.Repeat(closer, over)
	}
	chain := expressionOfHeight["member"](MaxNesting)
	for name, e := range map[string]string{
		"a member chain one level over":    expressionOfHeight["member"](MaxNesting + 1),
		"an and chain one level over":      expressionOfHeight["binary left"](MaxNesting + 1),
		"a chain in parentheses":           "(" + chain + ")",
		"a negated chain":                  "!" + chain,
		"a chain in a list":                "[" + chain + "]",
		"a chain as an argument":           "v -> Default(" + chain + ")",
		"negated terms":                    "!v" + strings.Repeat(" && !v", over),
		"not and minus":                    nest("!-", "v", ""),
		"not and parentheses":              nest("!(", "v", ")"),
		"parentheses and not":              nest("(!", "v", ")"),
		"a list and not":                   nest("[!", "v", "]"),
		"a right operand of nots":          "v == " + nest("(!", "v", ")"),
		"nots before a negated group":      strings.Repeat("!", over) + "(!v)",
		"one not per line":                 strings.Repeat("!\n\t", over) + "v",
		"one not and a comment per line":   strings.Repeat("! // c\n\t", over) + "v",
		"terms after a deep group":         nest("(", "v", ")") + " && !v && !v && !v",
		"a negated operand in a group":     nest("(", "!v", ")"),
		"a trailing operator per line":     "v" + strings.Repeat(" &&\n\tv", over),
		"a trailing plus per line":         "v" + strings.Repeat(" +\n\tv", over),
		"a leading operator per line":      "v" + strings.Repeat("\n\t&& v", over),
		"a stage per line":                 "v" + strings.Repeat("\n\t-> Compact", over),
		"an argument per line":             nest("v -> Default(\n\t", "v", ")"),
		"an index per line":                nest("v[\n\t", "0", "]"),
		"a body with a parameter per line": nest("v -> Map |$x| {\n\t", "$x", " }"),
		"a conditional per line":           nest("v ? {\n\tv :\n\t", "v", " }"),
		"a call with no parentheses":       "v" + strings.Repeat(" -> Compact |$x,| { $x }", over) + ".v[0] in v",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			text := "schema \"s\"\n\ntype T {\n\tid String primary\n\tv String\n\t! \"m\" " + e +
				"\n\t! \"kept\" !v\n\tw String\n\t! \"broken\" v == String\n}\n"
			file, issues := Parse([]byte(text), location.NewSourceID("nesting.yammm"))
			if len(issues) != 2 || issues[0].Message() != refusal || issues[1].Message() != `unexpected keyword "String" in an expression` {
				t.Fatalf("got %v, want one %q and the last member's issue", issues, refusal)
			}
			if len(file.Types) != 1 {
				t.Fatalf("parsed %d types, want 1", len(file.Types))
			}
			typ := file.Types[0]
			if len(typ.Invariants) != 1 || typ.Invariants[0].Message != "kept" {
				t.Errorf("invariants %v, want the one named kept", typ.Invariants)
			}
			if len(typ.Properties) != 3 {
				t.Errorf("parsed %d properties, want id, v and w", len(typ.Properties))
			}
		})
	}
}

// A member that starts on the line a too-deep expression ends on is read: the
// refusal owns the expression and no token after it.
func TestParse_AMemberOnTheLineOfATooDeepExpressionIsRead(t *testing.T) {
	t.Parallel()
	const refusal = "expression nests deeper than 1000 levels"
	const over = MaxNesting + 200
	for name, e := range map[string]string{
		"a member chain":   expressionOfHeight["member"](over),
		"a comparison":     "v" + strings.Repeat(" -> Trim", over) + ` == "a"`,
		"parentheses":      strings.Repeat("(", over) + "v" + strings.Repeat(")", over),
		"a body":           "v" + strings.Repeat(" -> Map { v", over) + strings.Repeat(" }", over),
		"an index at last": "v" + strings.Repeat(" && !v", over) + "[0]",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			text := "schema \"s\"\n\ntype T {\n\tid String primary\n\tv String\n\t! \"m\" " + e +
				" w String x Integer\n\t! \"kept\" w == \"a\" && x > 0\n}\n"
			file, issues := Parse([]byte(text), location.NewSourceID("nesting.yammm"))
			if len(issues) != 1 || issues[0].Message() != refusal {
				t.Fatalf("got %v, want one %q", issues, refusal)
			}
			typ := file.Types[0]
			if len(typ.Invariants) != 1 || typ.Invariants[0].Message != "kept" {
				t.Errorf("invariants %v, want the one named kept", typ.Invariants)
			}
			if len(typ.Properties) != 4 {
				t.Errorf("parsed %d properties, want id, v, w and x", len(typ.Properties))
			}
		})
	}
}

// What follows a whole too-deep expression is read as it stands: the file
// draws the depth issue and then the issues it draws with true in the
// expression's place, and it keeps the same members.
func TestParse_WhatFollowsAWholeTooDeepExpressionIsReadAsItStands(t *testing.T) {
	t.Parallel()
	const refusal = "expression nests deeper than 1000 levels"
	deepExpression := strings.Repeat("(", MaxNesting+200) + "v" + strings.Repeat(")", MaxNesting+200)
	parse := func(e, follower string) ([]string, []string) {
		text := "schema \"s\"\n\ntype T {\n\tid String primary\n\tv String\n\t! \"m\" " + e + follower +
			"\n\ty Integer\n\t! \"kept\" y > 0\n}\n\ntype U {\n\tid String primary\n}\n"
		file, issues := Parse([]byte(text), location.NewSourceID("nesting.yammm"))
		var messages, members []string
		for _, issue := range issues {
			messages = append(messages, issue.Message())
		}
		for _, typ := range file.Types {
			members = append(members, "type "+typ.Name)
			for _, p := range typ.Properties {
				members = append(members, p.Name)
			}
			for _, inv := range typ.Invariants {
				if inv.Message != "m" {
					members = append(members, "! "+inv.Message)
				}
			}
		}
		return messages, members
	}
	for name, follower := range map[string]string{
		"a member":                      " w String x Integer",
		"a token that starts no member": " 5 x String",
		"a closing bracket":             " ) w String",
		"a malformed number":            " 1x",
		"the type's closing brace":      " }\n\ntype V {\n\tid String primary",
		"an operator and a name, then a type on the next line": " +\n\tv String",
		"a stray token on the next line":                       "\n\t5 x String",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			wantIssues, wantMembers := parse("true", follower)
			issues, members := parse(deepExpression, follower)
			if len(issues) == 0 || issues[0] != refusal || !slices.Equal(issues[1:], wantIssues) {
				t.Errorf("got %q, want %q and then %q", issues, refusal, wantIssues)
			}
			if !slices.Equal(members, wantMembers) {
				t.Errorf("the file keeps %q, with true in the expression's place %q", members, wantMembers)
			}
		})
	}
}

// A too-deep expression that leaves a bracket open is one issue for its depth,
// and recovery resumes where it does after the same break in a shallow
// expression: at the token the expression parser refuses. A name on the
// expression's line is then no member start, as it is none in the shallow one.
func TestParse_ATooDeepExpressionWithAnOpenBracketRecoversAsAShallowOne(t *testing.T) {
	t.Parallel()
	const refusal = "expression nests deeper than 1000 levels"
	parse := func(e string) (*File, []string) {
		text := "schema \"s\"\n\ntype T {\n\tv String\n\t! \"m\" " + e +
			"\n\tid String primary\n\tw String\n\t! \"kept\" !v\n}\n\ntype U {\n\tid String primary\n}\n"
		file, issues := Parse([]byte(text), location.NewSourceID("nesting.yammm"))
		var messages []string
		for _, issue := range issues {
			messages = append(messages, issue.Message())
		}
		return file, messages
	}
	members := func(file *File) (out []string) {
		for _, typ := range file.Types {
			out = append(out, "type "+typ.Name)
			for _, p := range typ.Properties {
				out = append(out, p.Name)
			}
			for _, inv := range typ.Invariants {
				out = append(out, "! "+inv.Message)
			}
		}
		return out
	}
	for name, open := range map[string]struct {
		expression func(n int) string
		keeps      bool // the members after the expression are read
	}{
		"parentheses": {func(n int) string { return strings.Repeat("(", n) + "v" + strings.Repeat(")", n-1) }, true},
		"a list":      {func(n int) string { return strings.Repeat("[", n) + "v" + strings.Repeat("]", n-1) }, true},
		"an argument list": {func(n int) string {
			return "v" + strings.Repeat(" -> Default(v", n) + strings.Repeat(")", n-1)
		}, true},
		"an index": {func(n int) string { return "v" + strings.Repeat("[v", n) + strings.Repeat("]", n-1) }, true},
		// Recovery counts braces, so an open one takes the type's own.
		"a body": {func(n int) string {
			return "v" + strings.Repeat(" -> Map { v", n) + strings.Repeat(" }", n-1)
		}, false},
		"a conditional": {func(n int) string {
			return strings.Repeat("v ? { v : ", n) + "v" + strings.Repeat(" }", n-1)
		}, false},
	} {
		for _, follower := range []string{"", " x Integer"} {
			t.Run(name+follower, func(t *testing.T) {
				t.Parallel()
				shallowFile, shallow := parse(open.expression(3) + follower)
				deepFile, deep := parse(open.expression(MaxNesting+200) + follower)
				if len(deep) == 0 || deep[0] != refusal {
					t.Fatalf("got %q, want %q first", deep, refusal)
				}
				if len(deep) != len(shallow) || !slices.Equal(deep[1:], shallow[1:]) {
					t.Errorf("after its first issue the deep expression draws %q, the shallow one %q", deep[1:], shallow[1:])
				}
				if got, want := members(deepFile), members(shallowFile); !slices.Equal(got, want) {
					t.Errorf("the deep expression leaves %q, the shallow one %q", got, want)
				}
				kept := slices.Equal(members(deepFile), []string{"type T", "v", "id", "w", "! kept", "type U", "id"})
				if kept != open.keeps || open.keeps && len(deep) != 1 {
					t.Errorf("the members after the expression are read: %t with issues %q, want %t", kept, deep, open.keeps)
				}
			})
		}
	}
}

func TestParse_ListNestsAtMostMaxNestingLevels(t *testing.T) {
	t.Parallel()
	if got := syntaxIssues(t, nestingSchema(nestedList(MaxNesting), "true")); len(got) != 0 {
		t.Errorf("%d levels: got %v, want no syntax error", MaxNesting, got)
	}
	listRefusedAtTheBound(t, MaxNesting+1)
}

// A List far over the bound is refused where a List one level over it is.
func TestParse_AListFarOverTheBoundIsRefusedAtTheBound(t *testing.T) {
	t.Parallel()
	// The text is read on one goroutine, and the detector multiplies each lex.
	raceskip.Skip(t)
	listRefusedAtTheBound(t, 100_000)
}

// listRefusedAtTheBound requires that a List nested n levels draws the one
// refusal, on the first List past the bound.
func listRefusedAtTheBound(t *testing.T, n int) {
	t.Helper()
	const refusal = "List nests deeper than 1000 levels"
	text := nestingSchema(nestedList(n), "true")
	got := syntaxIssues(t, text)
	if len(got) != 1 || got[0].Message() != refusal {
		t.Fatalf("%d levels: got %v, want one %q", n, got, refusal)
	}
	want := strings.Index(text, "List<") + MaxNesting*len("List<")
	if start := got[0].Span().Start.Byte; start != want {
		t.Errorf("%d levels: issue at byte %d, want %d", n, start, want)
	}
}

func TestParse_ListLevelIsCountedPerProperty(t *testing.T) {
	t.Parallel()
	// Two properties of 600 levels each are 1,200 List keywords and no nesting
	// past the bound, with a comment and a line break inside one of them.
	text := "schema \"s\"\n\ntype T {\n\ta " + nestedList(600) + "\n\tb List<\n\t// the element\n\t" + nestedList(599) + ">\n}\n"
	if got := syntaxIssues(t, text); len(got) != 0 {
		t.Errorf("got %v, want no syntax error", got)
	}
}

// skipExpression ends at the first token the expression parser does not read,
// and reports whether a whole expression ends there.
func TestSkipExpression_EndsWhereAnExpressionCannotContinue(t *testing.T) {
	t.Parallel()
	ps := mustParsers()
	for _, row := range []struct {
		src, rest string
		whole     bool
	}{
		{"a + b c", "c", true},
		{"a +", "", false},
		{"a ! b", "! b", true},
		{"-!-a.b", "", true},
		{"- ! ( a ) ]", "]", true},
		{") a", ") a", false},
		{"1 2.5", "2.5", true},
		{"$x _", "_", true},
		{"_ X", "X", true},
		{`"s" "t"`, `"t"`, true},
		{"a =~ /b/ c", "c", true},
		{"/b/ c", "c", true},
		{"a in b c", "c", true},
		{"a + in b", "in b", false},
		{"a + part b", "part b", false},
		{"a -> f(b, , ) c", ", ) c", false},
		{"a -> f(, b) c", "b) c", false},
		{"Integer + 1", "Integer + 1", false},
		{"a -> 5", "5", false},
		{"a . 5", "5", false},
		{"a ? b", "b", false},
		{"a ? { b : c } d", "d", true},
		{"a ? { b : c } { d }", "{ d }", true},
		{"a ? { b } c", "} c", false},
		{"a ? { b : c : d }", ": d }", false},
		{"a ? { : c }", ": c }", false},
		{"(a ? { b : c }) d", "d", true},
		{"a -> F (b -> G { c }) d", "d", true},
		{"[a -> F |$x| { $x }] d", "d", true},
		{"a -> F (x) |$a, $b| { $a } (y)", "(y)", true},
		{"a -> F |$a| (x)", "(x)", true},
		{"a -> F |$x, $y,| { $x } b", "b", true},
		{"a -> F |$a b|", "b|", false},
		{"a -> F |x| b", "x| b", false},
		{"a -> F |,| b", ",| b", false},
		{"a -> F { b } { c }", "{ c }", true},
		{"a -> F { b } |$x|", "|$x|", true},
		{"a -> F { b, c }", ", c }", false},
		{"a -> F (x) (y)", "(y)", true},
		{"a -> F |$a| |$b|", "|$b|", true},
		{"a -> F(,) c", "c", true},
		{"a -> F(a,) c", "c", true},
		{"a -> F(a + ,) c", ",) c", false},
		{"a -> F(, b)", "b)", false},
		{"a (b)", "(b)", true},
		{"a { b }", "{ b }", true},
		{"a |$x|", "|$x|", true},
		{"a.b (c)", "(c)", true},
		{"a[0] { b }", "{ b }", true},
		{"a[] b", "] b", false},
		{"a[0, 1] b", ", 1] b", false},
		{"(a) (b)", "(b)", true},
		{"() a", ") a", false},
		{"(a, b)", ", b)", false},
		{"(a b", "b", false},
		{"(a] b", "] b", false},
		{"[a, (b] c", "] c", false},
		{"[a, b c]", "c]", false},
		{"[a, b,] c", "c", true},
		{"[] c", "c", true},
		{"[,] c", ",] c", false},
		{"[a] [b] c", "c", true},
		{"a[0] [1] . b -> c in d e", "e", true},
		{"a -> F -> G(x) -> H |$x| { $x } ! b", "! b", true},
	} {
		end, whole := skipExpression(expressionLexer(t, ps, row.src), ps.tok)
		if got := row.src[end:]; got != row.rest || whole != row.whole {
			t.Errorf("%q: the scan ends before %q (whole %t), want before %q (whole %t)", row.src, got, whole, row.rest, row.whole)
		}
	}
}

func expressionLexer(t *testing.T, ps *parsers, src string) *lexer.PeekingLexer {
	t.Helper()
	lx, err := ps.def.LexString("", src)
	if err != nil {
		t.Fatalf("%q: %v", src, err)
	}
	plex, err := lexer.Upgrade(lx, ps.elide...)
	if err != nil {
		t.Fatalf("%q: %v", src, err)
	}
	return plex
}

// skipExpression reads the tokens the expression parser reads: over token
// sequences drawn at random, it ends at the token the parser stops at when the
// expression parses, and at the token the parser refuses when it does not.
func TestSkipExpression_StopsWhereTheParserStops(t *testing.T) {
	t.Parallel()
	// The draw is read on one goroutine, and the detector multiplies each lex.
	raceskip.Skip(t)
	ps := mustParsers()
	words := []string{
		"a", "b", "B", "Integer", "in", "as", "nil", "true", "1", "2.5", `"s"`, "/r/", "$x", "_",
		"+", "-", "!", "==", "&&", "||", "=~", "(", ")", "[", "]", "{", "}", ",", ":", "?", "->", ".", "|",
	}
	rng := rand.New(rand.NewPCG(1, 2)) //nolint:gosec // a fixed seed makes the sequences reproducible
	parsed, refused := 0, 0
	for range 40_000 {
		parts := make([]string, 1+rng.IntN(12))
		for i := range parts {
			parts[i] = words[rng.IntN(len(words))]
		}
		src := strings.Join(parts, " ")
		lx := expressionLexer(t, ps, src)
		var diags []exprDiag
		_, _, err := newExprParser(lx, &diags, ps.tok).parse(0)
		wantEnd, wantWhole := lx.Peek().Pos.Offset, true
		if err != nil {
			ee, ok := errors.AsType[*exprError](err)
			if !ok {
				t.Fatalf("%q: the parser failed with %v", src, err)
			}
			wantEnd, wantWhole = ee.pos.Offset, false
			refused++
		} else {
			parsed++
		}
		if end, whole := skipExpression(expressionLexer(t, ps, src), ps.tok); end != wantEnd || whole != wantWhole {
			t.Fatalf("%q: the scan ends at byte %d (whole %t), the parser at %d (whole %t)", src, end, whole, wantEnd, wantWhole)
		}
	}
	if parsed < 1000 || refused < 1000 {
		t.Errorf("%d sequences parsed and %d were refused; the draw must reach both", parsed, refused)
	}
}

// White space and a line comment between a List's tokens do not hide a level
// from the bound.
func TestParse_ListLevelReadsThroughSpaceAndComments(t *testing.T) {
	t.Parallel()
	const refusal = "List nests deeper than 1000 levels"
	for name, open := range map[string]string{
		"a space before the bracket":  "List <",
		"a space after the bracket":   "List< ",
		"a comment after the bracket": "List< // an element\n\t",
		"a comment before it":         "List // a list\n\t<",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			build := func(levels int) string {
				return nestingSchema(strings.Repeat(open, levels)+"String"+strings.Repeat(">", levels), "true")
			}
			if got := syntaxIssues(t, build(MaxNesting)); len(got) != 0 {
				t.Errorf("%d levels: got %v, want no syntax error", MaxNesting, got)
			}
			text := build(MaxNesting + 1)
			got := syntaxIssues(t, text)
			if len(got) != 1 || got[0].Message() != refusal {
				t.Fatalf("%d levels: got %v, want one %q", MaxNesting+1, got, refusal)
			}
			want := strings.Index(text, open) + MaxNesting*len(open)
			if start := got[0].Span().Start.Byte; start != want {
				t.Errorf("issue at byte %d, want %d", start, want)
			}
		})
	}
}

// The bound's message is given to a construct that fails to parse and holds a
// List nested past the bound, and to no other.
func TestParse_ListLevelMessageNamesANestPastTheBoundAlone(t *testing.T) {
	t.Parallel()
	const refusal = "List nests deeper than 1000 levels"
	over := nestedList(MaxNesting + 1)
	half := nestedList(MaxNesting/2 + 1)
	body := func(members string) string {
		return "schema \"s\"\n\ntype T {\n\tid String primary\n" + members + "}\n"
	}
	for _, tc := range []struct {
		name, text string
		want       []string
	}{
		{
			"another word nested as deep",
			body("\tv " + strings.Repeat("V<", MaxNesting+2) + "String" + strings.Repeat(">", MaxNesting+2) + "\n"),
			[]string{`unexpected token "<" in a type body`},
		},
		{
			"a nest after a stray List",
			body("\tv List " + over + "\n"),
			[]string{refusal},
		},
		{
			"List keywords with no bracket",
			body("\tv " + strings.Repeat("List ", MaxNesting+1) + "\n"),
			[]string{`unexpected token "List" in a type body`},
		},
		{
			"a nest under the bound after List keywords with no bracket",
			body("\tv " + strings.Repeat("List ", MaxNesting) + "List<List<String>>\n"),
			[]string{`unexpected token "List" in a type body`},
		},
		{
			"two brackets in a nest under the bound",
			body("\tv " + strings.Repeat("List<", 600) + "<" + strings.Repeat("List<", 600) + "String\n"),
			[]string{`unexpected token "List" in a type body`},
		},
		{
			"two nests under the bound in one failed construct",
			body("\t--> R (one) T {\n\t\ta " + half + "\n\t\tb " + half + "\n\t\tc Strin g\n\t}\n"),
			[]string{`unexpected token "g" in a type body`},
		},
		{
			"a nest in the member after a failed one",
			body("\tv Strin g\n\tw " + over + "\n"),
			[]string{`unexpected token "w" in a type body`, refusal},
		},
		{
			"a malformed number in the nest",
			body("\tv " + strings.Repeat("List<", MaxNesting+1) + "Integer[0x10, 5]" + strings.Repeat(">", MaxNesting+1) + "\n"),
			[]string{InvalidNumberMessage("0x10")},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var got []string
			for _, issue := range syntaxIssues(t, tc.text) {
				got = append(got, issue.Message())
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// An expression's error is reported once when its member begins before the
// invariant's marker.
func TestParse_AnExpressionErrorAfterADocCommentIsOneIssue(t *testing.T) {
	t.Parallel()
	text := "schema \"s\"\n\ntype T {\n\tid String primary\n\t/* d */ ! \"m\" String\n}\n"
	got := syntaxIssues(t, text)
	if len(got) != 1 || got[0].Message() != `unexpected keyword "String" in an expression` {
		t.Errorf("got %v, want the keyword's one issue", got)
	}
}
