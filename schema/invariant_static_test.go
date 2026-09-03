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
// evaluator would refuse. Each row states one shape docs/SPEC.md settles.
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

type Order {
    id String primary
    name String
    tags List<String>
    *-> LINES (one:many) Line
    *-> MAIN_LINE (one) Line
    --> PLACED_BY (one) Customer
    --> CUSTOMERS (one:many) Customer
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
		`LINES -> Sort -> First.qty > 0`,
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
		// an undefined named variable is a guaranteed evaluation error
		{`$undefined > 0`, diag.E_INVALID_INVARIANT, "undefined variable"},
		// an unknown function and a call shape the builtin refuses
		{`LINES -> Bogus > 0`, diag.E_INVALID_INVARIANT, "Bogus"},
		{`LINES -> Len |$l| { $l.qty } > 0`, diag.E_INVALID_INVARIANT, "lambda"},
		{`LINES -> All > 0`, diag.E_INVALID_INVARIANT, "lambda"},
		{`name -> Substring(1, 2, 3) != ""`, diag.E_INVALID_INVARIANT, "argument"},
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
