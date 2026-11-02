package schema_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

type referenceAnswer uint8

const (
	// refResolves: a source states the name and it names a datatype.
	refResolves referenceAnswer = iota
	// refNamesNothing: a source states the name and it names no datatype.
	refNamesNothing
	// refNotAName: no source states the name.
	refNotAName
)

// referenceSpellings holds each datatype reference name with the answer the
// DSL gives the nearest source, in a schema that declares the datatype T1 and
// imports, as lib, a schema declaring the datatype Zip.
var referenceSpellings = []struct {
	name string
	want referenceAnswer
}{
	{"T1", refResolves},
	{"lib.Zip", refResolves},
	{"Nope", refNamesNothing},
	{"T_1", refNamesNothing},
	{"lib.Nope", refNamesNothing},
	{"lib.T1", refNamesNothing},
	{"other.Zip", refNamesNothing},
	{"Lib.Zip", refNamesNothing},
	{"T1.T1", refNamesNothing},
	{".T1", refNotAName},
	{"T1.", refNotAName},
	{"lib.", refNotAName},
	{".", refNotAName},
	{"", refNotAName},
	{"a..b", refNotAName},
	{"a.b.c", refNotAName},
	{".lib.Zip", refNotAName},
	{"lib..Zip", refNotAName},
	{" T1", refNotAName},
	{"T1 ", refNotAName},
	{"T1\x00", refNotAName},
	{"t1", refNotAName},
	{"1T", refNotAName},
	{"T-1", refNotAName},
	{"Ünit", refNotAName},
	{"lib.zip", refNotAName},
	{"lib.String", refNotAName},
	{"String.Zip", refNotAName},
	{"type.Zip", refNotAName},
	{"1lib.Zip", refNotAName},
}

const referenceLibSource = "schema \"lib\"\n\ntype Zip = Pattern[\"^[0-9]{5}$\"]\n"

// referencePositions holds each place a source writes a datatype reference: the
// DSL text around the name, and the constraint that holds the same reference
// where a Go caller can state one.
var referencePositions = []struct {
	name string
	dsl  func(ref string) string
	wrap func(c schema.Constraint) schema.Constraint
}{
	{
		name: "a property",
		dsl:  func(ref string) string { return "type Car {\n\tvin String primary\n\tx " + ref + "\n}\n" },
		wrap: func(c schema.Constraint) schema.Constraint { return c },
	},
	{
		name: "a List element",
		dsl:  func(ref string) string { return "type Car {\n\tvin String primary\n\tx List<" + ref + ">\n}\n" },
		wrap: func(c schema.Constraint) schema.Constraint { return schema.NewListConstraint(c) },
	},
	{
		name: "a List element at depth two",
		dsl: func(ref string) string {
			return "type Car {\n\tvin String primary\n\tx List<List<" + ref + ">>[1, 3]\n}\n"
		},
		wrap: func(c schema.Constraint) schema.Constraint {
			return schema.ListLenBetween(schema.NewListConstraint(c), 1, 3)
		},
	},
	{
		name: "a datatype",
		dsl: func(ref string) string {
			return "type X = List<" + ref + ">\n\ntype Car {\n\tvin String primary\n}\n"
		},
		wrap: func(c schema.Constraint) schema.Constraint { return schema.NewListConstraint(c) },
	},
	{
		name: "an edge property",
		dsl: func(ref string) string {
			return "type Car {\n\tvin String primary\n\t--> NEAR (many) Car {\n\t\tx " + ref + "\n\t}\n}\n"
		},
	},
}

// dslStates loads the source that writes ref at one position and reports the
// DSL's answer: whether a source states the name, and whether it resolves.
func dslStates(t *testing.T, ref, body string) referenceAnswer {
	t.Helper()
	main := "schema \"main\"\n\nimport \"./lib\" as lib\n\ntype T1 = Integer[1, 5]\n\n" + body
	s, res := schema.LoadSourcesWithEntry(t.Context(),
		map[string][]byte{"main.yammm": []byte(main), "lib.yammm": []byte(referenceLibSource)},
		"main.yammm", "", schema.WithSourcesOnly(true), schema.WithSyntheticRoot("test://refs"))
	if !s.IsZero() && !res.HasErrors() {
		if stated := statedReference(t, s); stated != ref {
			return refNotAName // the source states another name
		}
		return refResolves
	}
	for issue := range res.Issues() {
		if issue.Code() == diag.E_UNKNOWN_TYPE && strings.HasPrefix(issue.Message(), fmt.Sprintf("unknown type %q in datatype reference", ref)) {
			return refNamesNothing
		}
	}
	return refNotAName
}

// statedReference returns the reference name the loaded source holds: in the
// datatype X, or else at the property x of Car or of its NEAR edge.
func statedReference(t *testing.T, s schema.Schema) string {
	t.Helper()
	held := func(c schema.Constraint) string {
		for {
			list, isList := c.(schema.ListConstraint)
			if !isList {
				return c.String()
			}
			c = list.Element()
		}
	}
	if dt, ok := s.DataType("X"); ok {
		return held(dt.Constraint())
	}
	car, _ := s.Type("Car")
	if p, ok := car.Property("x"); ok {
		return held(p.Constraint())
	}
	near, ok := car.Relation("NEAR")
	if !ok {
		t.Fatal("the source holds no reference")
	}
	p, _ := near.Property("x")
	return held(p.Constraint())
}

// A datatype reference name is read alike by the DSL and CheckConstraint: what a
// .yammm source cannot state, CheckConstraint refuses as a constraint fault.
func TestReferenceName_CheckConstraintGivesTheDSLsAnswer(t *testing.T) {
	for _, sp := range referenceSpellings {
		for _, pos := range referencePositions {
			t.Run(fmt.Sprintf("%q as %s", sp.name, pos.name), func(t *testing.T) {
				if got := dslStates(t, sp.name, pos.dsl(sp.name)); got != sp.want {
					t.Fatalf("the DSL answers %d for the nearest source; the table says %d", got, sp.want)
				}
				if pos.wrap == nil {
					return
				}
				raw := pos.wrap(schema.NewAliasConstraint(sp.name, schema.NewStringConstraint()))
				err := schema.CheckConstraint(raw)
				if refused := sp.want == refNotAName; (err != nil) != refused {
					t.Errorf("CheckConstraint = %v; want a refusal: %t", err, refused)
				}
			})
		}
	}
}

// The zero AliasConstraint meets no constructor; its empty name is refused as
// NewAliasConstraint("", nil) is.
func TestReferenceName_TheZeroReferenceIsRefusedAsAnEmptyName(t *testing.T) {
	for name, c := range map[string]schema.Constraint{
		"the zero value": schema.AliasConstraint{},
		"an empty name":  schema.NewAliasConstraint("", schema.NewStringConstraint()),
	} {
		const want = `datatype reference "" is not one a .yammm source can write`
		if err := schema.CheckConstraint(c); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: CheckConstraint = %v; want %s", name, err, want)
		}
	}
}
