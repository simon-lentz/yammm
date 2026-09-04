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

type Order {
    id String primary
    name String
    tags List<String>
    note String
    *-> LINES (one:many) Line
    *-> MAIN_LINE (one) Line
    --> PLACED_BY (one) Customer
    --> CUSTOMERS (one:many) Customer
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
		"id": "o1", "name": "north", "tags": []any{"alpha", "beta"},
		"lines":     []any{line("l1", 5, "s1"), line("l2", 7, "s2")},
		"main_line": []any{line("m1", 9, "s3")},
		"placed_by": map[string]any{"_target_id": "c1"},
		"customers": []any{map[string]any{"_target_id": "c1"}, map[string]any{"_target_id": "c2"}},
	}
}

type mutation func(order map[string]any)

func firstLine(o map[string]any) map[string]any { return o["lines"].([]any)[0].(map[string]any) }

func setLineQty(q int64) mutation { return func(o map[string]any) { firstLine(o)["qty"] = q } }
func setMainQty(q int64) mutation {
	return func(o map[string]any) { o["main_line"].([]any)[0].(map[string]any)["qty"] = q }
}

func setItemSku(sku string) mutation {
	return func(o map[string]any) { firstLine(o)["item"].([]any)[0].(map[string]any)["sku"] = sku }
}

func setLineTags(tags ...any) mutation { return func(o map[string]any) { firstLine(o)["tags"] = tags } }
func setName(n string) mutation        { return func(o map[string]any) { o["name"] = n } }
func setTags(tags ...any) mutation     { return func(o map[string]any) { o["tags"] = tags } }
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
		{`note -> Lest { name -> Len > 0 }`, setName("")},
		{`note -> Lest { true }`, nil},
		// associations: keys are answerable for presence, count and comparison
		{`PLACED_BY != nil`, nil},
		{`CUSTOMERS -> Len > 1`, nil},
		{`CUSTOMERS -> All |$c| { $c != nil }`, nil},
		{`PLACED_BY == "c1"`, setPlacedBy("c9")},
		// relation names resolve in either case
		{`lines -> Len > 0`, nil},
		{`placed_by != nil`, nil},
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
		{`LINES -> All |$l| { $l.ITEM != nil }`, nil},
		{`(3 -> Max(4)) == 4`, nil},
		// Flatten of a list of instances is that list
		{`(LINES -> Flatten) -> All |$l| { $l.qty > 0 }`, setLineQty(0)},
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
	root := v.childScope(good)
	scope := eval.PropertyScopeFromMap(root).WithSelf(root)
	empty := map[string]any{}
	emptyScope := eval.PropertyScopeFromMap(empty).WithSelf(empty)

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
		{`note -> Lest |$x| { true }`, diag.E_INVALID_INVARIANT, "lambda parameter", "at most 0 parameters"},
		// a list builtin on a scalar, an instance or a key; a scalar builtin on a list
		{`name -> Filter |$c| { true } -> Len > 0`, diag.E_INVALID_INVARIANT, "takes a list", "slice or array"},
		{`"abc" -> Contains("b")`, diag.E_INVALID_INVARIANT, "takes a list", "slice or array"},
		{`1 -> All |$x| { true }`, diag.E_INVALID_INVARIANT, "takes a list", "slice or array"},
		{`PLACED_BY -> Sort -> Len > 0`, diag.E_INVALID_INVARIANT, "takes a list", "slice or array"},
		{`LINES -> Sort -> First.qty > 0`, diag.E_INVALID_INVARIANT, "list of scalars", ""},
		{`tags -> Upper == "A"`, diag.E_INVALID_INVARIANT, "takes a scalar", ""},
		// the bracket takes exactly one index, and a number cannot be indexed
		{`tags[] -> IsNil`, diag.E_INVALID_INVARIANT, "exactly one index", "slice access requires an index"},
		{`tags[0, 1] -> IsNil`, diag.E_INVALID_INVARIANT, "exactly one index", "slice access accepts exactly one index"},
		{`[10, 20, 30][0, 2] == 10`, diag.E_INVALID_INVARIANT, "exactly one index", "slice access accepts exactly one index"},
		{`LINES[0].qty[0] > 0`, diag.E_INVALID_INVARIANT, "cannot be indexed", "cannot index"},
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
