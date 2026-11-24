package schema_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/diagtest"
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

// referencePositions holds each place a source writes a datatype reference, as
// the DSL text around the name.
var referencePositions = []struct {
	name string
	dsl  func(ref string) string
}{
	{
		name: "a property",
		dsl:  func(ref string) string { return "type Car {\n\tvin String primary\n\tx " + ref + "\n}\n" },
	},
	{
		name: "a List element",
		dsl:  func(ref string) string { return "type Car {\n\tvin String primary\n\tx List<" + ref + ">\n}\n" },
	},
	{
		name: "a List element at depth two",
		dsl: func(ref string) string {
			return "type Car {\n\tvin String primary\n\tx List<List<" + ref + ">>[1, 3]\n}\n"
		},
	},
	{
		name: "a datatype",
		dsl: func(ref string) string {
			return "type X = List<" + ref + ">\n\ntype Car {\n\tvin String primary\n}\n"
		},
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
	s, res := diagtest.Capture(t, func(warn func(diag.Issue)) (schema.Schema, error) {
		return schema.LoadSourcesWithEntry(t.Context(),
			map[string][]byte{"main.yammm": []byte(main), "lib.yammm": []byte(referenceLibSource)},
			"main.yammm", schema.WithSourcesOnly(true), schema.WithSyntheticRoot("test://refs"), schema.WithWarnings(warn))
	})
	if !s.IsZero() && diagtest.Err(res) == nil {
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

// Each reference spelling draws the DSL's recorded answer at every position a
// source writes a reference.
func TestReferenceName_TheDSLGivesTheRecordedAnswer(t *testing.T) {
	for _, sp := range referenceSpellings {
		for _, pos := range referencePositions {
			t.Run(fmt.Sprintf("%q as %s", sp.name, pos.name), func(t *testing.T) {
				if got := dslStates(t, sp.name, pos.dsl(sp.name)); got != sp.want {
					t.Errorf("the DSL answers %d for the nearest source; the table says %d", got, sp.want)
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
		"an empty name":  schema.TestNewAliasConstraint("", schema.TestNewStringConstraint()),
	} {
		const want = `datatype reference "" is not one a .yammm source can write`
		if err := schema.CheckConstraint(c); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: CheckConstraint = %v; want %s", name, err, want)
		}
	}
}
