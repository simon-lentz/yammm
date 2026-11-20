package neo4j

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/schematest"

	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/schema"
)

type faultRow struct {
	name string
	c    schema.Constraint
}

// faultRows holds, for every constraint kind, one accepted constraint of the
// kind and each fault a caller can hold: the zero value of a kind that needs an
// argument, and a pointer to a constraint. The DSL refuses every other fault,
// so no loaded schema holds one.
func faultRows(tb testing.TB) []faultRow {
	tb.Helper()
	str := schematest.Constraint(tb, "String").(schema.StringConstraint)
	two := schematest.Constraint(tb, `Enum["a", "b"]`).(schema.EnumConstraint)
	list := schematest.Constraint(tb, "List<String>").(schema.ListConstraint)
	stamp := schematest.Constraint(tb, "Timestamp").(schema.TimestampConstraint)

	return []faultRow{
		{"nil", nil},

		{"enum accepted", two},
		{"enum zero value", schema.EnumConstraint{}},
		{"enum pointer", &two},
		{"enum accepted behind a reference", schematest.Constraint(tb, "Kind", `type Kind = Enum["a", "b"]`)},

		{"boolean accepted", schematest.Constraint(tb, "Boolean")},
		{"date accepted", schematest.Constraint(tb, "Date")},
		{"uuid accepted", schematest.Constraint(tb, "UUID")},

		{"timestamp accepted", stamp},
		{"timestamp in a List", schematest.Constraint(tb, "List<Timestamp>")},
		{"timestamp pointer", &stamp},

		{"integer accepted", schematest.Constraint(tb, "Integer[1, 5]")},
		{"float accepted", schematest.Constraint(tb, "Float[0.5, 2.5]")},
		{"string accepted", schematest.Constraint(tb, "String[1, 5]")},

		{"list accepted", schematest.Constraint(tb, "List<String>[1, 5]")},
		{"list zero value", schema.ListConstraint{}},
		{"list pointer", &list},

		{"pattern accepted", schematest.Constraint(tb, `Pattern["^a"]`)},
		{"pattern zero value", schema.PatternConstraint{}},
		{"pattern surrogate escape accepted", schematest.Constraint(tb, `Pattern["\\x{D800}"]`)},

		{"vector accepted", schematest.Constraint(tb, "Vector[3]")},
		{"vector zero value", schema.VectorConstraint{}},

		{"pointer to a string", &str},

		{"reference accepted", schematest.Constraint(tb, "Code", "type Code = Integer[1, 5]")},
		{"reference zero value", schema.AliasConstraint{}},
		{"reference in a List", schematest.Constraint(tb, "List<Code>", "type Code = String")},
	}
}

func errAnswer(err error) string {
	if err == nil {
		return "ok"
	}
	return err.Error()
}

// The judge's answers are held to a record: the message each door gives for
// each fault, and which fault it reports first where a constraint holds two.
func TestConstraintFaults_EveryDoorGivesTheRecordedAnswer(t *testing.T) {
	var b strings.Builder
	for _, row := range faultRows(t) {
		fmt.Fprintf(&b, "%s\n", row.name)
		fmt.Fprintf(&b, "\tCheckConstraint: %s\n", errAnswer(schema.CheckConstraint(row.c)))
		params, err := CoerceParams(map[string]any{"v": "x"}, ParamTypes{"v": row.c})
		fmt.Fprintf(&b, "\tCoerceParams: %v, %s\n", params, errAnswer(err))
	}
	yammmtest.Golden(t, "constraint_faults", []byte(b.String()))
}

// Of several refused keys CoerceParams names the first in sorted order, with
// that key's own fault, whatever the map's iteration order and whichever keys
// params holds.
func TestCoerceParams_NamesTheFirstRefusedKeyWithItsOwnFault(t *testing.T) {
	types := ParamTypes{
		"a0": schematest.Constraint(t, "String"),
		"b1": nil,
		"c2": schema.EnumConstraint{},
		"c3": schema.PatternConstraint{},
		"d4": schema.ListConstraint{},
		"e5": schematest.Constraint(t, `Enum["x", "y"]`),
	}
	const want = `param type "c2": schema: enum must have at least two values (got 0)`
	for range 50 {
		for _, params := range []map[string]any{nil, {}, {"a0": "s"}, {"d4": []any{"s"}, "zz": 1}} {
			got, err := CoerceParams(params, types)
			if got != nil || err == nil || err.Error() != want {
				t.Fatalf("CoerceParams(%v) = %v, %v; want nil and %s", params, got, err, want)
			}
		}
	}
	delete(types, "c2")
	if _, err := CoerceParams(nil, types); err == nil || !strings.HasPrefix(err.Error(), `param type "c3": `) {
		t.Errorf("CoerceParams = %v; want an error naming \"c3\"", err)
	}
	delete(types, "c3")
	delete(types, "d4")
	if got, err := CoerceParams(map[string]any{"a0": "s"}, types); err != nil || got["a0"] != "s" {
		t.Errorf("CoerceParams over accepted types = %v, %v", got, err)
	}
}

// An empty params whose types are all accepted comes back as the caller's own
// map, and a refusal wraps the judge's error, so errors.Unwrap reaches it.
func TestCoerceParams_ReturnsAnEmptyParamsAsGivenAndWrapsTheJudgesError(t *testing.T) {
	empty := map[string]any{}
	got, err := CoerceParams(empty, ParamTypes{"a": schematest.Constraint(t, "String")})
	if err != nil || got == nil || reflect.ValueOf(got).UnsafePointer() != reflect.ValueOf(empty).UnsafePointer() {
		t.Errorf("CoerceParams(empty map) = %#v, %v; want the same map and no error", got, err)
	}
	refused := schema.ListConstraint{}
	_, err = CoerceParams(nil, ParamTypes{"a": refused})
	judge := schema.CheckConstraint(refused)
	if err == nil || errors.Unwrap(err) == nil || errors.Unwrap(err).Error() != judge.Error() {
		t.Errorf("CoerceParams = %v; want an error wrapping %v", err, judge)
	}
}
