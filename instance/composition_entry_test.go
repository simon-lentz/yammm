package instance_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

const docWithAssociation = `schema "p"

type Company {
    id String primary
}

part type Line {
    n String primary
    qty Integer
}

type Doc {
    id String primary
    --> ISSUER (one) Company
    *-> LINES (one:many) Line
}
`

func loadDoc(t *testing.T) *schema.Schema {
	t.Helper()
	return loadSrc(t, docWithAssociation)
}

// An association is not a composition, whatever Type.Relation indexes.
func TestValidateForComposition_RefusesAssociationName(t *testing.T) {
	v := instance.NewValidator(loadDoc(t))
	valids, res := v.ValidateForComposition(t.Context(), "Doc", "ISSUER",
		[]instance.RawInstance{{Properties: map[string]any{"id": "c1"}}})
	if valids != nil {
		t.Errorf("valids = %v, want nil", valids)
	}
	if !res.HasCode(instance.ErrCompositionNotFound) {
		t.Errorf("want E_COMPOSITION_NOT_FOUND, got %s", res)
	}
}

// The nil-raws shortcut must not skip the resolution an empty slice performs.
func TestValidateForComposition_ResolvesBeforeNilRaws(t *testing.T) {
	v := instance.NewValidator(loadDoc(t))
	_, res := v.ValidateForComposition(t.Context(), "NoSuchType", "LINES", nil)
	if !res.HasCode(instance.ErrTypeNotFound) {
		t.Errorf("unknown parent with nil raws: want E_INSTANCE_TYPE_NOT_FOUND, got %s", res)
	}
	_, res = v.ValidateForComposition(t.Context(), "Doc", "NO_SUCH", nil)
	if !res.HasCode(instance.ErrCompositionNotFound) {
		t.Errorf("unknown relation with nil raws: want E_COMPOSITION_NOT_FOUND, got %s", res)
	}
}

// The relation resolves by either spelling, as every other name-taking
// surface in the package accepts.
func TestValidateForComposition_AcceptsFieldNameSpelling(t *testing.T) {
	v := instance.NewValidator(loadDoc(t))
	raws := []instance.RawInstance{{Properties: map[string]any{"n": "a", "qty": 1}}}
	for _, name := range []string{"LINES", "lines"} {
		valids, res := v.ValidateForComposition(t.Context(), "Doc", name, raws)
		if !res.OK() {
			t.Fatalf("%s: %s", name, res)
		}
		if len(valids) != 1 {
			t.Fatalf("%s: got %d valids, want 1", name, len(valids))
		}
	}
}

const selfNesting = `schema "p"

part type Node {
    n String primary
    *-> KIDS (many) Node
}

type Root {
    id String primary
    *-> KIDS (many) Node
}
`

// nestedRoot returns a Root whose deepest composed child sits at the given
// depth, the root itself being depth 0.
func nestedRoot(depth int) map[string]any {
	var cur map[string]any
	for d := depth; d >= 1; d-- {
		node := map[string]any{"n": fmt.Sprintf("n%d", d)}
		if cur != nil {
			node["kids"] = []any{cur}
		}
		cur = node
	}
	root := map[string]any{"id": "r"}
	if cur != nil {
		root["kids"] = []any{cur}
	}
	return root
}

// A validated instance is always one the snapshot writer will accept: the
// composed-nesting bound is enforced here, on the same number.
func TestValidateOne_RefusesCompositionPastMaxDepth(t *testing.T) {
	v := instance.NewValidator(loadSrc(t, selfNesting))

	_, res := v.ValidateOne(t.Context(), "Root", instance.RawInstance{Properties: nestedRoot(instance.MaxComposedDepth)})
	if !res.OK() {
		t.Fatalf("depth %d must validate: %s", instance.MaxComposedDepth, res)
	}

	_, res = v.ValidateOne(t.Context(), "Root", instance.RawInstance{Properties: nestedRoot(instance.MaxComposedDepth + 1)})
	if res.OK() {
		t.Fatalf("depth %d must be refused", instance.MaxComposedDepth+1)
	}
	is, ok := issueWithCode(res, instance.ErrCompositionDepthExceeded)
	if !ok {
		t.Fatalf("want E_COMPOSITION_DEPTH_EXCEEDED, got %s", res)
	}
	if is.Severity() != diag.Error {
		t.Errorf("severity = %v, want Error", is.Severity())
	}
	if got := detailValues(is, diag.DetailKeyDepth); len(got) != 1 || got[0] != strconv.Itoa(instance.MaxComposedDepth+1) {
		t.Errorf("depth detail = %v", got)
	}
	if !strings.Contains(is.Path(), "kids") {
		t.Errorf("path %q does not name the offending composition", is.Path())
	}
}

// Two children on a (one) slot is the same fact graph reports under
// E_DUPLICATE_COMPOSED_PK; the instance layer names it the same way.
func TestValidateOne_OneCompositionOverflowIsDuplicateComposedPK(t *testing.T) {
	v := instance.NewValidator(loadSrc(t, `schema "p"

part type Home {
    id String primary
}

type Person {
    id String primary
    *-> HOME (one) Home
}
`))
	_, res := v.ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: map[string]any{
		"id": "p", "home": []any{map[string]any{"id": "h1"}, map[string]any{"id": "h2"}},
	}})
	if !res.HasCode(instance.ErrDuplicateComposedPK) || res.HasCode(instance.ErrEdgeShapeMismatch) {
		t.Errorf("want E_DUPLICATE_COMPOSED_PK alone, got %s", res)
	}
}
