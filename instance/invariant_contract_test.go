package instance

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance/internal/eval"
	"github.com/simon-lentz/yammm/internal/parse"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/expr"
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

func validateOrder(t *testing.T, s *schema.Schema, order map[string]any) diag.Result {
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
		{`(tags + ["x"]) -> Len == 3`, setTags()},
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
		{`MAIN_LINE -> Default(MAIN_LINE.ITEM) != nil`, nil},
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
		{`(name -> TypeOf) -> Upper == "STRING"`, nil},
		{`(MAIN_LINE.qty -> Compare(1)) -> Abs == 1`, setMainQty(1)},
		{`(name -> Substring(1)) -> Upper == "ORTH"`, setName("x")},
		{`(name -> Min("z")) -> Upper == "NORTH"`, setName("zz")},
		{`(name -> Min(1)) == 1`, nil},
		{`(tags -> Join(",")) -> Len == 10`, setTags("a")},
		// in with the nil literal on its right is false, not an error
		{`!(1 in nil)`, nil},
		// Compare ranks any two values the total order ranks: a list above a string
		{`LINES -> Compare("a") > 0`, nil},
		{`REGION -> Compare("a") > 0`, nil},
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
	emptyScope := eval.PropertyScopeFromMap(empty)

	rows := []struct {
		inv     string
		code    diag.Code
		want    string // a fragment of the static message
		evalErr string // a fragment of the evaluator's error, when the row must error and not merely fail
	}{
		// the target's properties are not readable through an association
		{`CUSTOMERS -> All |$c| { $c.name != "" }`, diag.E_INVALID_INVARIANT, "association", "cannot access member"},
		{`PLACED_BY.name != ""`, diag.E_INVALID_INVARIANT, "association", "cannot access member"},
		{`CUSTOMERS -> First.name != ""`, diag.E_INVALID_INVARIANT, "association", "cannot access member"},
		// unknown members on instances, however the instance was reached
		{`LINES -> All |$l| { $l.qnty > 0 }`, diag.E_UNKNOWN_PROPERTY, "qnty", ""},
		{`LINES -> All { $0.qnty > 0 }`, diag.E_UNKNOWN_PROPERTY, "qnty", ""},
		{`LINES[0].qnty > 0`, diag.E_UNKNOWN_PROPERTY, "qnty", ""},
		{`MAIN_LINE.qnty > 0`, diag.E_UNKNOWN_PROPERTY, "qnty", ""},
		{`LINES -> First.qnty > 0`, diag.E_UNKNOWN_PROPERTY, "qnty", ""},
		{`$self.nonexistent != ""`, diag.E_UNKNOWN_PROPERTY, "nonexistent", ""},
		{`nonexistent != ""`, diag.E_UNKNOWN_PROPERTY, "nonexistent", ""},
		// a pipeline stage changes the element type
		{`LINES -> Map |$l| { $l.ITEM } -> All |$x| { $x.qty > 0 }`, diag.E_UNKNOWN_PROPERTY, "qty", ""},
		// a builtin's arguments are checked
		{`name -> Slice(nonexistent, 2) != ""`, diag.E_INVALID_INVARIANT, "Slice", "unknown function"},
		{`name -> TrimPrefix(nonexistent) != ""`, diag.E_UNKNOWN_PROPERTY, "nonexistent", ""},
		// member then pipeline inside a lambda types the member against the element
		{`LINES -> All |$l| { $l.nonexistent -> Len > 0 }`, diag.E_UNKNOWN_PROPERTY, "nonexistent", ""},
		// a scalar or a list has no members; a builtin's name is not a member
		{`name.length > 0`, diag.E_INVALID_INVARIANT, "no members", "cannot access member"},
		{`LINES.qty > 0`, diag.E_INVALID_INVARIANT, "list", "cannot access member"},
		{`LINES -> All |$l| { $l.Len > 0 }`, diag.E_UNKNOWN_PROPERTY, "Len", ""},
		// an undefined named variable, and a variable that differs only in case
		{`$undefined > 0`, diag.E_INVALID_INVARIANT, "undefined variable", "undefined variable"},
		{`tags -> All |$myVar| { $myvar -> Len > 0 }`, diag.E_INVALID_INVARIANT, "undefined variable", "undefined variable"},
		// a $ member reference differing from the member's spelling only in case
		{`$nAme -> Len > 0`, diag.E_INVALID_INVARIANT, "undefined variable", "undefined variable"},
		{`$lInes -> Len > 0`, diag.E_INVALID_INVARIANT, "undefined variable", "undefined variable"},
		// an unknown function, and a call shape the builtin refuses
		{`LINES -> Bogus > 0`, diag.E_INVALID_INVARIANT, "Bogus", "unknown function"},
		{`LINES -> Len |$l| { $l.qty } > 0`, diag.E_INVALID_INVARIANT, "lambda", "does not accept a lambda"},
		{`LINES -> All > 0`, diag.E_INVALID_INVARIANT, "lambda", "requires a lambda"},
		{`name -> Substring(1, 2, 3) != ""`, diag.E_INVALID_INVARIANT, "argument", "at most"},
		// the argument rule: a literal the builtin refuses on every input
		{`name -> TrimPrefix(1) != ""`, diag.E_INVALID_INVARIANT, "as its argument", "expects string argument"},
		{`name -> TrimSuffix(1) != ""`, diag.E_INVALID_INVARIANT, "as its argument", "expects string argument"},
		{`name -> StartsWith(1)`, diag.E_INVALID_INVARIANT, "as its argument", "expects string argument"},
		{`name -> EndsWith(true)`, diag.E_INVALID_INVARIANT, "as its argument", "expects string argument"},
		{`name -> Split(1) -> Len > 0`, diag.E_INVALID_INVARIANT, "as its argument", "expects string separator"},
		{`tags -> Join(1) != ""`, diag.E_INVALID_INVARIANT, "as its argument", "expects string separator"},
		{`name -> Replace(1, "b") != ""`, diag.E_INVALID_INVARIANT, "as its argument", "expects string for old value"},
		{`name -> Replace("a", 1) != ""`, diag.E_INVALID_INVARIANT, "as its argument", "expects string for new value"},
		{`name -> Substring("a") != ""`, diag.E_INVALID_INVARIANT, "as its argument", "expects integer start index"},
		{`name -> Substring(1, "b") != ""`, diag.E_INVALID_INVARIANT, "as its argument", "expects integer end index"},
		{`name -> Match("nor") -> Len > 0`, diag.E_INVALID_INVARIANT, "as its argument", "expects regexp argument"},
		{`name -> Compare(MAIN_LINE) > 0`, diag.E_INVALID_INVARIANT, "as its argument", "comparison"},
		{`name -> Min(MAIN_LINE) != ""`, diag.E_INVALID_INVARIANT, "as its argument", "comparison"},
		{`name -> Max(MAIN_LINE) != ""`, diag.E_INVALID_INVARIANT, "as its argument", "comparison"},
		{`note -> Lest |$x| { true }`, diag.E_INVALID_INVARIANT, "lambda parameter", "at most 0 parameters"},
		// a list builtin on a scalar, an instance or a key; a scalar builtin on a list
		{`name -> Filter |$c| { true } -> Len > 0`, diag.E_INVALID_INVARIANT, "takes a list", "slice or array"},
		{`"abc" -> Contains("b")`, diag.E_INVALID_INVARIANT, "takes a list", "slice or array"},
		{`1 -> All |$x| { true }`, diag.E_INVALID_INVARIANT, "takes a list", "slice or array"},
		{`PLACED_BY -> Sort -> Len > 0`, diag.E_INVALID_INVARIANT, "takes a list", "slice or array"},
		{`LINES -> Sort -> First.qty > 0`, diag.E_INVALID_INVARIANT, "list of scalars", ""},
		{`tags -> Upper == "A"`, diag.E_INVALID_INVARIANT, "takes a string", ""},
		// the bracket takes exactly one index, and a number cannot be indexed
		{`tags[] -> IsNil`, diag.E_INVALID_INVARIANT, "exactly one index", "slice access requires an index"},
		{`tags[0, 1] -> IsNil`, diag.E_INVALID_INVARIANT, "exactly one index", "slice access accepts exactly one index"},
		{`[10, 20, 30][0, 2] == 10`, diag.E_INVALID_INVARIANT, "exactly one index", "slice access accepts exactly one index"},
		{`LINES[0].qty[0] > 0`, diag.E_INVALID_INVARIANT, "cannot be indexed", "cannot index"},
		// a receiver the builtin refuses on every input
		{`MAIN_LINE.qty -> Upper != ""`, diag.E_INVALID_INVARIANT, "takes a string", "expects string"},
		{`name -> Abs > 0`, diag.E_INVALID_INVARIANT, "takes a number", "expects numeric"},
		{`MAIN_LINE.qty -> Len > 0`, diag.E_INVALID_INVARIANT, "takes a string, a list or a map", "unsupported for type"},
		{`MAIN_LINE.qty -> Min == 1`, diag.E_INVALID_INVARIANT, "takes a list", "slice or array"},
		{`LINES -> Map |$l| { $l.qty } -> Join(",") != ""`, diag.E_INVALID_INVARIANT, "list of strings", "expects all string"},
		{`tags -> Sum > 0`, diag.E_INVALID_INVARIANT, "list of numbers", "expects numeric"},
		{`name in name`, diag.E_INVALID_INVARIANT, "in takes a list", "slice or array"},
		{`(name != "" ? { MAIN_LINE.qty : MAIN_LINE.qty }) -> Upper != ""`, diag.E_INVALID_INVARIANT, "takes a string", "expects string"},
		// every refuse row of the static table, judged by the evaluator too
		{`MAIN_LINE -> Max(1) != nil`, diag.E_INVALID_INVARIANT, "cannot be ordered", "unsupported type comparison"},
		{`(name == "n")[0] != nil`, diag.E_INVALID_INVARIANT, "cannot be indexed", "cannot index"},
		// The receiver is absent in these two, so the fallback is taken and the
		// stage after it is what the evaluator refuses.
		{`(extras -> Default("none") -> First) == nil`, diag.E_INVALID_INVARIANT, "Default", "expects slice or array input"},
		{`(note -> Default(1)) -> Upper == "A"`, diag.E_INVALID_INVARIANT, "Default", "expects string argument"},
		// Present receiver, so the fallback never fires: the evaluator answers
		// false rather than erroring, which is all this row can assert.
		{`(tags -> Default([1]) -> First) == 1`, diag.E_INVALID_INVARIANT, "Default", ""},
		// the nil-guard family refuses alternatives of disjoint kinds, on an
		// absent receiver so the fallback is what the evaluator refuses
		{`(note -> Coalesce(1)) -> Upper == "A"`, diag.E_INVALID_INVARIANT, "Coalesce", "expects string argument"},
		{`(note -> Coalesce(nil, 1)) -> Upper == "A"`, diag.E_INVALID_INVARIANT, "Coalesce", "expects string argument"},
		{`(note -> Lest { 1 }) -> Upper == "A"`, diag.E_INVALID_INVARIANT, "Lest", "expects string argument"},
		{`(extras -> Lest { "x" }) -> First == "x"`, diag.E_INVALID_INVARIANT, "Lest", "expects slice or array input"},
		{`(MAIN_LINE -> Then |$l| { $l.qty }) -> Upper != ""`, diag.E_INVALID_INVARIANT, "takes a string", "expects string argument"},
		// a string receiver beside a boolean body: with the receiver present the
		// invariant evaluates to the string, which is an evaluation error. Both
		// were accept rows while Lest was untyped.
		{`note -> Lest { true }`, diag.E_INVALID_INVARIANT, "Lest", ""},
		{`name -> Lest { true }`, diag.E_INVALID_INVARIANT, "Lest", "expected boolean, got string"},
		// a member read through a union of instances must be declared on every
		// alternative; these rows read one that one alternative lacks
		{`(MAIN_LINE -> Lest { LINES[0].ITEM }).sku -> Len > 0`, diag.E_UNKNOWN_PROPERTY, "sku", ""},
		{`(MAIN_LINE -> Coalesce(MAIN_LINE.ITEM)).qty == nil`, diag.E_UNKNOWN_PROPERTY, "qty", ""},
		{`(MAIN_LINE -> Default(MAIN_LINE.ITEM)).sku -> Len > 0`, diag.E_UNKNOWN_PROPERTY, "sku", ""},
		{`(ALT -> Default(OTHER)).extra == nil`, diag.E_UNKNOWN_PROPERTY, "extra", ""},
		{`(OTHER -> Default(ALT)).other -> Len > 0`, diag.E_UNKNOWN_PROPERTY, "other", ""},
		{`(f1 ? { MAIN_LINE : MAIN_LINE.ITEM }).qty == nil`, diag.E_UNKNOWN_PROPERTY, "qty", ""},
		{`(LINES -> Default([MAIN_LINE.ITEM]) -> First).qty == nil`, diag.E_UNKNOWN_PROPERTY, "qty", ""},
		// a non-empty scalar list is not the empty-list wildcard
		{`(LINES -> Default(["a", 1]) -> First).qty == nil`, diag.E_INVALID_INVARIANT, "Default", ""},
		// a builtin's result of one subkind into a builtin that refuses it
		{`name -> Len -> Upper != ""`, diag.E_INVALID_INVARIANT, "takes a string", "expects string argument"},
		{`name -> Upper -> Abs > 0`, diag.E_INVALID_INVARIANT, "takes a number", "expects numeric argument"},
		{`(name -> StartsWith("n")) -> Abs > 0`, diag.E_INVALID_INVARIANT, "takes a number", "expects numeric argument"},
		{`(name -> TypeOf) -> Abs > 0`, diag.E_INVALID_INVARIANT, "takes a number", "expects numeric argument"},
		{`(tags -> Contains("a")) -> Upper != ""`, diag.E_INVALID_INVARIANT, "takes a string", "expects string argument"},
		{`(MAIN_LINE.qty -> Compare(1)) -> Upper != ""`, diag.E_INVALID_INVARIANT, "takes a string", "expects string argument"},
		{`(name -> Min("z")) -> Abs > 0`, diag.E_INVALID_INVARIANT, "takes a number", "expects numeric argument"},
		{`(tags -> Join(",")) -> Abs > 0`, diag.E_INVALID_INVARIANT, "takes a number", "expects numeric argument"},
		{`(name -> IsNil) -> Len > 0`, diag.E_INVALID_INVARIANT, "takes a string, a list or a map", "unsupported for type bool"},
		{`tags -> Max("z") != ""`, diag.E_INVALID_INVARIANT, "argument", "ranks its receiver"},
		{`tags -> Min("z") != ""`, diag.E_INVALID_INVARIANT, "argument", "ranks its receiver"},
		{`name =~ Vector`, diag.E_INVALID_INVARIANT, "Vector", "unknown datatype"},
		{`name =~ List`, diag.E_INVALID_INVARIANT, "List", "unknown datatype"},
		{`name =~ Enum`, diag.E_INVALID_INVARIANT, "Enum", "unknown datatype"},
		{`name !~ Pattern`, diag.E_INVALID_INVARIANT, "Pattern", "unknown datatype"},
		// a composite key is a list at evaluation time
		{`REGION -> Upper != ""`, diag.E_INVALID_INVARIANT, "takes a string", "expects string"},
		{`REGION + "!" != ""`, diag.E_INVALID_INVARIANT, "+ takes", "+ of non-numeric values"},
		// a list of lists or of keys into Sum or Join
		{`matrix -> Sum > 0`, diag.E_INVALID_INVARIANT, "list of numbers", "expects numeric"},
		{`matrix -> Join(",") != ""`, diag.E_INVALID_INVARIANT, "list of strings", "expects all string"},
		{`CUSTOMERS -> Sum > 0`, diag.E_INVALID_INVARIANT, "list of numbers", "expects numeric"},
		// a boolean under + and the nil literal under +
		{`(f1 + f2) != nil`, diag.E_INVALID_INVARIANT, "+ takes", "non-numeric"},
		{`nil + 1 > 0`, diag.E_INVALID_INVARIANT, "+ takes", "of nil operand"},
		{`(name + nil) != ""`, diag.E_INVALID_INVARIANT, "+ takes", "of nil operand"},
		// the nil literal under the other arithmetic operators, and unary minus
		{`(-nil) > 0`, diag.E_INVALID_INVARIANT, "unary - takes a number", "-x of non-numeric value"},
		{`nil - 1 > 0`, diag.E_INVALID_INVARIANT, "- takes two numbers", "- of non-numeric values"},
		{`nil * 1 > 0`, diag.E_INVALID_INVARIANT, "* takes two numbers", "* of non-numeric values"},
		{`nil / 1 > 0`, diag.E_INVALID_INVARIANT, "/ takes two numbers", "/ of non-numeric values"},
		{`nil % 1 > 0`, diag.E_INVALID_INVARIANT, "% takes two numbers", "% requires integer operands"},
		// a boolean is not a number, and an instance is neither a string nor one
		{`(f1 + MAIN_LINE.qty) != nil`, diag.E_INVALID_INVARIANT, "+ takes", "+ of non-numeric values"},
		{`f1 -> Abs > 0`, diag.E_INVALID_INVARIANT, "takes a number", "expects numeric argument"},
		{`f1 -> Len > 0`, diag.E_INVALID_INVARIANT, "takes a string, a list or a map", "unsupported for type bool"},
		{`MAIN_LINE -> Upper != ""`, diag.E_INVALID_INVARIANT, "takes a string", "expects string argument"},
		{`MAIN_LINE -> Abs > 0`, diag.E_INVALID_INVARIANT, "takes a number", "expects numeric argument"},
		// a ternary whose branches agree keeps their subkind
		{`(name != "" ? { name : name }) -> Abs > 0`, diag.E_INVALID_INVARIANT, "takes a number", "expects numeric argument"},
		// a list of instances is not a list of numbers or of strings
		{`LINES -> Sum > 0`, diag.E_INVALID_INVARIANT, "list of numbers", "expects numeric elements"},
		{`LINES -> Join(",") != ""`, diag.E_INVALID_INVARIANT, "list of strings", "expects all string elements"},
		// the remaining refuse arms
		{`PLACED_BY -> Default(0) -> Abs > 0`, diag.E_INVALID_INVARIANT, "Default", "expects numeric argument"},
		{`MAIN_LINE -> Compare("a") > 0`, diag.E_INVALID_INVARIANT, "total order", "unsupported type comparison"},
		{`LINES -> Min != nil`, diag.E_INVALID_INVARIANT, "list of scalars", "unsupported type comparison"},
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
			ok, err := v.evaluator.EvaluateBool(e, scope)
			if err == nil && ok {
				onEmpty, emptyErr := v.evaluator.EvaluateBool(e, emptyScope)
				if emptyErr != nil || !onEmpty {
					t.Fatal("the evaluator honoured, on a conforming instance, a shape the checker refuses")
				}
			}
			if row.evalErr != "" && (err == nil || !strings.Contains(err.Error(), row.evalErr)) {
				t.Errorf("want an evaluation error mentioning %q; got ok=%v err=%v", row.evalErr, ok, err)
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
