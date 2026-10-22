package neo4j

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/schema"
)

type faultRow struct {
	name string
	c    schema.Constraint
}

// faultRows holds, for every constraint kind, each fault the judge reports,
// orders in which two faults of one constraint meet, and one accepted
// constraint of the kind.
func faultRows() []faultRow {
	re := func(sources ...string) []*regexp.Regexp {
		out := make([]*regexp.Regexp, len(sources))
		for i, s := range sources {
			out[i] = regexp.MustCompile(s)
		}
		return out
	}
	enum := func(values ...string) schema.Constraint { return schema.NewEnumConstraint(values) }
	str := schema.NewStringConstraint()
	two := schema.NewEnumConstraint([]string{"a", "b"})
	list := schema.NewListConstraint(str)
	later := []string{"a", "b"}
	changedLater := schema.NewEnumConstraint(later)
	later[1] = "a"

	return []faultRow{
		{"nil", nil},

		{"enum accepted", enum("a", "b")},
		{"enum zero value", schema.EnumConstraint{}},
		{"enum nil values", schema.NewEnumConstraint(nil)},
		{"enum no values", enum()},
		{"enum one value", enum("a")},
		{"enum one empty value", enum("")},
		{"enum one value not UTF-8", enum("\xff")},
		{"enum empty first", enum("", "a")},
		{"enum empty last", enum("a", "")},
		{"enum two empty", enum("", "")},
		{"enum not UTF-8", enum("a", "\xff")},
		{"enum duplicate", enum("a", "b", "a")},
		{"enum duplicate pair", enum("a", "a")},
		{"enum duplicate before empty", enum("a", "a", "")},
		{"enum empty before duplicate", enum("a", "", "a")},
		{"enum not UTF-8 twice", enum("\xff", "\xff")},
		{"enum duplicate before not UTF-8", enum("a", "a", "\xff")},
		{"enum not UTF-8 before empty", enum("\xff", "")},
		{"enum empty before not UTF-8", enum("", "\xff")},
		{"enum values changed after construction", changedLater},
		{"enum pointer", &two},
		{"enum fault in a List", schema.NewListConstraint(enum("a"))},
		{"enum fault in a List of a List", schema.NewListConstraint(schema.NewListConstraint(enum("a", "a")))},
		{"enum fault behind a List length fault", schema.ListLenBetween(enum("a"), 5, 1)},
		{"enum fault behind a reference", schema.NewAliasConstraint("Kind", enum("a"))},
		{"enum accepted behind a reference", schema.NewAliasConstraint("Kind", enum("a", "b"))},

		{"boolean accepted", schema.NewBooleanConstraint()},
		{"date accepted", schema.NewDateConstraint()},
		{"uuid accepted", schema.NewUUIDConstraint()},

		{"timestamp accepted", schema.NewTimestampConstraintFormatted("2006-01-02")},
		{"timestamp format not UTF-8", schema.NewTimestampConstraintFormatted("\xff")},

		{"integer accepted", schema.IntegerBetween(1, 5)},
		{"integer inverted", schema.IntegerBetween(5, 1)},

		{"float accepted", schema.FloatBetween(0.5, 2.5)},
		{"float infinite max", schema.FloatMax(math.Inf(1))},
		{"float infinite min", schema.FloatMin(math.Inf(-1))},
		{"float NaN min", schema.FloatBetween(math.NaN(), 1)},
		{"float NaN max", schema.FloatBetween(0, math.NaN())},
		{"float inverted", schema.FloatBetween(2.5, 1)},
		{"float infinite min before infinite max", schema.FloatBetween(math.Inf(1), math.Inf(-1))},

		{"string accepted", schema.StringLenBetween(1, 5)},
		{"string negative min", schema.StringMinLen(-1)},
		{"string negative max", schema.StringMaxLen(-1)},
		{"string inverted", schema.StringLenBetween(5, 1)},
		{"string negative min before negative max", schema.StringLenBetween(-1, -2)},

		{"list accepted", schema.ListLenBetween(str, 1, 5)},
		{"list zero value", schema.ListConstraint{}},
		{"list no element", schema.NewListConstraint(nil)},
		{"list negative min", schema.ListMinLen(str, -1)},
		{"list negative max", schema.ListMaxLen(str, -1)},
		{"list inverted", schema.ListLenBetween(str, 5, 1)},
		{"list inverted before no element", schema.ListLenBetween(nil, 5, 1)},
		{"list negative min before negative max", schema.ListLenBetween(str, -1, -2)},
		{"list element fault", schema.NewListConstraint(schema.IntegerBetween(5, 1))},
		{"list element fault at depth two", schema.NewListConstraint(schema.NewListConstraint(schema.StringMinLen(-1)))},
		{"list pointer", &list},

		{"pattern accepted", schema.NewPatternConstraint(re("^a", "b$"))},
		{"pattern zero value", schema.PatternConstraint{}},
		{"pattern none", schema.NewPatternConstraint(nil)},
		{"pattern three", schema.NewPatternConstraint(re("a", "b", "c"))},
		{"pattern Perl syntax refuses", schema.NewPatternConstraint([]*regexp.Regexp{regexp.MustCompilePOSIX("a**")})},
		{"pattern surrogate", schema.NewPatternConstraint(re(`\x{D800}`))},
		{"pattern three before one refused", schema.NewPatternConstraint([]*regexp.Regexp{regexp.MustCompile("a"), regexp.MustCompile("b"), regexp.MustCompilePOSIX("a**")})},
		{"pattern two refused", schema.NewPatternConstraint([]*regexp.Regexp{regexp.MustCompilePOSIX("a**"), regexp.MustCompilePOSIX("b**")})},
		{"pattern surrogate before one refused", schema.NewPatternConstraint([]*regexp.Regexp{regexp.MustCompile(`\x{DFFF}`), regexp.MustCompilePOSIX("a**")})},
		{"pattern refused before surrogate", schema.NewPatternConstraint([]*regexp.Regexp{regexp.MustCompilePOSIX("a**"), regexp.MustCompile(`\x{DFFF}`)})},

		{"vector accepted", schema.NewVectorConstraint(3)},
		{"vector zero", schema.NewVectorConstraint(0)},
		{"vector negative", schema.NewVectorConstraint(-1)},
		{"vector past the maximum", schema.NewVectorConstraint(65537)},

		{"pointer to a string", &str},

		{"reference accepted", schema.NewAliasConstraint("Code", schema.IntegerBetween(1, 5))},
		{"reference unresolved", schema.NewAliasConstraint("Code", nil)},
		{"reference unresolved in a List", schema.NewListConstraint(schema.NewAliasConstraint("Code", nil))},
		{"reference resolved to a fault", schema.NewAliasConstraint("Code", schema.IntegerBetween(5, 1))},
		{"reference resolved to an unresolved reference", schema.NewAliasConstraint("Code", schema.NewAliasConstraint("Inner", nil))},
		{"reference zero value", schema.AliasConstraint{}},
		{"reference name no source writes", schema.NewAliasConstraint("lower", schema.NewStringConstraint())},
		{"reference name no source writes, unresolved", schema.NewAliasConstraint("a.b.C", nil)},
		{"reference name no source writes in a List", schema.NewListConstraint(schema.NewAliasConstraint("type.Code", schema.NewStringConstraint()))},
		{"reference resolved through a name no source writes", schema.NewAliasConstraint("Code", schema.NewAliasConstraint("lower", schema.NewStringConstraint()))},
		{"reference resolved through a fault behind a name no source writes", schema.NewAliasConstraint("Code", schema.NewAliasConstraint("lower", schema.IntegerBetween(5, 1)))},
		{"reference resolved to a well-formed reference", schema.NewAliasConstraint("Code", schema.NewAliasConstraint("Inner", schema.NewStringConstraint()))},
		{"reference resolved to a reference of its own name", schema.NewAliasConstraint("Code", schema.NewAliasConstraint("Code", schema.NewStringConstraint()))},
		{"reference resolved to a reference in a List", schema.NewListConstraint(schema.NewAliasConstraint("Code", schema.NewAliasConstraint("Inner", schema.NewStringConstraint())))},
	}
}

func buildAnswer(build func(*schema.Builder) *schema.Builder) string {
	s, res := build(schema.NewBuilder().WithName("fleet")).Build()
	if s != nil && res.OK() {
		return "ok"
	}
	var out []string
	for issue := range res.Issues() {
		out = append(out, fmt.Sprintf("%s: %s", issue.Code(), issue.Message()))
	}
	return strings.Join(out, " ;; ")
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
	for _, row := range faultRows() {
		fmt.Fprintf(&b, "%s\n", row.name)
		fmt.Fprintf(&b, "\tBuild property: %s\n", buildAnswer(func(sb *schema.Builder) *schema.Builder {
			return sb.AddType("Car").WithPrimaryKey("vin", schema.NewStringConstraint()).WithProperty("v", row.c).Done()
		}))
		fmt.Fprintf(&b, "\tBuild datatype: %s\n", buildAnswer(func(sb *schema.Builder) *schema.Builder {
			return sb.AddDataType("V", row.c).AddType("Car").WithPrimaryKey("vin", schema.NewStringConstraint()).Done()
		}))
		fmt.Fprintf(&b, "\tCheckConstraint: %s\n", errAnswer(schema.CheckConstraint(row.c)))
		got, err := Coerce(row.c, "x")
		fmt.Fprintf(&b, "\tCoerce: %#v, %s\n", got, errAnswer(err))
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
		"a0": schema.NewStringConstraint(),
		"b1": nil,
		"c2": schema.NewEnumConstraint([]string{"x", "x"}),
		"c3": schema.IntegerBetween(5, 1),
		"d4": schema.NewListConstraint(nil),
		"e5": schema.NewEnumConstraint([]string{"x", "y"}),
	}
	const want = `param type "c2": schema: duplicate enum value "x"`
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
	got, err := CoerceParams(empty, ParamTypes{"a": schema.NewStringConstraint()})
	if err != nil || got == nil || reflect.ValueOf(got).UnsafePointer() != reflect.ValueOf(empty).UnsafePointer() {
		t.Errorf("CoerceParams(empty map) = %#v, %v; want the same map and no error", got, err)
	}
	refused := schema.IntegerBetween(5, 1)
	_, err = CoerceParams(nil, ParamTypes{"a": refused})
	judge := schema.CheckConstraint(refused)
	if err == nil || errors.Unwrap(err) == nil || errors.Unwrap(err).Error() != judge.Error() {
		t.Errorf("CoerceParams = %v; want an error wrapping %v", err, judge)
	}
}
