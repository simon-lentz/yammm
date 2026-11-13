package instance

import (
	"os"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance/internal/eval"
	"github.com/simon-lentz/yammm/internal/expr"
	"github.com/simon-lentz/yammm/internal/parse"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// The static invariant checker and the evaluator implement one scope contract,
// and this file judges every row by both. An accept row loads, holds on the
// good instance, and fails on the bad instance where one is given, so it is
// not vacuous. A refuse row is refused at load AND cannot hold on the good
// instance when the evaluator runs it, which is what makes the refusal right.
// The schema package's static table pins the checker alone; a row added there
// belongs here too.

const contractBase = `schema "s"

part type Item {
    id String primary
    sku String
}

part type Line {
    id String primary
    qty Integer
    tags List<String>
    *-> ITEM (one) Item
}

type Customer {
    id String primary
    name String
}

type Region {
    code String primary
    zone String primary
}

abstract type Named {
    label String
}

abstract type Stamped {
    at String
}

part type Alt extends Named, Stamped {
    id String primary
    extra String
}

part type Other extends Named, Stamped {
    id String primary
    other String
}

// StringTagged and NumberTagged share "label" from Named at one kind and
// declare "tag" at DISJOINT kinds. Alt and Other take every shared member
// from the same two abstract bases, so no pair in this corpus could express
// a union whose member read disagrees.
part type StringTagged extends Named {
    id String primary
    tag String
}

part type NumberTagged extends Named {
    id String primary
    tag Integer
}

type Order {
    id String primary
    name String
    f1 Boolean
    f2 Boolean
    tags List<String>
    matrix List<List<Integer>>
    note String
    extras List<String>
    vec Vector[4]
    *-> LINES (one:many) Line
    *-> MAIN_LINE (one) Line
    *-> ALT (_) Alt
    *-> OTHER (_) Other
    *-> STAG (_) StringTagged
    *-> NTAG (_) NumberTagged
    // ABSENT_STAG is the slot goodOrder leaves empty, so a guard over it takes
    // the fallback and the conforming instance selects the OTHER alternative.
    *-> ABSENT_STAG (_) StringTagged
    --> PLACED_BY (one) Customer
    --> CUSTOMERS (one:many) Customer
    --> REGION (one) Region
`

func contractSource(inv string) string {
	src := contractBase
	if inv != "" {
		src += "    ! \"m\" " + inv + "\n"
	}
	return src + "}\n"
}

func goodOrder() map[string]any {
	line := func(id string, qty int64, sku string) map[string]any {
		return map[string]any{
			"id": id, "qty": qty, "tags": []any{"t"},
			"item": []any{map[string]any{"id": "i-" + id, "sku": sku}},
		}
	}
	return map[string]any{
		"id": "o1", "name": "north", "f1": true, "f2": false, "tags": []any{"alpha", "beta"},
		"matrix":    []any{[]any{int64(1), int64(2)}, []any{int64(3)}},
		"vec":       []any{1.5, 2.5, 3.0, 3.0},
		"lines":     []any{line("l1", 5, "s1"), line("l2", 7, "s2")},
		"main_line": []any{line("m1", 9, "s3")},
		"alt":       []any{map[string]any{"id": "a1", "extra": "e1", "label": "L", "at": "t1"}},
		"stag":      []any{map[string]any{"id": "st1", "tag": "T", "label": "LS"}},
		"ntag":      []any{map[string]any{"id": "nt1", "tag": int64(7), "label": "LN"}},
		"placed_by": map[string]any{"_target_id": "c1"},
		"customers": []any{map[string]any{"_target_id": "c1"}, map[string]any{"_target_id": "c2"}},
		"region":    map[string]any{"_target_code": "r1", "_target_zone": "z1"},
	}
}

type mutation func(order map[string]any)

func firstLine(o map[string]any) map[string]any { return o["lines"].([]any)[0].(map[string]any) }

func setLineQty(q int64) mutation { return func(o map[string]any) { firstLine(o)["qty"] = q } }
func setMainQty(q int64) mutation {
	return func(o map[string]any) { o["main_line"].([]any)[0].(map[string]any)["qty"] = q }
}

func dropMainTags(o map[string]any) { delete(o["main_line"].([]any)[0].(map[string]any), "tags") }

func setItemSku(sku string) mutation {
	return func(o map[string]any) { firstLine(o)["item"].([]any)[0].(map[string]any)["sku"] = sku }
}

func setLineTags(tags ...any) mutation { return func(o map[string]any) { firstLine(o)["tags"] = tags } }
func setName(n string) mutation        { return func(o map[string]any) { o["name"] = n } }
func setTags(tags ...any) mutation     { return func(o map[string]any) { o["tags"] = tags } }
func setNote(n string) mutation        { return func(o map[string]any) { o["note"] = n } }
func setExtras(x ...any) mutation      { return func(o map[string]any) { o["extras"] = x } }
func setF1(b bool) mutation            { return func(o map[string]any) { o["f1"] = b } }

// dropName removes the property outright, which is how a guard's fallback
// arm and a nil receiver are reached on an otherwise conforming instance.
func dropName(o map[string]any) { delete(o, "name") }

func setMainField(field string, v any) mutation {
	return func(o map[string]any) { o["main_line"].([]any)[0].(map[string]any)[field] = v }
}

// setAlt overwrites one field of the alt part with a value no row expects.
func setAlt(field string) mutation {
	return func(o map[string]any) { o["alt"].([]any)[0].(map[string]any)[field] = "x" }
}

func setPlacedBy(id string) mutation {
	return func(o map[string]any) { o["placed_by"] = map[string]any{"_target_id": id} }
}

func validateOrder(t *testing.T, s schema.Schema, order map[string]any) diag.Result {
	t.Helper()
	_, res := NewValidator(s).ValidateOne(t.Context(), "Order", RawInstance{Properties: order})
	return res
}

func TestInvariantContract_AcceptRows(t *testing.T) {
	t.Parallel()

	rows := []struct {
		inv string
		bad mutation // turns the good instance into one the rule must fail on
	}{
		// the argument rule admits the right kinds, and the evaluator honours them
		{`name -> TrimPrefix("n") == "orth"`, setName("x")},
		{`name -> Substring(1) == "orth"`, setName("x")},
		{`name -> Substring(1, 3) == "or"`, setName("x")},
		{`name -> Match(/n.*/) -> Len > 0`, setName("x")},
		{`name -> Replace("n", "N") == "North"`, setName("x")},
		{`name -> Split("r") -> Len == 2`, setName("x")},
		{`name -> Compare("m") > 0`, setName("a")},
		{`name -> Min("z") == "north"`, setName("zz")},
		// compositions: children are instances, a (one) composition the single child
		{`LINES -> All |$l| { $l.qty > 0 }`, setLineQty(0)},
		{`LINES -> All { $0.qty > 0 }`, setLineQty(0)},
		{`LINES[0].qty > 0`, setLineQty(0)},
		{`MAIN_LINE.qty > 0`, setMainQty(0)},
		{`LINES -> First.qty > 0`, setLineQty(0)},
		{`LINES -> Filter |$l| { $l.qty > 6 } -> All |$l| { $l.id != "" }`, nil},
		{`LINES -> Map |$l| { $l.qty } -> Sum > 11`, setLineQty(0)},
		{`LINES -> Reduce(0) |$acc, $l| { $acc + $l.qty } > 11`, setLineQty(0)},
		{`LINES -> All |$l| { $l.tags[0] != "" }`, setLineTags("")},
		{`LINES -> All |$l| { $l.tags -> Len > 0 }`, setLineTags()},
		// nested compositions: a child is an instance with its own relations
		{`LINES -> Map |$l| { $l.ITEM } -> All |$x| { $x.sku != "" }`, setItemSku("")},
		{`LINES -> All |$l| { $l.ITEM.sku != "" }`, setItemSku("")},
		{`MAIN_LINE.ITEM.sku == "s3"`, nil},
		// member then pipeline, member then index
		{`$self.name -> Len > 0`, setName("")},
		{`$self.tags[0] != ""`, setTags("")},
		{`name -> Then |$n| { $n -> Len > 0 }`, setName("")},
		{`MAIN_LINE -> Then |$l| { $l.qty > 0 }`, setMainQty(0)},
		// Lest evaluates its body in the caller's scope and binds nothing
		{`(note -> Lest { name -> Upper }) == "NORTH"`, setName("x")},
		{`(note -> Lest { name }) == "north"`, setName("x")},
		// associations: keys are answerable for presence, count and comparison
		{`PLACED_BY != nil`, nil},
		{`CUSTOMERS -> Len > 1`, nil},
		{`CUSTOMERS -> All |$c| { $c != nil }`, nil},
		{`PLACED_BY == "c1"`, setPlacedBy("c9")},
		// relation names resolve in either case
		{`lines -> Len > 0`, nil},
		{`placed_by != nil`, nil},
		// self is a bound variable at both layers, so a bare self reads the
		// owner's members and a parameter named self shadows it
		{`self.name -> Len > 0`, setName("")},
		{`self.LINES -> Len > 0`, nil},
		// a $ variable names a member by its exact spelling
		{`$name -> Len > 0`, setName("")},
		{`$lines -> Len > 0`, nil},
		// a bare lambda variable name resolves like the evaluator's scope does
		{`LINES -> All |$l| { l != nil }`, nil},
		// a parameter shadows a same-named property, and $self may be rebound
		{`LINES -> All |$name| { name.qty > 0 }`, setLineQty(0)},
		{`LINES -> All |$self| { $self.qty > 0 }`, setLineQty(0)},
		// a union of two instance types reads the member both declare
		{`(STAG -> Default(NTAG)).label -> Len > 0`, nil},
		{`STAG.tag -> Len > 0`, nil},
		{`NTAG.tag > 0`, nil},
		{`STAG.tag -> Upper == "T"`, nil},
		{`NTAG.tag -> Abs == 7`, nil},
		// indexing a string yields a string
		{`name[0] == "n"`, setName("south")},
		// Min and Max yield an element without an argument and a scalar with one
		{`LINES -> Map |$l| { $l.qty } -> Max == 7`, setLineQty(9)},
		// the null-guard idiom applies to a present composition as to a property
		{`MAIN_LINE != nil`, nil},
		// a (one) composition is the single child, so Len counts its members
		// — id, qty, tags and ITEM — where a list would count one element
		{`MAIN_LINE -> Len == 4`, dropMainTags},
		{`LINES -> All |$l| { $l.ITEM != nil }`, nil},
		{`(3 -> Max(4)) == 4`, nil},
		// Flatten of a list of instances is that list
		{`(LINES -> Flatten) -> All |$l| { $l.qty > 0 }`, setLineQty(0)},
		// a body that is the nil literal is a body: Then yields nil for a present receiver
		{`(name -> Then |$n| { nil }) == nil`, nil},
		{`(name -> Then |$n| { _ }) == nil`, nil},
		{`(name -> Then |$n| { nil }) == nil && name -> Len > 0`, setName("")},
		// + concatenates lists and strings, as SPEC defines it; pinned through a
		// list-only builtin, which is where the static type is judged
		{`([1] + [2]) -> First == 1`, nil},
		{`(tags + ["x"]) -> Len == 3`, setTags([]any{}...)},
		{`(tags + ["x"]) -> Last == "x"`, nil},
		{`("a" + "b") -> Len == 2`, nil},
		// the receiver kinds, honoured by both layers
		{`name -> Upper == "NORTH"`, setName("south")},
		{`MAIN_LINE.qty -> Abs == 9`, setMainQty(1)},
		{`MAIN_LINE.qty -> Max(1) == 9`, setMainQty(0)},
		{`name -> Compare("a") > 0`, setName("")},
		{`tags -> Join(",") == "alpha,beta"`, setTags("x")},
		{`LINES -> Map |$l| { $l.qty } -> Sum == 12`, setLineQty(0)},
		{`name -> Len == 5`, setName("")},
		// a ternary whose branches disagree in subkind is admitted as unknown
		{`(name != "" ? { name : MAIN_LINE.qty }) -> Upper == "NORTH"`, setName("south")},
		// equality is structural and never errors, on instances as on lists.
		// Two members of one composition slot differ by key, so the same
		// instance is compared with itself through a list literal.
		{`LINES[0] == LINES[0]`, nil},
		{`MAIN_LINE == MAIN_LINE`, nil},
		{`LINES[0] != LINES[1]`, nil},
		{`!(LINES[0] == LINES[1])`, nil},
		{`[LINES[0], LINES[0]] -> Unique -> Len == 1`, nil},
		{`[LINES[0], LINES[1]] -> Unique -> Len == 2`, nil},
		{`LINES -> Contains(LINES[0])`, nil},
		{`!([LINES[0]] -> Contains(LINES[1]))`, nil},
		{`LINES[0] in LINES`, nil},
		{`!(MAIN_LINE in LINES)`, nil},
		{`[1, 1.0, 2] -> Unique -> Len == 2`, nil},
		// an association reads as its target's primary key: a String key is a
		// string, a composite key a list of strings
		{`PLACED_BY + "!" == "c1!"`, setPlacedBy("c9")},
		{`REGION -> Len == 2`, nil},
		{`REGION[0] == "r1"`, nil},
		{`REGION -> Default(["a", "b"]) -> Len == 2`, nil},
		// the nil literal defaults any receiver; an empty list defaults any list
		{`(tags -> Default(nil)) -> Len == 2`, setTags("a")},
		{`(LINES -> Default(nil)) -> Len == 2`, nil},
		{`(MAIN_LINE -> Default(nil)) != nil`, nil},
		{`(LINES -> Default([])) -> Len == 2`, nil},
		{`(CUSTOMERS -> Default([])) -> Len == 2`, nil},
		// the nil-guard family types by one rule, the join of every value it can
		// yield: Coalesce and Lest as Default does, Then as its body
		{`(note -> Coalesce("x")) -> Upper == "X"`, setNote("y")},
		{`(name -> Coalesce(nil, "x")) -> Upper == "NORTH"`, setName("x")},
		{`(extras -> Coalesce([]) -> Len) == 0`, setExtras("a")},
		{`(note -> Lest { "x" }) -> Upper == "X"`, setNote("y")},
		{`(MAIN_LINE -> Then |$l| { $l.qty }) -> Abs > 0`, setMainQty(0)},
		{`(MAIN_LINE -> Then |$l| { $l.ITEM }).sku == "s3"`, setMainField("item", []any{map[string]any{"id": "i", "sku": "x"}})},
		// two instances join to their union, whose members are those every
		// alternative declares — through two shared bases, or declared on each;
		// an absent receiver takes the fallback and the shared member reads
		{`(ALT -> Default(OTHER)).at == "t1"`, setAlt("at")},
		{`(ALT -> Default(OTHER)).label == "L"`, setAlt("label")},
		{`(ALT -> Default(OTHER)).id == "a1"`, setAlt("id")},
		{`(OTHER -> Default(ALT)).at == "t1"`, setAlt("at")},
		{`(OTHER -> Lest { ALT }).label == "L"`, setAlt("label")},
		{`(OTHER -> Coalesce(nil, ALT)).id == "a1"`, setAlt("id")},
		{`(MAIN_LINE -> Default(MAIN_LINE.ITEM)).id == "m1"`, setMainField("id", "x")},
		{`(MAIN_LINE -> Default(MAIN_LINE.ITEM)).id == "m1"`, setMainField("id", "x")},
		// a conditional and a list literal join the same way, and the nil
		// literal is the bottom of the lattice in every position
		{`(f1 ? { MAIN_LINE : MAIN_LINE.ITEM }).id == "m1"`, setF1(false)},
		{`[MAIN_LINE, MAIN_LINE.ITEM] -> All |$x| { $x.id -> Len < 5 }`, setMainField("id", "longer")},
		{`(f1 ? { nil : name }) -> Default("x") -> Upper == "X"`, setF1(false)},
		// a builtin's result is typed by its subkind — a number, a string, a
		// boolean — so the stage after it is judged; Min and Max with an
		// argument yield one of the two
		{`(name -> Len) -> Abs == 5`, setName("x")},
		{`(name -> Upper) -> Len == 5`, setName("x")},
		{`(name -> StartsWith("n")) == true`, setName("x")},
		{`(tags -> Count |$t| { true }) -> Abs == 2`, setTags("a")},
		{`(name -> TypeOf) -> Upper == "STRING"`, dropName},
		{`(MAIN_LINE.qty -> Compare(1)) -> Abs == 1`, setMainQty(1)},
		{`(name -> Substring(1)) -> Upper == "ORTH"`, setName("x")},
		{`(name -> Min("z")) -> Upper == "NORTH"`, setName("zz")},
		{`(name -> Min(1)) == 1`, dropName},
		// Min with an argument is the lower of the two by the total order, and
		// Max is either, so the stage that takes the value loads and holds
		{`(name -> Min(MAIN_LINE.qty)) -> Abs == 9`, setMainQty(1)},
		{`(name -> Max(1)) -> Upper == "NORTH"`, setName("x")},
		// the nil literal passes where the catalogue states no argument kind
		{`(name -> Coalesce(nil)) -> Upper == "NORTH"`, setName("x")},
		{`(name -> Default(nil)) -> Upper == "NORTH"`, setName("x")},
		{`tags -> Contains(nil) == false`, nil},
		{`(tags -> Join(",")) -> Len == 10`, setTags("a")},
		// in with the nil literal on its right is false, not an error
		{`!(1 in nil)`, nil},
		// Compare ranks any two values the total order ranks: a list above a string
		{`REGION -> Compare("a") > 0`, nil},
		{`matrix -> Compare("a") > 0`, nil},
		// Sort, Min and Max rank a list of lists as they rank a list of scalars
		{`(matrix -> Sort -> First -> First) == 1`, nil},
		{`([[2], [1]] -> Sort) == [[1], [2]]`, nil},
		{`(matrix -> Min -> Len) == 2`, nil},
		{`matrix < [[9]]`, nil},
		// an integer stays one through the operators and builtins that keep it,
		// so an index, a remainder and Substring take the result
		{`tags[MAIN_LINE.qty - 9] == "alpha"`, setMainQty(10)},
		{`tags[MAIN_LINE.qty -> Abs -> Min(1)] == "beta"`, setMainQty(0)},
		{`(MAIN_LINE.qty / 2) % 2 == 0`, setMainQty(6)},
		{`(MAIN_LINE.qty -> Floor) % 2 == 1`, setMainQty(6)},
		{`name -> Substring(name -> Len - 2) == "th"`, setName("south-west")},
		{`tags[tags -> Len - 1] == "beta"`, setTags("a", "b", "c")},
		{`tags[name -> Compare("z") + 1] == "alpha"`, setName("zz")},
		{`(LINES -> Map |$l| { $l.qty } -> Sum) % 5 == 2`, setLineQty(6)},
		// a Float and each element of a Vector is a float, whole or not
		{`(vec[2] -> TypeOf) == "float"`, nil},
		{`(vec -> Sum -> TypeOf) == "float"`, nil},
		{`(vec[2] -> Floor -> TypeOf) == "float"`, nil},
		{`(MAIN_LINE.qty -> TypeOf) == "integer"`, nil},
		{`((MAIN_LINE.qty * 1.0) -> TypeOf) == "float"`, nil},
		{`((MAIN_LINE.qty + MAIN_LINE.qty) -> TypeOf) == "integer"`, nil},
		// a number literal carries its sign: the least integer is one literal
		{`MAIN_LINE.qty > -9223372036854775808`, nil},
		{`MAIN_LINE.qty - -1 == 10`, setMainQty(1)},
		{`(-1.5 -> TypeOf) == "float"`, nil},
		{`-(MAIN_LINE.qty) == -9`, setMainQty(1)},
		// a builtin is called by its exact name, and a member of that name is a member
		{`name -> Len == 5 && name -> Upper -> Lower == "north"`, setName("x")},
		// Reduce binds two names, written or numbered
		{`LINES -> Reduce(0) |$acc| { $acc + $1.qty } == 12`, setLineQty(0)},
		{`LINES -> Reduce(0) { $0 + $1.qty } == 12`, setLineQty(0)},
		// a pattern stands on the right of a match and as Match's argument
		{`name =~ /^n/ && name !~ /^s/`, setName("south")},
		{`name =~ /rt/`, setName("south")},
		// the nil literal is ranked, below every value
		{`nil < name`, nil},
		{`(nil -> Compare(name)) == -1`, nil},
		// the nil literal is an empty list to a list builtin, and nothing to Len
		{`(nil -> Sum) == 0 && (nil -> Len) == 0 && (nil -> Sort -> Len) == 0`, nil},
		{`(nil -> Join(",")) == "" && (nil -> Min(name)) == nil && (nil -> Reduce(7) |$a, $b| { $a }) == 7`, nil},
		// two lists of one shape concatenate to a list of that shape, and the
		// stage after the merge is typed by it — instances and nested lists
		// alike, not only scalars
		{`(LINES + LINES) -> First.qty > 0`, setLineQty(0)},
		{`(matrix + [[9]]) -> First -> Sum > 0`, nil},
		// a Vector's element is a number, at both layers
		{`(vec -> Sum) > 0.0`, nil},
		{`vec -> All |$x| { $x > 0.0 }`, nil},
		// a stored nested list flattens
		{`matrix -> Flatten -> Len == 3`, nil},
		{`matrix -> Flatten -> Sum == 6`, nil},
	}
	for _, row := range rows {
		t.Run(row.inv, func(t *testing.T) {
			t.Parallel()
			s, res := schema.LoadString(t.Context(), contractSource(row.inv), "s.yammm")
			if res.Err() != nil {
				t.Fatalf("legal invariant refused at load: %v", res.Err())
			}
			if res := validateOrder(t, s, goodOrder()); res.Err() != nil {
				t.Fatalf("the invariant does not hold on a conforming instance: %v", res.Err())
			}
			if row.bad == nil {
				return
			}
			order := goodOrder()
			row.bad(order)
			res = validateOrder(t, s, order)
			if !res.HasCode(diag.E_INVARIANT_FAIL) {
				t.Errorf("the invariant did not fail on a violating instance (vacuous): %v", res.Err())
			}
			if res.HasCode(diag.E_EVAL_ERROR) {
				t.Errorf("the invariant errored instead of failing: %v", res.Err())
			}
		})
	}
}

// patternIsNoValue is the checker's message for a pattern literal that stands
// where a value does.
const patternIsNoValue = "a pattern literal stands only on the right of =~ or !~ and as the argument of Match"

// evalVerdict is what the evaluator reaches on a shape the checker refuses,
// where its arm cannot error at all. A row that errors states the fragment
// instead and carries evalErrors.
type evalVerdict int

const (
	evalErrors evalVerdict = iota
	evalAnswersFalse
	// evalHoldsVacuously is the third arm the harness has always allowed: the
	// row holds without reading the instance, which an empty instance
	// answering alike shows.
	evalHoldsVacuously
)

func TestInvariantContract_RefuseRows(t *testing.T) {
	t.Parallel()

	// The good instance, validated once against the base schema, is the scope
	// every refuse row is evaluated in.
	base, res := schema.LoadString(t.Context(), contractSource(""), "s.yammm")
	if res.Err() != nil {
		t.Fatal(res.Err())
	}
	v := NewValidator(base)
	good, res := v.ValidateOne(t.Context(), "Order", RawInstance{Properties: goodOrder()})
	if res.Err() != nil {
		t.Fatal(res.Err())
	}
	scope := eval.PropertyScopeOf(v.scopeOf(good))
	empty := map[string]any{}
	emptyScope := eval.PropertyScopeOf(immutable.WrapMap(empty))

	// Every row states what the EVALUATOR does with the shape the checker
	// refuses: a fragment of the error it produces, or — where its arm cannot
	// error — the verdict it reaches instead. A row stating neither asserts
	// nothing about the evaluator, and the harness refuses it.
	rows := []struct {
		inv     string
		code    diag.Code
		want    string // a fragment of the static message
		evalErr string // a fragment of the evaluator's error, when the row errors
		verdict evalVerdict
	}{
		// the target's properties are not readable through an association
		{`CUSTOMERS -> All |$c| { $c.name != "" }`, diag.E_INVALID_INVARIANT, "association", "cannot access member", evalErrors},
		{`PLACED_BY.name != ""`, diag.E_INVALID_INVARIANT, "association", "cannot access member", evalErrors},
		{`CUSTOMERS -> First.name != ""`, diag.E_INVALID_INVARIANT, "association", "cannot access member", evalErrors},
		// unknown members on instances, however the instance was reached
		{`LINES -> All |$l| { $l.qnty > 0 }`, diag.E_UNKNOWN_PROPERTY, "qnty", "", evalAnswersFalse},
		{`LINES -> All { $0.qnty > 0 }`, diag.E_UNKNOWN_PROPERTY, "qnty", "", evalAnswersFalse},
		{`LINES[0].qnty > 0`, diag.E_UNKNOWN_PROPERTY, "qnty", "", evalAnswersFalse},
		{`MAIN_LINE.qnty > 0`, diag.E_UNKNOWN_PROPERTY, "qnty", "", evalAnswersFalse},
		{`LINES -> First.qnty > 0`, diag.E_UNKNOWN_PROPERTY, "qnty", "", evalAnswersFalse},
		{`$self.nonexistent != ""`, diag.E_UNKNOWN_PROPERTY, "nonexistent", "", evalHoldsVacuously},
		{`nonexistent != ""`, diag.E_UNKNOWN_PROPERTY, "nonexistent", "", evalHoldsVacuously},
		// a pipeline stage changes the element type
		{`LINES -> Map |$l| { $l.ITEM } -> All |$x| { $x.qty > 0 }`, diag.E_UNKNOWN_PROPERTY, "qty", "", evalAnswersFalse},
		// a builtin's arguments are checked
		{`name -> Slice(nonexistent, 2) != ""`, diag.E_INVALID_INVARIANT, "Slice", "unknown function", evalErrors},
		{`name -> TrimPrefix(nonexistent) != ""`, diag.E_UNKNOWN_PROPERTY, "nonexistent", "expects string argument", evalErrors},
		// member then pipeline inside a lambda types the member against the element
		{`LINES -> All |$l| { $l.nonexistent -> Len > 0 }`, diag.E_UNKNOWN_PROPERTY, "nonexistent", "", evalAnswersFalse},
		// a scalar or a list has no members; a builtin's name is not a member
		{`name.length > 0`, diag.E_INVALID_INVARIANT, "no members", "cannot access member", evalErrors},
		{`LINES.qty > 0`, diag.E_INVALID_INVARIANT, "list", "cannot access member", evalErrors},
		{`LINES -> All |$l| { $l.Len > 0 }`, diag.E_UNKNOWN_PROPERTY, "Len", "", evalAnswersFalse},
		// an undefined named variable, and a variable that differs only in case
		{`$undefined > 0`, diag.E_INVALID_INVARIANT, "undefined variable", "undefined variable", evalErrors},
		{`tags -> All |$myVar| { $myvar -> Len > 0 }`, diag.E_INVALID_INVARIANT, "undefined variable", "undefined variable", evalErrors},
		// a $ member reference differing from the member's spelling only in case
		{`$nAme -> Len > 0`, diag.E_INVALID_INVARIANT, "undefined variable", "undefined variable", evalErrors},
		{`$lInes -> Len > 0`, diag.E_INVALID_INVARIANT, "undefined variable", "undefined variable", evalErrors},
		// an unknown function, and a call shape the builtin refuses
		{`LINES -> Bogus > 0`, diag.E_INVALID_INVARIANT, "Bogus", "unknown function", evalErrors},
		{`LINES -> Len |$l| { $l.qty } > 0`, diag.E_INVALID_INVARIANT, "lambda", "does not accept a lambda", evalErrors},
		{`LINES -> All > 0`, diag.E_INVALID_INVARIANT, "lambda", "requires a lambda", evalErrors},
		{`name -> Substring(1, 2, 3) != ""`, diag.E_INVALID_INVARIANT, "argument", "at most", evalErrors},
		// the argument rule: a literal the builtin refuses on every input
		{`name -> TrimPrefix(1) != ""`, diag.E_INVALID_INVARIANT, "as its argument", "expects string argument", evalErrors},
		{`name -> TrimSuffix(1) != ""`, diag.E_INVALID_INVARIANT, "as its argument", "expects string argument", evalErrors},
		{`name -> StartsWith(1)`, diag.E_INVALID_INVARIANT, "as its argument", "expects string argument", evalErrors},
		{`name -> EndsWith(true)`, diag.E_INVALID_INVARIANT, "as its argument", "expects string argument", evalErrors},
		{`name -> Split(1) -> Len > 0`, diag.E_INVALID_INVARIANT, "as its argument", "expects string separator", evalErrors},
		{`tags -> Join(1) != ""`, diag.E_INVALID_INVARIANT, "as its argument", "expects string separator", evalErrors},
		{`name -> Replace(1, "b") != ""`, diag.E_INVALID_INVARIANT, "as its argument", "expects string for old value", evalErrors},
		{`name -> Replace("a", 1) != ""`, diag.E_INVALID_INVARIANT, "as its argument", "expects string for new value", evalErrors},
		{`name -> Substring("a") != ""`, diag.E_INVALID_INVARIANT, "as its argument", "expects integer start index", evalErrors},
		{`name -> Substring(1, "b") != ""`, diag.E_INVALID_INVARIANT, "as its argument", "expects integer end index", evalErrors},
		{`name -> Match("nor") -> Len > 0`, diag.E_INVALID_INVARIANT, "as its argument", "expects regexp argument", evalErrors},
		// the nil literal at a position the catalogue types: refused at load, and
		// an evaluation error on every instance when it is not
		{`name -> TrimPrefix(nil) != ""`, diag.E_INVALID_INVARIANT, "as its argument", "expects string argument", evalErrors},
		{`name -> Substring(nil) != ""`, diag.E_INVALID_INVARIANT, "as its argument", "expects integer start index", evalErrors},
		{`name -> Substring(1, nil) != ""`, diag.E_INVALID_INVARIANT, "as its argument", "expects integer end index", evalErrors},
		{`name -> Match(nil) -> Len > 0`, diag.E_INVALID_INVARIANT, "as its argument", "expects regexp argument", evalErrors},
		{`tags -> Join(nil) != ""`, diag.E_INVALID_INVARIANT, "as its argument", "expects string separator", evalErrors},
		{`name -> Compare(MAIN_LINE) > 0`, diag.E_INVALID_INVARIANT, "as its argument", "comparison", evalErrors},
		{`name -> Min(MAIN_LINE) != ""`, diag.E_INVALID_INVARIANT, "as its argument", "comparison", evalErrors},
		{`name -> Max(MAIN_LINE) != ""`, diag.E_INVALID_INVARIANT, "as its argument", "comparison", evalErrors},
		{`note -> Lest |$x| { true }`, diag.E_INVALID_INVARIANT, "lambda parameter", "at most 0 parameters", evalErrors},
		// a list builtin on a scalar, an instance or a key; a scalar builtin on a list
		{`name -> Filter |$c| { true } -> Len > 0`, diag.E_INVALID_INVARIANT, "takes a list", "expects a list", evalErrors},
		{`"abc" -> Contains("b")`, diag.E_INVALID_INVARIANT, "takes a list", "expects a list", evalErrors},
		{`1 -> All |$x| { true }`, diag.E_INVALID_INVARIANT, "takes a list", "expects a list", evalErrors},
		{`PLACED_BY -> Sort -> Len > 0`, diag.E_INVALID_INVARIANT, "takes a list", "expects a list", evalErrors},
		{`LINES -> Sort -> First.qty > 0`, diag.E_INVALID_INVARIANT, "Sort takes a list of values the total order ranks", "unsupported type comparison", evalErrors},
		{`tags -> Upper == "A"`, diag.E_INVALID_INVARIANT, "takes a string", "expects string argument", evalErrors},
		// a number cannot be indexed
		{`LINES[0].qty[0] > 0`, diag.E_INVALID_INVARIANT, "a number cannot be indexed", "cannot index", evalErrors},
		// a receiver the builtin refuses on every input
		{`MAIN_LINE.qty -> Upper != ""`, diag.E_INVALID_INVARIANT, "takes a string", "expects string", evalErrors},
		{`name -> Abs > 0`, diag.E_INVALID_INVARIANT, "takes a number", "expects numeric", evalErrors},
		{`MAIN_LINE.qty -> Len > 0`, diag.E_INVALID_INVARIANT, "takes a string, a list or a map", "unsupported for type", evalErrors},
		{`MAIN_LINE.qty -> Min == 1`, diag.E_INVALID_INVARIANT, "takes a list", "expects a list", evalErrors},
		{`LINES -> Map |$l| { $l.qty } -> Join(",") != ""`, diag.E_INVALID_INVARIANT, "list of strings", "expects all string", evalErrors},
		{`tags -> Sum > 0`, diag.E_INVALID_INVARIANT, "list of numbers", "expects numeric", evalErrors},
		{`name in name`, diag.E_INVALID_INVARIANT, "in takes a list", "expects a list", evalErrors},
		{`(name != "" ? { MAIN_LINE.qty : MAIN_LINE.qty }) -> Upper != ""`, diag.E_INVALID_INVARIANT, "takes a string", "expects string", evalErrors},
		// every refuse row of the static table, judged by the evaluator too
		{`MAIN_LINE -> Max(1) != nil`, diag.E_INVALID_INVARIANT, "cannot be ordered", "unsupported type comparison", evalErrors},
		{`(name == "n")[0] != nil`, diag.E_INVALID_INVARIANT, "a boolean cannot be indexed", "cannot index", evalErrors},
		{`/re/[0] != nil`, diag.E_INVALID_INVARIANT, patternIsNoValue, "cannot index", evalErrors},
		// The receiver is absent in these two, so the fallback is taken and the
		// stage after it is what the evaluator refuses.
		{`(extras -> Default("none") -> First) == nil`, diag.E_INVALID_INVARIANT, "Default", "expects a list", evalErrors},
		{`(note -> Default(1)) -> Upper == "A"`, diag.E_INVALID_INVARIANT, "Default", "expects string argument", evalErrors},
		// Present receiver, so the fallback never fires: the evaluator answers
		// false rather than erroring, which is all this row can assert.
		{`(tags -> Default([1]) -> First) == 1`, diag.E_INVALID_INVARIANT, "Default", "", evalAnswersFalse},
		// the nil-guard family refuses alternatives of disjoint kinds, on an
		// absent receiver so the fallback is what the evaluator refuses
		{`(note -> Coalesce(1)) -> Upper == "A"`, diag.E_INVALID_INVARIANT, "Coalesce", "expects string argument", evalErrors},
		{`(note -> Coalesce(nil, 1)) -> Upper == "A"`, diag.E_INVALID_INVARIANT, "Coalesce", "expects string argument", evalErrors},
		// a disjoint alternative is refused wherever it stands: the same two
		// alternatives, in both orders, with a subkind-less one between them
		{`(note -> Coalesce((f1 ? { note : MAIN_LINE.qty }), 1)) -> Upper == "A"`, diag.E_INVALID_INVARIANT, "Coalesce", "expects string argument", evalErrors},
		{`(note -> Coalesce(1, (f1 ? { note : MAIN_LINE.qty }))) -> Upper == "A"`, diag.E_INVALID_INVARIANT, "Coalesce", "expects string argument", evalErrors},
		{`(note -> Lest { 1 }) -> Upper == "A"`, diag.E_INVALID_INVARIANT, "Lest", "expects string argument", evalErrors},
		{`(extras -> Lest { "x" }) -> First == "x"`, diag.E_INVALID_INVARIANT, "Lest", "expects a list", evalErrors},
		{`(MAIN_LINE -> Then |$l| { $l.qty }) -> Upper != ""`, diag.E_INVALID_INVARIANT, "takes a string", "expects string argument", evalErrors},
		// a string receiver beside a boolean body: with the receiver present the
		// invariant evaluates to the string, which is an evaluation error. Both
		// were accept rows while Lest was untyped.
		{`note -> Lest { true }`, diag.E_INVALID_INVARIANT, "Lest", "", evalHoldsVacuously},
		{`name -> Lest { true }`, diag.E_INVALID_INVARIANT, "Lest", "expected boolean, got string", evalErrors},
		// a member read through a union of instances must be declared on every
		// alternative; these rows read one that one alternative lacks
		{`(MAIN_LINE -> Lest { LINES[0].ITEM }).sku -> Len > 0`, diag.E_UNKNOWN_PROPERTY, "sku", "", evalAnswersFalse},
		{`(MAIN_LINE -> Coalesce(MAIN_LINE.ITEM)).qty == nil`, diag.E_UNKNOWN_PROPERTY, "qty", "", evalAnswersFalse},
		{`(MAIN_LINE -> Default(MAIN_LINE.ITEM)).sku -> Len > 0`, diag.E_UNKNOWN_PROPERTY, "sku", "", evalAnswersFalse},
		{`(ALT -> Default(OTHER)).extra == nil`, diag.E_UNKNOWN_PROPERTY, "extra", "", evalAnswersFalse},
		// a member every alternative declares, at kinds that disagree: the
		// instance selecting the other alternative is what the evaluator refuses
		{`(ABSENT_STAG -> Default(NTAG)).tag -> Upper != ""`, diag.E_INVALID_INVARIANT, "disjoint kinds", "expects string argument", evalErrors},
		{`(OTHER -> Default(ALT)).other -> Len > 0`, diag.E_UNKNOWN_PROPERTY, "other", "", evalAnswersFalse},
		{`(f1 ? { MAIN_LINE : MAIN_LINE.ITEM }).qty == nil`, diag.E_UNKNOWN_PROPERTY, "qty", "", evalAnswersFalse},
		{`(LINES -> Default([MAIN_LINE.ITEM]) -> First).qty == nil`, diag.E_UNKNOWN_PROPERTY, "qty", "", evalAnswersFalse},
		// a non-empty scalar list is not the empty-list wildcard
		{`(LINES -> Default(["a", 1]) -> First).qty == nil`, diag.E_INVALID_INVARIANT, "Default", "", evalAnswersFalse},
		// a builtin's result of one subkind into a builtin that refuses it
		{`name -> Len -> Upper != ""`, diag.E_INVALID_INVARIANT, "takes a string", "expects string argument", evalErrors},
		{`name -> Upper -> Abs > 0`, diag.E_INVALID_INVARIANT, "takes a number", "expects numeric argument", evalErrors},
		{`(name -> StartsWith("n")) -> Abs > 0`, diag.E_INVALID_INVARIANT, "takes a number", "expects numeric argument", evalErrors},
		{`(name -> TypeOf) -> Abs > 0`, diag.E_INVALID_INVARIANT, "takes a number", "expects numeric argument", evalErrors},
		{`(tags -> Contains("a")) -> Upper != ""`, diag.E_INVALID_INVARIANT, "takes a string", "expects string argument", evalErrors},
		{`(MAIN_LINE.qty -> Compare(1)) -> Upper != ""`, diag.E_INVALID_INVARIANT, "takes a string", "expects string argument", evalErrors},
		{`(name -> Min("z")) -> Abs > 0`, diag.E_INVALID_INVARIANT, "takes a number", "expects numeric argument", evalErrors},
		// Min ranks a string above a number, so the mixed pair is the number and
		// a string stage refuses it
		{`(name -> Min(1)) -> Upper != ""`, diag.E_INVALID_INVARIANT, "takes a string", "expects string argument", evalErrors},
		{`(tags -> Join(",")) -> Abs > 0`, diag.E_INVALID_INVARIANT, "takes a number", "expects numeric argument", evalErrors},
		{`(name -> IsNil) -> Len > 0`, diag.E_INVALID_INVARIANT, "takes a string, a list or a map", "unsupported for type bool", evalErrors},
		{`tags -> Max("z") != ""`, diag.E_INVALID_INVARIANT, "argument", "ranks its receiver", evalErrors},
		{`tags -> Min("z") != ""`, diag.E_INVALID_INVARIANT, "argument", "ranks its receiver", evalErrors},
		// a composite key is a list at evaluation time
		{`REGION -> Upper != ""`, diag.E_INVALID_INVARIANT, "takes a string", "expects string", evalErrors},
		{`REGION + "!" != ""`, diag.E_INVALID_INVARIANT, "+ takes", "+ of non-numeric values", evalErrors},
		// a list of lists or of keys into Sum or Join
		{`matrix -> Sum > 0`, diag.E_INVALID_INVARIANT, "list of numbers", "expects numeric", evalErrors},
		{`matrix -> Join(",") != ""`, diag.E_INVALID_INVARIANT, "list of strings", "expects all string", evalErrors},
		{`CUSTOMERS -> Sum > 0`, diag.E_INVALID_INVARIANT, "list of numbers", "expects numeric", evalErrors},
		// a boolean under + and the nil literal under +
		{`(f1 + f2) != nil`, diag.E_INVALID_INVARIANT, "+ takes", "non-numeric", evalErrors},
		{`nil + 1 > 0`, diag.E_INVALID_INVARIANT, "+ takes", "of nil operand", evalErrors},
		{`(name + nil) != ""`, diag.E_INVALID_INVARIANT, "+ takes", "of nil operand", evalErrors},
		// the nil literal under the other arithmetic operators, and unary minus
		{`(-nil) > 0`, diag.E_INVALID_INVARIANT, "unary - takes a number as its operand", "-x of non-numeric value", evalErrors},
		{`nil - 1 > 0`, diag.E_INVALID_INVARIANT, "- takes a number as its left operand", "- of non-numeric values", evalErrors},
		{`nil * 1 > 0`, diag.E_INVALID_INVARIANT, "* takes a number as its left operand", "* of non-numeric values", evalErrors},
		{`nil / 1 > 0`, diag.E_INVALID_INVARIANT, "/ takes a number as its left operand", "/ of non-numeric values", evalErrors},
		{`nil % 1 > 0`, diag.E_INVALID_INVARIANT, "% takes an integer as its left operand", "% requires integer operands", evalErrors},
		// each operator names the operand it refuses, the right one as the left
		{`1 - name > 0`, diag.E_INVALID_INVARIANT, "- takes a number as its right operand", "- of non-numeric values", evalErrors},
		{`MAIN_LINE.qty * f1 > 0`, diag.E_INVALID_INVARIANT, "* takes a number as its right operand", "* of non-numeric values", evalErrors},
		{`tags / 2 > 0`, diag.E_INVALID_INVARIANT, "/ takes a number as its left operand", "/ of non-numeric values", evalErrors},
		{`-name == "x"`, diag.E_INVALID_INVARIANT, "unary - takes a number as its operand", "-x of non-numeric value", evalErrors},
		{`-MAIN_LINE == nil`, diag.E_INVALID_INVARIANT, "unary - takes a number as its operand", "-x of non-numeric value", evalErrors},
		// a remainder, an index and Substring take an integer: a float is
		// refused, whole or not, a property, a literal or a result
		{`vec[0] % 2 == 0`, diag.E_INVALID_INVARIANT, "% takes an integer as its left operand", "% requires integer operands", evalErrors},
		{`MAIN_LINE.qty % 2.0 == 1`, diag.E_INVALID_INVARIANT, "% takes an integer as its right operand", "% requires integer operands", evalErrors},
		{`MAIN_LINE.qty % vec[2] == 0`, diag.E_INVALID_INVARIANT, "% takes an integer as its right operand", "% requires integer operands", evalErrors},
		{`(MAIN_LINE.qty * 1.5) % 2 == 0`, diag.E_INVALID_INVARIANT, "% takes an integer as its left operand", "% requires integer operands", evalErrors},
		{`(MAIN_LINE.qty + vec[0]) % 2 == 0`, diag.E_INVALID_INVARIANT, "% takes an integer as its left operand", "% requires integer operands", evalErrors},
		{`(MAIN_LINE.qty / 2.0) % 2 == 0`, diag.E_INVALID_INVARIANT, "% takes an integer as its left operand", "% requires integer operands", evalErrors},
		{`(vec[2] -> Floor) % 2 == 1`, diag.E_INVALID_INVARIANT, "% takes an integer as its left operand", "% requires integer operands", evalErrors},
		{`(vec[2] -> Abs) % 2 == 1`, diag.E_INVALID_INVARIANT, "% takes an integer as its left operand", "% requires integer operands", evalErrors},
		{`(-(vec[2])) % 2 == 1`, diag.E_INVALID_INVARIANT, "% takes an integer as its left operand", "% requires integer operands", evalErrors},
		{`name % 2 == 0`, diag.E_INVALID_INVARIANT, "% takes an integer as its left operand", "% requires integer operands", evalErrors},
		{`tags[1.0] == "beta"`, diag.E_INVALID_INVARIANT, "indexing takes an integer as its index", "slice index must be integer", evalErrors},
		{`tags[vec[2] - 2.0] == "beta"`, diag.E_INVALID_INVARIANT, "indexing takes an integer as its index", "slice index must be integer", evalErrors},
		{`tags["0"] == "alpha"`, diag.E_INVALID_INVARIANT, "indexing takes an integer as its index", "slice index must be integer", evalErrors},
		{`tags[nil] == "alpha"`, diag.E_INVALID_INVARIANT, "indexing takes an integer as its index", "slice index must be integer", evalErrors},
		{`tags[f2] == "alpha"`, diag.E_INVALID_INVARIANT, "indexing takes an integer as its index", "slice index must be integer", evalErrors},
		{`name -> Substring(1.0) != ""`, diag.E_INVALID_INVARIANT, "Substring takes an integer as its argument", "expects integer start index", evalErrors},
		{`name -> Substring(0, vec[2]) != ""`, diag.E_INVALID_INVARIANT, "Substring takes an integer as its argument", "expects integer end index", evalErrors},
		// the logical operators and a conditional take a boolean
		{`!MAIN_LINE.qty`, diag.E_INVALID_INVARIANT, "! takes a boolean as its operand", "! expects boolean", evalErrors},
		{`!name`, diag.E_INVALID_INVARIANT, "! takes a boolean as its operand", "! expects boolean", evalErrors},
		{`!nil`, diag.E_INVALID_INVARIANT, "! takes a boolean as its operand", "! expects boolean", evalErrors},
		{`!tags`, diag.E_INVALID_INVARIANT, "! takes a boolean as its operand", "! expects boolean", evalErrors},
		{`MAIN_LINE.qty && f1`, diag.E_INVALID_INVARIANT, "&& takes a boolean as its left operand", "expected boolean in &&", evalErrors},
		{`f1 && name`, diag.E_INVALID_INVARIANT, "&& takes a boolean as its right operand", "expected boolean in &&", evalErrors},
		{`nil && f1`, diag.E_INVALID_INVARIANT, "&& takes a boolean as its left operand", "expected boolean in &&", evalErrors},
		{`MAIN_LINE || f1`, diag.E_INVALID_INVARIANT, "|| takes a boolean as its left operand", "expected boolean in ||", evalErrors},
		{`f2 || name`, diag.E_INVALID_INVARIANT, "|| takes a boolean as its right operand", "expected boolean in ||", evalErrors},
		{`f2 || nil`, diag.E_INVALID_INVARIANT, "|| takes a boolean as its right operand", "expected boolean in ||", evalErrors},
		{`name ? { true : false }`, diag.E_INVALID_INVARIANT, "a conditional takes a boolean as its condition", "expected boolean condition", evalErrors},
		{`MAIN_LINE.qty ? { true : false }`, diag.E_INVALID_INVARIANT, "a conditional takes a boolean as its condition", "expected boolean condition", evalErrors},
		{`nil ? { true : false }`, diag.E_INVALID_INVARIANT, "a conditional takes a boolean as its condition", "expected boolean condition", evalErrors},
		// the ordering operators take what the total order ranks: an instance is
		// refused, and a list that holds one
		{`MAIN_LINE < 1`, diag.E_INVALID_INVARIANT, "< takes a value the total order ranks as its left operand", "< comparison error", evalErrors},
		{`1 >= MAIN_LINE`, diag.E_INVALID_INVARIANT, ">= takes a value the total order ranks as its right operand", ">= comparison error", evalErrors},
		{`MAIN_LINE <= MAIN_LINE`, diag.E_INVALID_INVARIANT, "<= takes a value the total order ranks as its left operand", "<= comparison error", evalErrors},
		{`LINES > LINES`, diag.E_INVALID_INVARIANT, "> takes a value the total order ranks as its left operand", "> comparison error", evalErrors},
		{`[[MAIN_LINE]] < [[MAIN_LINE]]`, diag.E_INVALID_INVARIANT, "< takes a value the total order ranks as its left operand", "< comparison error", evalErrors},
		{`[[MAIN_LINE], [MAIN_LINE]] -> Min != nil`, diag.E_INVALID_INVARIANT, "Min takes a list of values the total order ranks", "unsupported type comparison", evalErrors},
		{`[[MAIN_LINE], [MAIN_LINE]] -> Sort != nil`, diag.E_INVALID_INVARIANT, "Sort takes a list of values the total order ranks", "unsupported type comparison", evalErrors},
		{`LINES -> Compare(LINES) == 0`, diag.E_INVALID_INVARIANT, "Compare takes a value the total order ranks, and its receiver is not one", "unsupported type comparison", evalErrors},
		// A list of instances against a value of another kind is ranked by kind,
		// so the evaluator answers where the checker refuses the list's type.
		{`LINES < 1`, diag.E_INVALID_INVARIANT, "< takes a value the total order ranks as its left operand", "", evalAnswersFalse},
		{`LINES -> Compare("a") < 0`, diag.E_INVALID_INVARIANT, "Compare takes a value the total order ranks, and its receiver is not one", "", evalAnswersFalse},
		{`name -> Compare(LINES) > 0`, diag.E_INVALID_INVARIANT, "Compare takes a value the total order ranks as its argument", "", evalAnswersFalse},
		// a match takes a string on its left and a pattern literal on its right
		{`MAIN_LINE.qty =~ /9/`, diag.E_INVALID_INVARIANT, "=~ takes a string as its left operand", "=~ left operand must be string", evalErrors},
		{`nil =~ /a/`, diag.E_INVALID_INVARIANT, "=~ takes a string as its left operand", "=~ left operand must be string", evalErrors},
		{`tags !~ /a/`, diag.E_INVALID_INVARIANT, "!~ takes a string as its left operand", "!~ left operand must be string", evalErrors},
		{`name =~ "nor"`, diag.E_INVALID_INVARIANT, "=~ takes a pattern literal as its right operand", "=~ right operand must be a pattern", evalErrors},
		{`name !~ name`, diag.E_INVALID_INVARIANT, "!~ takes a pattern literal as its right operand", "!~ right operand must be a pattern", evalErrors},
		{`name =~ nil`, diag.E_INVALID_INVARIANT, "=~ takes a pattern literal as its right operand", "=~ right operand must be a pattern", evalErrors},
		{`name =~ (f1 ? { /a/ : /b/ })`, diag.E_INVALID_INVARIANT, "=~ takes a pattern literal as its right operand", "", evalAnswersFalse},
		// a pattern literal is no value: it stands nowhere else
		{`/a/ == "a"`, diag.E_INVALID_INVARIANT, patternIsNoValue, "", evalAnswersFalse},
		{`"a" in [/a/]`, diag.E_INVALID_INVARIANT, patternIsNoValue, "", evalAnswersFalse},
		{`(/x/ -> TypeOf) == "pattern"`, diag.E_INVALID_INVARIANT, patternIsNoValue, "", evalAnswersFalse},
		{`/a/ < "b"`, diag.E_INVALID_INVARIANT, patternIsNoValue, "< comparison error", evalErrors},
		{`[/b/, /a/] -> Sort -> Len == 2`, diag.E_INVALID_INVARIANT, patternIsNoValue, "unsupported type comparison", evalErrors},
		{`(/a/ -> Compare("a")) == 0`, diag.E_INVALID_INVARIANT, patternIsNoValue, "unsupported type comparison", evalErrors},
		{`(note -> Default(/a/)) == "a"`, diag.E_INVALID_INVARIANT, patternIsNoValue, "", evalAnswersFalse},
		{`/a/`, diag.E_INVALID_INVARIANT, patternIsNoValue, "expected boolean, got *regexp.Regexp", evalErrors},
		// an invariant evaluates to a boolean, and a root of another known kind is refused
		{`name`, diag.E_INVALID_INVARIANT, `invariant "m" on type "Order" evaluates to a string, and an invariant evaluates to a boolean`, "expected boolean, got string", evalErrors},
		{`MAIN_LINE.qty + 1`, diag.E_INVALID_INVARIANT, `evaluates to a number, and an invariant evaluates to a boolean`, "expected boolean, got int64", evalErrors},
		{`vec[0]`, diag.E_INVALID_INVARIANT, `evaluates to a number, and an invariant evaluates to a boolean`, "expected boolean, got float64", evalErrors},
		{`tags`, diag.E_INVALID_INVARIANT, `evaluates to a list, and an invariant evaluates to a boolean`, "expected boolean, got immutable.Slice", evalErrors},
		{`MAIN_LINE`, diag.E_INVALID_INVARIANT, `evaluates to an instance, and an invariant evaluates to a boolean`, "expected boolean, got immutable.Map", evalErrors},
		{`name -> Len`, diag.E_INVALID_INVARIANT, `evaluates to a number, and an invariant evaluates to a boolean`, "expected boolean, got int64", evalErrors},
		{`f1 ? { name : name }`, diag.E_INVALID_INVARIANT, `evaluates to a string, and an invariant evaluates to a boolean`, "expected boolean, got string", evalErrors},
		// a builtin is called by its exact name
		{`name -> len > 0`, diag.E_INVALID_INVARIANT, `unknown function "len"`, "unknown function: len", evalErrors},
		{`name -> LEN > 0`, diag.E_INVALID_INVARIANT, `unknown function "LEN"`, "unknown function: LEN", evalErrors},
		{`name -> typeOf == "string"`, diag.E_INVALID_INVARIANT, `unknown function "typeOf"`, "unknown function: typeOf", evalErrors},
		{`tags -> all |$t| { true }`, diag.E_INVALID_INVARIANT, `unknown function "all"`, "unknown function: all", evalErrors},
		// any word after -> is a function's name, a keyword among them. The
		// evaluator reads a call named in or p as that form with a call's parts.
		{`name -> in > 0`, diag.E_INVALID_INVARIANT, `unknown function "in"`, "unknown expression type", evalErrors},
		{`name -> p > 0`, diag.E_INVALID_INVARIANT, `unknown function "p"`, "property lookup requires 1 operand", evalErrors},
		{`name -> nil > 0`, diag.E_INVALID_INVARIANT, `unknown function "nil"`, "unknown function: nil", evalErrors},
		{`name -> String > 0`, diag.E_INVALID_INVARIANT, `unknown function "String"`, "unknown function: String", evalErrors},
		{`name -> type > 0`, diag.E_INVALID_INVARIANT, `unknown function "type"`, "unknown function: type", evalErrors},
		{`name -> primary > 0`, diag.E_INVALID_INVARIANT, `unknown function "primary"`, "unknown function: primary", evalErrors},
		// Reduce binds its accumulator and its element under two names
		{`(LINES -> Reduce(0) |$x, $x| { $x }) == 0`, diag.E_INVALID_INVARIANT, "Reduce binds $x twice", "", evalAnswersFalse},
		{`(LINES -> Reduce(0) |$1| { $1 }) == 0`, diag.E_INVALID_INVARIANT, "Reduce binds $1 twice", "", evalAnswersFalse},
		{`(LINES -> Reduce(0) |$0, $0| { $0 }) == 0`, diag.E_INVALID_INVARIANT, "Reduce binds $0 twice", "", evalAnswersFalse},
		// the nil literal is no receiver of an index or of a builtin that errors on nil
		{`nil[0] != nil`, diag.E_INVALID_INVARIANT, "nil cannot be indexed", "", evalAnswersFalse},
		{`nil -> Upper == ""`, diag.E_INVALID_INVARIANT, "Upper takes a string, and its receiver is not one", "expects string", evalErrors},
		{`nil -> Substring(1) == nil`, diag.E_INVALID_INVARIANT, "Substring takes a string, and its receiver is not one", "expects string", evalErrors},
		{`nil -> Match(/a/) -> Len == 0`, diag.E_INVALID_INVARIANT, "Match takes a string, and its receiver is not one", "expects string", evalErrors},
		{`nil -> Lower == ""`, diag.E_INVALID_INVARIANT, "Lower takes a string, and its receiver is not one", "expects string", evalErrors},
		{`nil -> Trim == ""`, diag.E_INVALID_INVARIANT, "Trim takes a string, and its receiver is not one", "expects string", evalErrors},
		{`nil -> TrimPrefix("a") == ""`, diag.E_INVALID_INVARIANT, "TrimPrefix takes a string, and its receiver is not one", "expects string", evalErrors},
		{`nil -> TrimSuffix("a") == ""`, diag.E_INVALID_INVARIANT, "TrimSuffix takes a string, and its receiver is not one", "expects string", evalErrors},
		{`nil -> Split(",") -> Len == 0`, diag.E_INVALID_INVARIANT, "Split takes a string, and its receiver is not one", "expects string", evalErrors},
		{`nil -> StartsWith("a")`, diag.E_INVALID_INVARIANT, "StartsWith takes a string, and its receiver is not one", "expects string", evalErrors},
		{`nil -> EndsWith("a")`, diag.E_INVALID_INVARIANT, "EndsWith takes a string, and its receiver is not one", "expects string", evalErrors},
		{`nil -> Replace("a", "b") == ""`, diag.E_INVALID_INVARIANT, "Replace takes a string, and its receiver is not one", "expects string", evalErrors},
		{`nil -> Abs == 0`, diag.E_INVALID_INVARIANT, "Abs takes a number, and its receiver is not one", "expects numeric", evalErrors},
		{`nil -> Floor == 0`, diag.E_INVALID_INVARIANT, "Floor takes a number, and its receiver is not one", "expects numeric", evalErrors},
		{`nil -> Ceil == 0`, diag.E_INVALID_INVARIANT, "Ceil takes a number, and its receiver is not one", "expects numeric", evalErrors},
		{`nil -> Round == 0`, diag.E_INVALID_INVARIANT, "Round takes a number, and its receiver is not one", "expects numeric", evalErrors},
		{`nil -> Min == nil`, diag.E_INVALID_INVARIANT, "Min takes a list, and its receiver is not one", "min of empty sequence", evalErrors},
		{`nil -> Max == nil`, diag.E_INVALID_INVARIANT, "Max takes a list, and its receiver is not one", "max of empty sequence", evalErrors},
		{`(nil -> Reduce |$a, $b| { $a }) == nil`, diag.E_INVALID_INVARIANT, "Reduce takes a list, and its receiver is not one", "reduce of empty sequence", evalErrors},
		// a boolean is not a number, and an instance is neither a string nor one
		{`(f1 + MAIN_LINE.qty) != nil`, diag.E_INVALID_INVARIANT, "+ takes", "+ of non-numeric values", evalErrors},
		{`f1 -> Abs > 0`, diag.E_INVALID_INVARIANT, "takes a number", "expects numeric argument", evalErrors},
		{`f1 -> Len > 0`, diag.E_INVALID_INVARIANT, "takes a string, a list or a map", "unsupported for type bool", evalErrors},
		{`MAIN_LINE -> Upper != ""`, diag.E_INVALID_INVARIANT, "takes a string", "expects string argument", evalErrors},
		{`MAIN_LINE -> Abs > 0`, diag.E_INVALID_INVARIANT, "takes a number", "expects numeric argument", evalErrors},
		// a ternary whose branches agree keeps their subkind
		{`(name != "" ? { name : name }) -> Abs > 0`, diag.E_INVALID_INVARIANT, "takes a number", "expects numeric argument", evalErrors},
		// a list of instances is not a list of numbers or of strings
		{`LINES -> Sum > 0`, diag.E_INVALID_INVARIANT, "list of numbers", "expects numeric elements", evalErrors},
		{`LINES -> Join(",") != ""`, diag.E_INVALID_INVARIANT, "list of strings", "expects all string elements", evalErrors},
		// the remaining refuse arms
		{`PLACED_BY -> Default(0) -> Abs > 0`, diag.E_INVALID_INVARIANT, "Default", "expects numeric argument", evalErrors},
		{`MAIN_LINE -> Compare("a") > 0`, diag.E_INVALID_INVARIANT, "total order", "unsupported type comparison", evalErrors},
		{`LINES -> Min != nil`, diag.E_INVALID_INVARIANT, "Min takes a list of values the total order ranks", "unsupported type comparison", evalErrors},
		// an instance cannot be indexed, which the arm that says so must say
		{`MAIN_LINE[0] != nil`, diag.E_INVALID_INVARIANT, `type "Line" cannot be indexed`, "cannot index", evalErrors},
		// a union names its types deduplicated and ordered by identity, so
		// one union has one rendering however the alternatives were written
		{`(STAG -> Default(NTAG)).nope != nil`, diag.E_UNKNOWN_PROPERTY, "(NumberTagged or StringTagged)", "", evalAnswersFalse},
		{`(NTAG -> Default(STAG)).nope != nil`, diag.E_UNKNOWN_PROPERTY, "(NumberTagged or StringTagged)", "", evalAnswersFalse},
		{`(STAG -> Default(STAG)).nope != nil`, diag.E_UNKNOWN_PROPERTY, `"nope" on type "StringTagged" in invariant`, "", evalAnswersFalse},
	}
	for _, row := range rows {
		t.Run(row.inv, func(t *testing.T) {
			t.Parallel()
			src := contractSource(row.inv)
			_, res := schema.LoadString(t.Context(), src, "s.yammm")
			if res.Err() == nil {
				t.Fatal("the checker accepted a shape the evaluator cannot honour")
			}
			if is, ok := issueWithFragment(res, row.code, row.want); !ok {
				t.Errorf("want %s mentioning %q; got %v", row.code, row.want, res.Err())
			} else if is.Span().IsZero() {
				t.Error("the diagnostic carries no span")
			}

			// The evaluator must error, fail, or hold without reading the
			// instance at all, which an empty instance answering alike shows.
			file, issues := parse.Parse([]byte(src), location.NewSourceID("s.yammm"))
			if len(issues) != 0 {
				t.Fatalf("the row does not parse: %v", issues)
			}
			var e expr.Expression
			for _, ty := range file.Types {
				if ty.Name == "Order" {
					e = ty.Invariants[0].Expr
				}
			}
			if row.evalErr == "" && row.verdict == evalErrors {
				t.Fatal("the row states nothing about the evaluator: give it the error fragment, or the verdict its arm reaches instead")
			}
			ok, err := v.evaluator.EvaluateBool(t.Context(), e, scope)
			if err == nil && ok {
				onEmpty, emptyErr := v.evaluator.EvaluateBool(t.Context(), e, emptyScope)
				if emptyErr != nil || !onEmpty {
					t.Fatal("the evaluator honoured, on a conforming instance, a shape the checker refuses")
				}
			}
			if row.evalErr != "" && (err == nil || !strings.Contains(err.Error(), row.evalErr)) {
				t.Errorf("want an evaluation error mentioning %q; got ok=%v err=%v", row.evalErr, ok, err)
			}
			switch row.verdict {
			case evalAnswersFalse:
				if err != nil || ok {
					t.Errorf("want the evaluator to answer false with no error; got ok=%v err=%v", ok, err)
				}
			case evalHoldsVacuously:
				if err != nil || !ok {
					t.Errorf("want the evaluator to hold with no error; got ok=%v err=%v", ok, err)
				}
			case evalErrors:
			}
		})
	}
}

func issueWithFragment(res diag.Result, code diag.Code, fragment string) (diag.Issue, bool) {
	for is := range res.Issues() {
		if is.Code() == code && strings.Contains(is.Message(), fragment) {
			return is, true
		}
	}
	return diag.Issue{}, false
}

// One mistake in one invariant is one diagnostic, however many times the
// expression repeats it.
func TestInvariantContract_OneMistakeOneDiagnostic(t *testing.T) {
	t.Parallel()

	for _, inv := range []string{
		`nonexistent > 0 && nonexistent < 9`,
		`LINES -> Bogus |$l| { $l.qty > 0 }`,
		`LINES -> Len |$l| { $l.qty } > 0`,
	} {
		t.Run(inv, func(t *testing.T) {
			t.Parallel()
			_, res := schema.LoadString(t.Context(), contractSource(inv), "s.yammm")
			n := 0
			for range res.Issues() {
				n++
			}
			if n != 1 {
				t.Errorf("one mistake drew %d diagnostics: %v", n, res.Err())
			}
		})
	}
}

// TestInvariantContract_EveryRetypedResultIsRefusedByItsConsumer pins the
// result SUBKIND the catalogue states for every builtin that declares one.
// Each row pipes the builtin's result into a builtin that refuses that kind
// on every input, so the schema must be refused at load. A result reverted
// to the open ResultUnknown is admitted instead, and its row turns red — the
// whole set stays green only while every subkind in the catalogue is stated.
func TestInvariantContract_EveryRetypedResultIsRefusedByItsConsumer(t *testing.T) {
	t.Parallel()

	// receiverOf yields an expression whose value is that builtin's result.
	receiverOf := map[string]string{
		// number
		"Count":   `LINES -> Count`,
		"Len":     `name -> Len`,
		"Sum":     `LINES -> Map |$l| { $l.qty } -> Sum`,
		"Abs":     `MAIN_LINE.qty -> Abs`,
		"Floor":   `MAIN_LINE.qty -> Floor`,
		"Ceil":    `MAIN_LINE.qty -> Ceil`,
		"Round":   `MAIN_LINE.qty -> Round`,
		"Compare": `name -> Compare("a")`,
		// boolean
		"All":        `LINES -> All |$l| { $l.qty > 0 }`,
		"Any":        `LINES -> Any |$l| { $l.qty > 0 }`,
		"AllOrNone":  `LINES -> AllOrNone |$l| { $l.qty > 0 }`,
		"Contains":   `LINES -> Contains(LINES[0])`,
		"StartsWith": `name -> StartsWith("n")`,
		"EndsWith":   `name -> EndsWith("h")`,
		"IsNil":      `name -> IsNil`,
		// string
		"Upper":      `name -> Upper`,
		"Lower":      `name -> Lower`,
		"Trim":       `name -> Trim`,
		"TrimPrefix": `name -> TrimPrefix("n")`,
		"TrimSuffix": `name -> TrimSuffix("h")`,
		"Join":       `tags -> Join(",")`,
		"Replace":    `name -> Replace("n", "N")`,
		"Substring":  `name -> Substring(1)`,
		"TypeOf":     `name -> TypeOf`,
	}
	// A consumer that refuses the stated kind on every input.
	consumer := map[expr.BuiltinResult]struct{ pipe, want string }{
		expr.ResultNumber:         {`-> Upper != ""`, "takes a string"},
		expr.ResultReceiverNumber: {`-> Upper != ""`, "takes a string"},
		expr.ResultString:         {`-> Abs > 0`, "takes a number"},
		expr.ResultBoolean:        {`-> Abs > 0`, "takes a number"},
	}

	data, err := os.ReadFile("../internal/expr/testdata/builtin_names.txt")
	if err != nil {
		t.Fatal(err)
	}

	var checked int
	for name := range strings.FieldsSeq(string(data)) {
		s, ok := expr.LookupBuiltin(name)
		if !ok {
			t.Fatalf("builtin_names.txt names %s, which the catalogue does not hold", name)
		}
		c, retyped := consumer[s.Result]
		if !retyped {
			continue
		}
		recv, ok := receiverOf[s.Name]
		if !ok {
			t.Errorf("%s declares a stated result subkind and no row produces it", s.Name)
			continue
		}
		checked++
		t.Run(s.Name, func(t *testing.T) {
			t.Parallel()
			inv := "(" + recv + ") " + c.pipe
			_, res := schema.LoadString(t.Context(), contractSource(inv), "s.yammm")
			if res.Err() == nil {
				t.Fatalf("%s's result was piped into a builtin that refuses it and the schema loaded", s.Name)
			}
			if _, ok := issueWithFragment(res, diag.E_INVALID_INVARIANT, c.want); !ok {
				t.Errorf("want E_INVALID_INVARIANT mentioning %q; got %v", c.want, res.Err())
			}
		})
	}
	if checked != len(receiverOf) {
		t.Errorf("checked %d builtins, the table names %d", checked, len(receiverOf))
	}
}
