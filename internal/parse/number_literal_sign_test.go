package parse

import (
	"math"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/expr"
	"github.com/simon-lentz/yammm/location"
)

func invariantSource(expression string) string {
	return "schema \"s\"\ntype T {\n\tid String primary\n\tn Integer\n\t! \"m\" " + expression + "\n}\n"
}

// A minus whose next token is a number is that literal's sign, as it is a
// bound's. Before any other operand, and after a left operand, it is an
// operator.
func TestParse_NumberLiteralCarriesItsSign(t *testing.T) {
	t.Parallel()
	for src, want := range map[string]string{
		"-5 < 0":                   `(< -5 0)`,
		"- 5 < 0":                  `(< -5 0)`,
		"-\n5 < 0":                 `(< -5 0)`,
		"-5.5 < 0.0":               `(< -5.5 0)`,
		"-0 == 0":                  `(== 0 0)`,
		"-9223372036854775808 < 0": `(< -9223372036854775808 0)`,
		"--5 < 0":                  `(< (-x -5) 0)`,
		"-(5) < 0":                 `(< (-x 5) 0)`,
		"-n < 0":                   `(< (-x (p "n")) 0)`,
		"n - 5 < 0":                `(< (- (p "n") 5) 0)`,
		"n -5 < 0":                 `(< (- (p "n") 5) 0)`,
		"n - -5 < 0":               `(< (- (p "n") -5) 0)`,
		"[-1, 1] -> Len == 2":      `(== (Len ([] -1 1) args[] params[] <absent>) 2)`,
		"-5 -> Abs == 5":           `(== (Abs -5 args[] params[] <absent>) 5)`,
	} {
		file, issues := Parse([]byte(invariantSource(src)), location.NewSourceID("s.yammm"))
		if len(issues) != 0 {
			t.Errorf("%q: %s", src, messages(issues))
			continue
		}
		if got := sexprString(file.Types[0].Invariants[0].Expr); got != want {
			t.Errorf("%q parsed as %s, want %s", src, got, want)
		}
	}
}

// The sign is read with the digits, so the least int64 is a literal and a
// negative zero float keeps its sign.
func TestParse_SignedLiteralHoldsTheValueItsTextNames(t *testing.T) {
	t.Parallel()
	for src, want := range map[string]any{
		"-9223372036854775808": int64(math.MinInt64),
		"-0":                   int64(0),
		"-1.5":                 -1.5,
		"-0.0":                 math.Copysign(0, -1),
	} {
		file, issues := Parse([]byte(invariantSource("n == "+src)), location.NewSourceID("s.yammm"))
		if len(issues) != 0 {
			t.Errorf("%q: %s", src, messages(issues))
			continue
		}
		root, _ := file.Types[0].Invariants[0].Expr.(expr.SExpr)
		lit, ok := root.Children()[1].(*expr.Literal)
		if !ok {
			t.Errorf("%q: the right operand is %T, want a literal", src, root.Children()[1])
			continue
		}
		if f, isFloat := want.(float64); isFloat {
			got, ok := lit.Val.(float64)
			if !ok || math.Float64bits(got) != math.Float64bits(f) {
				t.Errorf("%q holds %#v, want the float64 %v", src, lit.Val, f)
			}
			continue
		}
		if lit.Val != want {
			t.Errorf("%q holds %#v, want %#v", src, lit.Val, want)
		}
	}
}

// A signed literal out of range is one issue, and its span starts at the sign.
func TestParse_SignedLiteralOutOfRangeIsNamedFromItsSign(t *testing.T) {
	t.Parallel()
	for src, want := range map[string]struct{ text, message string }{
		"-9223372036854775809 < 0": {
			"-9223372036854775809",
			`invalid integer literal: strconv.ParseInt: parsing "-9223372036854775809": value out of range`,
		},
		"- 9223372036854775809 < 0": {
			"- 9223372036854775809",
			`invalid integer literal: strconv.ParseInt: parsing "-9223372036854775809": value out of range`,
		},
		"-1.0e999 < 0.0": {
			"-1.0e999",
			`invalid float literal: strconv.ParseFloat: parsing "-1.0e999": value out of range`,
		},
	} {
		full := invariantSource(src)
		file, issues := Parse([]byte(full), location.NewSourceID("s.yammm"))
		if len(issues) != 1 || issues[0].Code() != diag.E_INVALID_INVARIANT || issues[0].Message() != want.message {
			t.Errorf("%q: %s; want one E_INVALID_INVARIANT %q", src, messages(issues), want.message)
			continue
		}
		sp := issues[0].Span()
		if got := full[sp.Start.Byte:sp.End.Byte]; got != want.text {
			t.Errorf("%q: the issue covers %q, want %q", src, got, want.text)
		}
		if bad := oneLiteralWithNoValue(file); bad != "" {
			t.Errorf("%q: %s", src, bad)
		}
	}
}
