package schema_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// The static invariant checker types every expression — an instance, an
// association key, a list, a scalar, or unknown — and refuses what the
// evaluator would refuse. This table pins the checker alone; the instance
// package's contract table judges the same shapes by both layers.
const staticBase = `schema "s"

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

func loadInvariant(t *testing.T, inv string) diag.Result {
	t.Helper()
	src := staticBase + "    ! \"m\" " + inv + "\n}\n"
	_, res := schema.LoadString(t.Context(), src, "s.yammm")
	return res
}

func TestStaticInvariant_Table(t *testing.T) {
	t.Parallel()

	accept := []string{
		// the argument rule admits the right kinds, a property among them
		`name -> TrimPrefix("n") != ""`,
		`name -> Substring(1) != ""`,
		`name -> Substring(1, 3) != ""`,
		`name -> Match(/n.*/) -> Len > 0`,
		`name -> Replace("n", "N") != ""`,
		`name -> Split(name) -> Len > 0`,
		`name -> Compare("m") > 0`,
		`name -> Min("z") != ""`,
		`tags -> Contains(MAIN_LINE) == false`,
		`name -> Default(MAIN_LINE.id) != ""`,
		// compositions: children are instances
		`LINES -> All |$l| { $l.qty > 0 }`,
		`LINES -> All { $0.qty > 0 }`,
		`LINES[0].qty > 0`,
		`MAIN_LINE.qty > 0`,
		`LINES -> First.qty > 0`,
		`LINES -> Filter |$l| { $l.qty > 0 } -> All |$l| { $l.id != "" }`,
		`LINES -> Map |$l| { $l.ITEM } -> All |$x| { $x.sku != "" }`,
		`LINES -> Map |$l| { $l.qty } -> Sum > 0`,
		`LINES -> Reduce(0) |$acc, $l| { $acc + $l.qty } > 0`,
		`LINES -> All |$l| { $l.tags[0] != "" }`,
		`LINES -> All |$l| { $l.tags -> Len > 0 }`,
		`LINES -> Map |$l| { $l.qty } -> Sort -> First > 0`,
		// nested compositions: a child is an instance with its own relations
		`LINES -> All |$l| { $l.ITEM.sku != "" }`,
		// a parameter shadows a same-named property, and $self may be rebound
		`LINES -> All |$name| { name.qty > 0 }`,
		`LINES -> All |$self| { $self.qty > 0 }`,
		// Lest binds nothing; its body reads the caller's scope
		`(note -> Lest { name }) != ""`,
		// self is a bound variable, so a bare self reads the owner's members
		`self.name -> Len > 0`,
		// member then pipeline, member then index
		`$self.name -> Len > 0`,
		`$self.tags[0] != ""`,
		`name -> Then |$n| { $n -> Len > 0 }`,
		`MAIN_LINE -> Then |$l| { $l.qty > 0 }`,
		// associations: keys are answerable for presence, count and comparison
		`PLACED_BY != nil`,
		`CUSTOMERS -> Len > 0`,
		`CUSTOMERS -> All |$c| { $c != nil }`,
		`PLACED_BY == "c1"`,
		// relation names resolve in either case
		`lines -> Len > 0`,
		`placed_by != nil`,
		// a bare lambda variable name resolves like the evaluator's scope does
		`LINES -> All |$l| { l != nil }`,
		// a body that is the nil literal is a body, not an absent one
		`(name -> Then |$n| { nil }) == nil`,
		`(name -> Then |$n| { _ }) == nil`,
		// + concatenates lists and strings, as SPEC defines it
		`([1] + [2]) -> First == 1`,
		`(tags + ["x"]) -> Len == 3`,
		`("a" + "b") -> Len == 2`,
		// the receiver kinds: a string builtin on a string, a numeric one on a number
		`name -> Upper == "N"`,
		`MAIN_LINE.qty -> Abs > 0`,
		`MAIN_LINE.qty -> Max(1) >= 1`,
		`name -> Compare("a") >= 0`,
		`["a", "b"] -> Join(",") == "a,b"`,
		`[1, 2] -> Sum == 3`,
		`tags -> Len > 0`,
		`name -> Len > 0`,
		// a ternary whose branches disagree in subkind is a scalar of unknown kind
		`(name != "" ? { name : MAIN_LINE.qty }) -> Upper != ""`,
		// equality on instances is structural, and a boolean result is a number-or-boolean scalar
		`LINES[0] != LINES[1]`,
		`[LINES[0], LINES[0]] -> Unique -> Len == 1`,
		`LINES -> Contains(LINES[0])`,
		`LINES[0] in LINES`,
		// a Vector's element is a number, as a List<Float>'s is
		`(vec -> Sum) > 0.0`,
		`vec -> All |$x| { $x > 0.0 }`,
		// a nested list's element is a list, and its element a number
		`matrix -> All |$r| { $r -> Sum > 0 }`,
		`matrix[0][0] > 0`,
		// Default's fallback is of the receiver's kind, so the stage after it is typed
		`(tags -> Default(["x"]) -> First) == "x"`,
		`(tags -> Default([]) -> Len) == 0`,
		`(name -> Default("n")) -> Upper == "N"`,
		`MAIN_LINE.qty -> Default(0) > -1`,
		`(PLACED_BY -> Default("c1")) == "c1"`,
		// a datatype check names a kind a value can have
		`name =~ String`,
		`MAIN_LINE.qty =~ Integer`,
		`name !~ Timestamp`,
		// an association reads as its target's primary key: a String key is a
		// string, a composite key a list of strings
		`PLACED_BY + "!" == "c1!"`,
		`REGION -> Len == 2`,
		`REGION -> Default(["a", "b"]) -> Len == 2`,
		`REGION[0] != ""`,
		// the nil literal is a wildcard under Default, whatever the receiver
		`(tags -> Default(nil)) -> Len == 2`,
		`(LINES -> Default(nil)) -> Len > 0`,
		`(MAIN_LINE -> Default(nil)) != nil`,
		// an empty list literal is a list of anything, so it defaults any list
		`(LINES -> Default([])) -> Len > 0`,
		`(CUSTOMERS -> Default([])) -> Len > 0`,
		// in with the nil literal on its right is false, not an error
		`!(1 in nil)`,
		// Compare ranks any two values the total order ranks: a list above a string
		`LINES -> Compare("a") > 0`,
		`REGION -> Compare("a") > 0`,
		// the nil-guard family types by one rule, the join of every value it can
		// yield: Coalesce and Lest as Default does, Then as its body
		`(note -> Coalesce("x")) -> Upper == "X"`,
		`(name -> Coalesce(nil, "x")) -> Upper == "X"`,
		`(extras -> Coalesce([]) -> Len) == 0`,
		`(note -> Lest { "x" }) -> Upper == "X"`,
		`(MAIN_LINE -> Then |$l| { $l.qty }) -> Abs > 0`,
		`(MAIN_LINE -> Then |$l| { $l.ITEM }).sku != ""`,
		// two instances join to their union, whose members are those every
		// alternative declares — through two shared bases, or declared on each
		`(ALT -> Default(OTHER)).at != ""`,
		`(ALT -> Default(OTHER)).label != ""`,
		`(ALT -> Default(OTHER)).id != ""`,
		`(OTHER -> Default(ALT)).at != ""`,
		`(OTHER -> Lest { ALT }).label != ""`,
		`(OTHER -> Coalesce(nil, ALT)).id != ""`,
		`MAIN_LINE -> Default(MAIN_LINE.ITEM) != nil`,
		`(MAIN_LINE -> Default(MAIN_LINE.ITEM)).id != ""`,
		// a conditional and a list literal join the same way, and the nil
		// literal is the bottom of the lattice in every position
		`(f1 ? { MAIN_LINE : MAIN_LINE.ITEM }).id != ""`,
		`[MAIN_LINE, MAIN_LINE.ITEM] -> All |$x| { $x.id -> Len < 5 }`,
		`(f1 ? { nil : name }) -> Default("x") -> Upper == "X"`,
		// a builtin's result is typed by its subkind — a number, a string, a
		// boolean — so the stage after it is judged; Min and Max with an
		// argument yield one of the two
		`(name -> Len) -> Abs == 5`,
		`(name -> Upper) -> Len == 5`,
		`(name -> StartsWith("n")) == true`,
		`(tags -> Count |$t| { true }) -> Abs == 2`,
		`(name -> TypeOf) -> Upper == "STRING"`,
		`(MAIN_LINE.qty -> Compare(1)) -> Abs == 1`,
		`(name -> Substring(1)) -> Upper == "ORTH"`,
		`(name -> Min("z")) -> Upper == "NORTH"`,
		`(name -> Min(1)) == 1`,
		`(tags -> Join(",")) -> Len > 0`,
	}
	for _, inv := range accept {
		t.Run("accepts "+inv, func(t *testing.T) {
			t.Parallel()
			if res := loadInvariant(t, inv); res.Err() != nil {
				t.Errorf("legal invariant refused: %v", res.Err())
			}
		})
	}

	refuse := []struct {
		inv  string
		code diag.Code
		want string // a fragment of the message
	}{
		// the target's properties are not readable through an association
		{`CUSTOMERS -> All |$c| { $c.name != "" }`, diag.E_INVALID_INVARIANT, "association"},
		{`PLACED_BY.name != ""`, diag.E_INVALID_INVARIANT, "association"},
		{`CUSTOMERS -> First.name != ""`, diag.E_INVALID_INVARIANT, "association"},
		// unknown members on instances, however the instance was reached
		{`LINES -> All |$l| { $l.qnty > 0 }`, diag.E_UNKNOWN_PROPERTY, "qnty"},
		{`LINES -> All { $0.qnty > 0 }`, diag.E_UNKNOWN_PROPERTY, "qnty"},
		{`LINES[0].qnty > 0`, diag.E_UNKNOWN_PROPERTY, "qnty"},
		{`MAIN_LINE.qnty > 0`, diag.E_UNKNOWN_PROPERTY, "qnty"},
		{`LINES -> First.qnty > 0`, diag.E_UNKNOWN_PROPERTY, "qnty"},
		{`$self.nonexistent != ""`, diag.E_UNKNOWN_PROPERTY, "nonexistent"},
		{`nonexistent != ""`, diag.E_UNKNOWN_PROPERTY, "nonexistent"},
		// a pipeline stage changes the element type
		{`LINES -> Map |$l| { $l.ITEM } -> All |$x| { $x.qty > 0 }`, diag.E_UNKNOWN_PROPERTY, "qty"},
		// a builtin's arguments are checked
		{`name -> Slice(nonexistent, 2) != ""`, diag.E_INVALID_INVARIANT, "Slice"},
		{`name -> TrimPrefix(nonexistent) != ""`, diag.E_UNKNOWN_PROPERTY, "nonexistent"},
		// member then pipeline inside a lambda types the member against the element
		{`LINES -> All |$l| { $l.nonexistent -> Len > 0 }`, diag.E_UNKNOWN_PROPERTY, "nonexistent"},
		// a scalar or a list has no members; a builtin's name is not a member
		{`name.length > 0`, diag.E_INVALID_INVARIANT, "no members"},
		{`LINES.qty > 0`, diag.E_INVALID_INVARIANT, "list"},
		{`LINES -> All |$l| { $l.Len > 0 }`, diag.E_UNKNOWN_PROPERTY, "Len"},
		// an undefined named variable is a guaranteed evaluation error, and a
		// variable name never folds
		{`$undefined > 0`, diag.E_INVALID_INVARIANT, "undefined variable"},
		{`tags -> All |$myVar| { $myvar -> Len > 0 }`, diag.E_INVALID_INVARIANT, "undefined variable"},
		// an unknown function and a call shape the builtin refuses
		{`LINES -> Bogus > 0`, diag.E_INVALID_INVARIANT, "Bogus"},
		{`LINES -> Len |$l| { $l.qty } > 0`, diag.E_INVALID_INVARIANT, "lambda"},
		{`LINES -> All > 0`, diag.E_INVALID_INVARIANT, "lambda"},
		{`name -> Substring(1, 2, 3) != ""`, diag.E_INVALID_INVARIANT, "argument"},
		// the argument rule: a literal the builtin refuses on every input
		{`name -> TrimPrefix(1) != ""`, diag.E_INVALID_INVARIANT, "as its argument"},
		{`name -> TrimSuffix(1) != ""`, diag.E_INVALID_INVARIANT, "as its argument"},
		{`name -> StartsWith(1)`, diag.E_INVALID_INVARIANT, "as its argument"},
		{`name -> EndsWith(true)`, diag.E_INVALID_INVARIANT, "as its argument"},
		{`name -> Split(1) -> Len > 0`, diag.E_INVALID_INVARIANT, "as its argument"},
		{`tags -> Join(1) != ""`, diag.E_INVALID_INVARIANT, "as its argument"},
		{`name -> Replace(1, "b") != ""`, diag.E_INVALID_INVARIANT, "as its argument"},
		{`name -> Replace("a", 1) != ""`, diag.E_INVALID_INVARIANT, "as its argument"},
		{`name -> Substring("a") != ""`, diag.E_INVALID_INVARIANT, "as its argument"},
		{`name -> Substring(1, "b") != ""`, diag.E_INVALID_INVARIANT, "as its argument"},
		{`name -> Match("nor") -> Len > 0`, diag.E_INVALID_INVARIANT, "as its argument"},
		{`name -> Compare(MAIN_LINE) > 0`, diag.E_INVALID_INVARIANT, "as its argument"},
		{`name -> Min(MAIN_LINE) != ""`, diag.E_INVALID_INVARIANT, "as its argument"},
		{`name -> Max(MAIN_LINE) != ""`, diag.E_INVALID_INVARIANT, "as its argument"},
		{`name -> Lest |$x| { true }`, diag.E_INVALID_INVARIANT, "lambda parameter"},
		// the receiver rule: a list builtin on a scalar or a key, a scalar
		// builtin on a list, an ordering builtin on instances
		{`name -> Filter |$c| { true } -> Len > 0`, diag.E_INVALID_INVARIANT, "takes a list"},
		{`PLACED_BY -> Sort -> Len > 0`, diag.E_INVALID_INVARIANT, "takes a list"},
		{`tags -> Upper == "A"`, diag.E_INVALID_INVARIANT, "takes a string"},
		{`LINES -> Sort -> First.qty > 0`, diag.E_INVALID_INVARIANT, "list of scalars"},
		// the bracket takes one index, and a number cannot be indexed
		{`tags[] -> IsNil`, diag.E_INVALID_INVARIANT, "exactly one index"},
		{`tags[0, 1] -> IsNil`, diag.E_INVALID_INVARIANT, "exactly one index"},
		{`LINES[0].qty[0] > 0`, diag.E_INVALID_INVARIANT, "cannot be indexed"},
		// a receiver the builtin refuses on every input: a number into a string
		// builtin, a string into a numeric one, a number into Len, Min or Max
		{`MAIN_LINE.qty -> Upper != ""`, diag.E_INVALID_INVARIANT, "takes a string"},
		{`name -> Abs > 0`, diag.E_INVALID_INVARIANT, "takes a number"},
		{`MAIN_LINE.qty -> Len > 0`, diag.E_INVALID_INVARIANT, "takes a string, a list or a map"},
		{`MAIN_LINE.qty -> Min == 1`, diag.E_INVALID_INVARIANT, "takes a list"},
		{`MAIN_LINE -> Max(1) != nil`, diag.E_INVALID_INVARIANT, "cannot be ordered"},
		{`LINES -> Map |$l| { $l.qty } -> Join(",") != ""`, diag.E_INVALID_INVARIANT, "list of strings"},
		{`tags -> Sum > 0`, diag.E_INVALID_INVARIANT, "list of numbers"},
		// in takes a list on its right
		{`name in name`, diag.E_INVALID_INVARIANT, "in takes a list"},
		// a ternary keeps the subkind its branches agree on
		{`(name != "" ? { MAIN_LINE.qty : MAIN_LINE.qty }) -> Upper != ""`, diag.E_INVALID_INVARIANT, "takes a string"},
		{`(name != "" ? { name : name }) -> Abs > 0`, diag.E_INVALID_INVARIANT, "takes a number"},
		// a boolean result cannot be indexed
		{`(name == "n")[0] != nil`, diag.E_INVALID_INVARIANT, "cannot be indexed"},
		// Default's fallback of another kind reaches the next stage unpredicted
		{`(tags -> Default("none") -> First) == nil`, diag.E_INVALID_INVARIANT, "Default"},
		{`(name -> Default(1)) -> Upper == "A"`, diag.E_INVALID_INVARIANT, "Default"},
		{`(tags -> Default([1]) -> First) == 1`, diag.E_INVALID_INVARIANT, "Default"},
		// Min and Max with an argument rank a scalar against it, never a list
		{`tags -> Max("z") != ""`, diag.E_INVALID_INVARIANT, "argument"},
		{`tags -> Min("z") != ""`, diag.E_INVALID_INVARIANT, "argument"},
		// a shape or a constraint keyword is not a datatype check
		{`name =~ Vector`, diag.E_INVALID_INVARIANT, "Vector"},
		{`name =~ List`, diag.E_INVALID_INVARIANT, "List"},
		{`name =~ Enum`, diag.E_INVALID_INVARIANT, "Enum"},
		{`name !~ Pattern`, diag.E_INVALID_INVARIANT, "Pattern"},
		// a composite key is a list at evaluation time, not a string
		{`REGION -> Upper != ""`, diag.E_INVALID_INVARIANT, "takes a string"},
		{`REGION + "!" != ""`, diag.E_INVALID_INVARIANT, "+ takes"},
		// a list of lists or of keys is not a list of numbers or strings
		{`matrix -> Sum > 0`, diag.E_INVALID_INVARIANT, "list of numbers"},
		{`matrix -> Join(",") != ""`, diag.E_INVALID_INVARIANT, "list of strings"},
		{`CUSTOMERS -> Sum > 0`, diag.E_INVALID_INVARIANT, "list of numbers"},
		// a boolean is not a number: + refuses it, as the evaluator does
		{`(f1 + f2) != nil`, diag.E_INVALID_INVARIANT, "+ takes"},
		{`(f1 + MAIN_LINE.qty) != nil`, diag.E_INVALID_INVARIANT, "+ takes"},
		{`f1 -> Abs > 0`, diag.E_INVALID_INVARIANT, "takes a number"},
		// the nil literal under + is an error on every input
		{`nil + 1 > 0`, diag.E_INVALID_INVARIANT, "+ takes"},
		{`(name + nil) != ""`, diag.E_INVALID_INVARIANT, "+ takes"},
		// and under the other arithmetic operators, and unary minus
		{`(-nil) > 0`, diag.E_INVALID_INVARIANT, "unary - takes a number"},
		{`nil - 1 > 0`, diag.E_INVALID_INVARIANT, "- takes two numbers"},
		{`nil * 1 > 0`, diag.E_INVALID_INVARIANT, "* takes two numbers"},
		{`nil / 1 > 0`, diag.E_INVALID_INVARIANT, "/ takes two numbers"},
		{`nil % 1 > 0`, diag.E_INVALID_INVARIANT, "% takes two numbers"},
		// every refuse arm of Default and the receiver kinds has its row
		{`PLACED_BY -> Default(0) -> Abs > 0`, diag.E_INVALID_INVARIANT, "Default"},
		// the nil-guard family refuses alternatives of disjoint kinds, and a
		// member read through a union of instances must be declared on every one
		{`(name -> Coalesce(1)) -> Upper == "A"`, diag.E_INVALID_INVARIANT, "Coalesce"},
		{`(name -> Coalesce(nil, 1)) -> Upper == "A"`, diag.E_INVALID_INVARIANT, "Coalesce"},
		{`(note -> Lest { 1 }) -> Upper == "A"`, diag.E_INVALID_INVARIANT, "Lest"},
		{`(extras -> Lest { "x" }) -> First == "x"`, diag.E_INVALID_INVARIANT, "Lest"},
		{`(MAIN_LINE -> Then |$l| { $l.qty }) -> Upper != ""`, diag.E_INVALID_INVARIANT, "takes a string"},
		// a string receiver beside a boolean body: with the receiver present the
		// invariant evaluates to the string, which is an evaluation error
		{`name -> Lest { true }`, diag.E_INVALID_INVARIANT, "Lest"},
		{`(MAIN_LINE -> Lest { LINES[0].ITEM }).sku != ""`, diag.E_UNKNOWN_PROPERTY, "sku"},
		{`(MAIN_LINE -> Coalesce(MAIN_LINE.ITEM)).qty > 0`, diag.E_UNKNOWN_PROPERTY, "qty"},
		{`(MAIN_LINE -> Default(MAIN_LINE.ITEM)).sku != ""`, diag.E_UNKNOWN_PROPERTY, "sku"},
		{`(ALT -> Default(OTHER)).extra != ""`, diag.E_UNKNOWN_PROPERTY, "extra"},
		{`(OTHER -> Default(ALT)).other != ""`, diag.E_UNKNOWN_PROPERTY, "other"},
		{`(f1 ? { MAIN_LINE : MAIN_LINE.ITEM }).qty > 0`, diag.E_UNKNOWN_PROPERTY, "qty"},
		{`(LINES -> Default([MAIN_LINE.ITEM]) -> First).qty > 0`, diag.E_UNKNOWN_PROPERTY, "qty"},
		// a non-empty scalar list is not the empty-list wildcard
		{`LINES -> Default(["a", 1]) -> Len > 0`, diag.E_INVALID_INVARIANT, "Default"},
		// a builtin's result of one subkind into a builtin that refuses it
		{`name -> Len -> Upper != ""`, diag.E_INVALID_INVARIANT, "takes a string"},
		{`name -> Upper -> Abs > 0`, diag.E_INVALID_INVARIANT, "takes a number"},
		{`(name -> StartsWith("n")) -> Abs > 0`, diag.E_INVALID_INVARIANT, "takes a number"},
		{`(name -> TypeOf) -> Abs > 0`, diag.E_INVALID_INVARIANT, "takes a number"},
		{`(tags -> Contains("a")) -> Upper != ""`, diag.E_INVALID_INVARIANT, "takes a string"},
		{`(MAIN_LINE.qty -> Compare(1)) -> Upper != ""`, diag.E_INVALID_INVARIANT, "takes a string"},
		{`(name -> Min("z")) -> Abs > 0`, diag.E_INVALID_INVARIANT, "takes a number"},
		{`(tags -> Join(",")) -> Abs > 0`, diag.E_INVALID_INVARIANT, "takes a number"},
		{`(name -> IsNil) -> Len > 0`, diag.E_INVALID_INVARIANT, "takes a string, a list or a map"},
		{`MAIN_LINE -> Compare("a") > 0`, diag.E_INVALID_INVARIANT, "total order"},
		{`LINES -> Min != nil`, diag.E_INVALID_INVARIANT, "list of scalars"},
		{`MAIN_LINE -> Upper != ""`, diag.E_INVALID_INVARIANT, "takes a string"},
		{`MAIN_LINE -> Abs > 0`, diag.E_INVALID_INVARIANT, "takes a number"},
		{`LINES -> Sum > 0`, diag.E_INVALID_INVARIANT, "list of numbers"},
		{`LINES -> Join(",") != ""`, diag.E_INVALID_INVARIANT, "list of strings"},
		{`f1 -> Len > 0`, diag.E_INVALID_INVARIANT, "takes a string, a list or a map"},
	}
	for _, tc := range refuse {
		t.Run("refuses "+tc.inv, func(t *testing.T) {
			t.Parallel()
			res := loadInvariant(t, tc.inv)
			if res.Err() == nil {
				t.Fatalf("illegal invariant loaded clean")
			}
			var found bool
			for is := range res.Issues() {
				if is.Code() == tc.code && strings.Contains(is.Message(), tc.want) {
					found = true
				}
			}
			if !found {
				t.Errorf("want %s mentioning %q; got %v", tc.code, tc.want, res.Err())
			}
		})
	}
}

// An inherited invariant was checked when its declaring type completed, and a
// subtype's scope is a superset of its ancestor's, so it is reported once.
func TestStaticInvariant_InheritedInvariantReportedOnce(t *testing.T) {
	t.Parallel()

	const src = `schema "s"

abstract type Base {
    id String primary
    ! "bad" nonexistent > 0
}

type A extends Base {
    x Integer
}

type B extends Base {
    y Integer
}
`
	_, res := schema.LoadString(t.Context(), src, "s.yammm")
	n := 0
	for is := range res.Issues() {
		if is.Code() == diag.E_UNKNOWN_PROPERTY {
			n++
		}
	}
	if n != 1 {
		t.Errorf("one declaration-site mistake drew %d E_UNKNOWN_PROPERTY diagnostics, want 1: %v", n, res.Err())
	}
}

// An inherited composition binds its element to the DECLARING schema's target,
// not to a same-named type in the reader — the relation's resolved TargetID is
// the identity, never a re-resolution of its syntactic reference.
func TestStaticInvariant_InheritedRelationBindsTheDeclaredTarget(t *testing.T) {
	t.Parallel()

	const base = `schema "base"

part type LineItem {
    sku String primary
    name String
}

abstract type HasLines {
    id String primary
    *-> LINES (one:many) LineItem
}
`
	app := func(member string) string {
		return `schema "app"

import "./base.yammm" as b

part type LineItem {
    sku String primary
    label String
}

type Order extends b.HasLines {
    ! "m" LINES -> All |$l| { $l.` + member + ` != "" }
}
`
	}
	load := func(t *testing.T, entry string) diag.Result {
		t.Helper()
		dir := t.TempDir()
		for name, src := range map[string]string{"base.yammm": base, "main.yammm": entry} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		_, res := schema.Load(t.Context(), filepath.Join(dir, "main.yammm"), schema.WithModuleRoot(dir))
		return res
	}

	if res := load(t, app("name")); res.Err() != nil {
		t.Errorf("the declaring schema's property was refused: %v", res.Err())
	}
	if res := load(t, app("label")); res.Err() == nil {
		t.Error("the reader's shadowing property was accepted")
	}
}

// One mistake is one diagnostic: a call with too many arguments is reported
// for its arity alone, not also for the receiver shape the extra argument
// implies.
func TestStaticInvariant_OneMistakeOneDiagnostic(t *testing.T) {
	t.Parallel()
	res := loadInvariant(t, `tags -> Min("a", "b") != ""`)
	var n int
	for is := range res.Issues() {
		if is.Code() == diag.E_INVALID_INVARIANT {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d E_INVALID_INVARIANT diagnostics, want 1: %v", n, res.Err())
	}
}

// A type reached through an import completed in its own schema, where its
// supertypes resolved. Judging it by whether its own inheritance refs resolve
// against the importing schema makes every such type look incomplete, and the
// member check on it is then skipped — so a typo'd member read through an
// imported type loads clean.
func TestStaticInvariant_MemberOnImportedTypeIsChecked(t *testing.T) {
	t.Parallel()
	sources := map[string][]byte{
		"entry.yammm": []byte(`schema "entry"

import "base.yammm" as base

type T {
	tid String primary
	*-> PART (one) base.Mid
	! "m" PART.nonexistent > 0
}
`),
		"base.yammm": []byte(`schema "base"

abstract type Ancestor {
	note String
}

part type Mid extends Ancestor {
	id String primary
}
`),
	}
	_, res := schema.LoadSourcesWithEntry(t.Context(), sources, "entry.yammm", ".", schema.WithSourcesOnly(true))
	if res.Err() == nil {
		t.Fatal("a typo'd member read through an imported type loaded clean")
	}
	if _, ok := issueWithCode(res, diag.E_UNKNOWN_PROPERTY); !ok {
		t.Errorf("want E_UNKNOWN_PROPERTY; got %v", res.Err())
	}
}

// A member the imported type really declares, inherited from its own
// schema's ancestor, still resolves — the guard admits the type rather than
// skipping the check.
func TestStaticInvariant_InheritedMemberOnImportedTypeResolves(t *testing.T) {
	t.Parallel()
	sources := map[string][]byte{
		"entry.yammm": []byte(`schema "entry"

import "base.yammm" as base

type T {
	tid String primary
	*-> PART (one) base.Mid
	! "m" PART.note != ""
}
`),
		"base.yammm": []byte(`schema "base"

abstract type Ancestor {
	note String
}

part type Mid extends Ancestor {
	id String primary
}
`),
	}
	_, res := schema.LoadSourcesWithEntry(t.Context(), sources, "entry.yammm", ".", schema.WithSourcesOnly(true))
	if res.Err() != nil {
		t.Errorf("a member inherited inside the imported schema was refused: %v", res.Err())
	}
}

func issueWithCode(res diag.Result, code diag.Code) (diag.Issue, bool) {
	for is := range res.Issues() {
		if is.Code() == code {
			return is, true
		}
	}
	return diag.Issue{}, false
}
