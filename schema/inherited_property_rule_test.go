package schema_test

import (
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/diagtest"
	"github.com/simon-lentz/yammm/schema"
)

// permutations returns every order of names.
func permutations(names []string) [][]string {
	if len(names) <= 1 {
		return [][]string{append([]string(nil), names...)}
	}
	var out [][]string
	for i, first := range names {
		rest := make([]string, 0, len(names)-1)
		rest = append(rest, names[:i]...)
		rest = append(rest, names[i+1:]...)
		for _, p := range permutations(rest) {
			out = append(out, append([]string{first}, p...))
		}
	}
	return out
}

// TestInheritance_TheNarrowestDefinitionIsThePropertyInEveryOrder pins the
// rule for a property a type does not declare: it is the inherited definition
// that narrows every other, in every order of the extends clause, and with none
// the load fails in every order. A type's own definition must narrow every
// inherited one.
func TestInheritance_TheNarrowestDefinitionIsThePropertyInEveryOrder(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name  string
		decls []string // one inherited definition of x a mixin, M0 onwards
		own   string   // T's own definition of x, or ""
		want  string   // the merged x's constraint, or "" for E_PROPERTY_CONFLICT
		req   bool     // whether the merged x is required
	}{
		{"three Integer ranges", []string{"Integer[0, 10]", "Integer[5, 20]", "Integer[6, 8]"}, "", "Integer[6, 8]", false},
		{"three Enums", []string{`Enum["a", "b", "c"]`, `Enum["b", "c", "d"]`, `Enum["b", "c"]`}, "", `Enum["b", "c"]`, false},
		{"ranges and required", []string{"Integer[0, 10] required", "Integer[2, 5]", "Integer[3, 4] required"}, "", "Integer[3, 4]", true},
		{"a required range and an optional one inside it", []string{"Integer[0, 10] required", "Integer[2, 5]"}, "", "", false},
		{"two overlapping ranges", []string{"Integer[0, 10]", "Integer[5, 20]"}, "", "", false},
		{"two equal definitions and a wider one", []string{"String[1, 9]", "String[1, 9]", "String"}, "", "String[1, 9]", false},
		{"an own range inside both", []string{"Integer[0, 10]", "Integer[5, 20]"}, "Integer[6, 8]", "Integer[6, 8]", false},
		{"an own range wider than one", []string{"Integer[6, 8]", "Integer[5, 20]"}, "Integer[0, 10]", "", false},
		{"Integer against Float", []string{"Integer", "Float"}, "", "", false},
		{"primary against required", []string{"String primary", "String required"}, "", "", false},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			var src strings.Builder
			src.WriteString("schema \"s\"\n")
			names := make([]string, len(row.decls))
			for i, d := range row.decls {
				names[i] = fmt.Sprintf("M%d", i)
				fmt.Fprintf(&src, "abstract type %s {\n\tx %s\n}\n", names[i], d)
			}
			for _, order := range permutations(names) {
				body := "\tid String primary\n"
				if row.own != "" {
					body += "\tx " + row.own + "\n"
				}
				text := src.String() + "type T extends " + strings.Join(order, ", ") + " {\n" + body + "}\n"
				s, res := diagtest.Capture(t, func(warn func(diag.Issue)) (schema.Schema, error) {
					return schema.LoadString(t.Context(), text, "s.yammm", schema.WithWarnings(warn))
				})
				if row.want == "" {
					if res.Len() != 1 || !res.HasCode(diag.E_PROPERTY_CONFLICT) {
						t.Errorf("extends %v: want one E_PROPERTY_CONFLICT, got %v", order, res)
					}
					continue
				}
				if s.IsZero() {
					t.Errorf("extends %v: want a load, got %v", order, res)
					continue
				}
				typ, _ := s.Type("T")
				x, _ := typ.Property("x")
				if got := x.Constraint().String(); got != row.want || x.IsRequired() != row.req {
					t.Errorf("extends %v: x is %s (required %v), want %s (required %v)", order, got, x.IsRequired(), row.want, row.req)
				}
			}
		})
	}
}

// TestInheritance_OneConflictPerTypeAndPropertyInEveryOrder pins that a
// property's conflict is one E_PROPERTY_CONFLICT on each type it reaches,
// however many definitions disagree and in every order of the extends clause.
func TestInheritance_OneConflictPerTypeAndPropertyInEveryOrder(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name    string
		mixins  string
		extends []string
		own     string
	}{
		{"three kinds", "abstract type A { x String }\nabstract type B { x Integer }\nabstract type D { x Boolean }\n", []string{"A", "B", "D"}, ""},
		{"three disjoint Enums", "abstract type A { x Enum[\"a\", \"b\"] }\nabstract type B { x Enum[\"c\", \"d\"] }\nabstract type D { x Enum[\"e\", \"f\"] }\n", []string{"A", "B", "D"}, ""},
		{"crossed chains", "abstract type MS1 { x Integer[0,50] }\nabstract type MP1 { x Integer[40,90] }\nabstract type MS2 { x Integer[40,50] }\nabstract type MP2 { x Integer[0,30] }\n", []string{"MS1", "MP1", "MS2", "MP2"}, ""},
		{"a butterfly", "abstract type A1 { x String[0,20] }\nabstract type A2 { x String[5,10] }\nabstract type B1 { x String required }\nabstract type B2 { x String[1,3] required }\n", []string{"A1", "A2", "B1", "B2"}, "\tx Integer\n"},
		{"own wider than two disjoint ranges", "abstract type M1 { x Integer[0,10] }\nabstract type M2 { x Integer[20,30] }\n", []string{"M1", "M2"}, "\tx Integer[0,100]\n"},
		{"a narrowing chain on each side", "abstract type Wide { x String }\nabstract type WideN extends Wide { x String required }\nabstract type Other { x Integer }\nabstract type OtherN extends Other { x Integer required }\n", []string{"Wide", "WideN", "Other", "OtherN"}, ""},
		{"one definition through both arms of a diamond", "abstract type Base { x Integer }\nabstract type P1 extends Base { p1 String }\nabstract type P2 extends Base { p2 String }\nabstract type Wide { x String }\n", []string{"Wide", "P1", "P2"}, ""},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			for _, order := range permutations(row.extends) {
				res := loadStringErr(t, "schema \"s\"\n"+row.mixins+"type T extends "+strings.Join(order, ", ")+" {\n\tid String primary\n"+row.own+"}\n")
				wantCounts(t, res, map[diag.Code]int{diag.E_PROPERTY_CONFLICT: 1})
			}
		})
	}
}

// TestInheritance_AConflictNamesItsDefinitions pins what one conflict names,
// at the type's declaration. With no own definition it names every inherited definition, in
// linearization order, and two of them in its message. With an own definition
// it names that and each inherited definition it does not narrow.
func TestInheritance_AConflictNamesItsDefinitions(t *testing.T) {
	t.Parallel()
	const mixins = `schema "s"
abstract type M1 { x Integer[0,50] }
abstract type M2 { x Integer[0,40] }
abstract type M3 { x Integer[60,90] }
abstract type M4 { x Integer[70,80] }
`
	related := func(res diag.Result) (string, map[int]string) {
		lines := make(map[int]string)
		var msg string
		for i := range res.Issues() {
			if i.Code() == diag.E_PROPERTY_CONFLICT {
				msg = i.Message()
				if got := i.Span().Start.Line; got != 6 {
					t.Errorf("the conflict stands at line %d, want the type's, 6", got)
				}
				for _, r := range i.Related() {
					lines[r.Span.Start.Line] = r.Message
				}
			}
		}
		return msg, lines
	}

	res := loadStringErr(t, mixins+"type T extends M1, M2, M3, M4 { id String primary }\n")
	msg, lines := related(res)
	if want := `type "T" inherits definitions of property "x" from M2 and M3, and neither narrows the other`; msg != want {
		t.Errorf("message %q, want %q", msg, want)
	}
	want := map[int]string{2: "definition declared here", 3: "definition declared here", 4: "definition declared here", 5: "definition declared here"}
	if !maps.Equal(lines, want) {
		t.Errorf("related %v, want %v", lines, want)
	}

	res = loadStringErr(t, mixins+"type T extends M1, M2, M3, M4 {\n\tid String primary\n\tx Integer[0,45]\n}\n")
	msg, lines = related(res)
	if want := `type "T" declares property "x", which does not narrow its definition from M2`; msg != want {
		t.Errorf("message %q, want %q", msg, want)
	}
	want = map[int]string{
		8: "the type's own definition",
		3: "inherited definition it does not narrow",
		4: "inherited definition it does not narrow",
		5: "inherited definition it does not narrow",
	}
	if !maps.Equal(lines, want) {
		t.Errorf("related %v, want %v", lines, want)
	}
}

// TestInheritance_EachTypeAConflictReachesReportsIt pins that a type below a
// conflicting one, which does not declare the property, reports the conflict
// too: its own rule reads the same definitions.
func TestInheritance_EachTypeAConflictReachesReportsIt(t *testing.T) {
	t.Parallel()
	res := loadStringErr(t, `schema "s"
abstract type M1 { x String }
abstract type M2 { x Integer }
abstract type C extends M1, M2 { c String }
type D extends C { id String primary }
`)
	wantCounts(t, res, map[diag.Code]int{diag.E_PROPERTY_CONFLICT: 2})
}

// TestInheritance_AnUnknownDefinitionAnswersAlikeInEveryOrder pins that a
// property no rule decides, with a definition of unknown type, reads as that
// definition in every order of the extends clause: a rule that judges the
// property's kind defers to the unknown type's own issue.
func TestInheritance_AnUnknownDefinitionAnswersAlikeInEveryOrder(t *testing.T) {
	t.Parallel()
	for _, order := range []string{"A, B", "B, A"} {
		res := loadStringErr(t, `schema "s"
abstract type A { x Nope }
abstract type B { x Vector[3] }
type T extends `+order+` {
	id String primary
	@@index(x)
}
`)
		wantCounts(t, res, map[diag.Code]int{diag.E_UNKNOWN_TYPE: 1})
	}
}
