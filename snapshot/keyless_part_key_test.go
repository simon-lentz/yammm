package snapshot_test

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/internal/snappartstest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

const partKeySchema = `schema "partkey"

type Doc {
	id String primary
	*-> LINES (_:many) Line
	*-> NOTE (_:one) Note
	*-> KLINES (_:many) KLine
}

part type Line {
	label String
	*-> SUBS (_:many) Sub
}

part type Note {
	text String
}

part type Sub {
	n Integer
}

part type KLine {
	k String primary
}
`

// partKeyShape is one place a part sits under a Doc root: the relation path
// from the root, the part type at its end, and whether that type is keyed.
type partKeyShape struct {
	name  string
	path  []string
	types []string
	keyed bool
}

var partKeyShapes = []partKeyShape{
	{"a keyless (many) part", []string{"LINES"}, []string{"Line"}, false},
	{"a keyless (one) part", []string{"NOTE"}, []string{"Note"}, false},
	{"a keyless part under a keyless part", []string{"LINES", "SUBS"}, []string{"Line", "Sub"}, false},
	{"a keyed (many) part", []string{"KLINES"}, []string{"KLine"}, true},
}

// partKeyCase is one key a part stated in a document carries, and whether
// graph.ParseKey reads each of its components back as itself.
type partKeyCase struct {
	name       string
	components []any
	readable   bool
}

var partKeyCases = []partKeyCase{
	{"no component", nil, true},
	{"a string", []any{"x"}, true},
	{"an integer", []any{int64(7)}, true},
	{"a fraction", []any{1.5}, true},
	{"a whole float, which a key spells as the integer", []any{float64(5)}, false},
	{"a bool", []any{true}, true},
	{"a null", []any{nil}, true},
	{"two scalars", []any{"a", int64(1)}, true},
	{"a JSON number", []any{json.Number("12")}, true},
	{"a JSON number past int64", []any{json.Number("12345678901234567890")}, false},
	{"a negative zero, which reads back as 0", []any{math.Copysign(0, -1)}, false},
	{"a list", []any{[]any{"x"}}, false},
	{"an empty list", []any{[]any{}}, false},
	{"a map", []any{map[string]any{"a": int64(1)}}, false},
	{"a number no float64 holds", []any{json.Number("1e400")}, false},
	{"a list after a scalar", []any{"a", []any{"x"}}, false},
}

// applies reports whether the case is one the shape's type can take. A keyed
// part's key must agree with its key property, so only a key of one non-null
// component is a key of that type at all.
func (c partKeyCase) applies(shape partKeyShape) bool {
	return !shape.keyed || (len(c.components) == 1 && c.components[0] != nil)
}

func partKeyProps(typ string, key []any) immutable.Properties {
	switch typ {
	case "KLine":
		return immutable.WrapProperties(map[string]any{"k": key[0]})
	case "Line":
		return immutable.WrapProperties(map[string]any{"label": "l"})
	case "Note":
		return immutable.WrapProperties(map[string]any{"text": "t"})
	default:
		return immutable.WrapProperties(map[string]any{"n": int64(1)})
	}
}

// validatorStates reports whether a validator can build the case's key at the
// shape: an empty key on a keyless part, a string on a keyed one.
func (c partKeyCase) validatorStates(shape partKeyShape) bool {
	if !shape.keyed {
		return len(c.components) == 0
	}
	s, ok := c.components[0].(string)
	return ok && utf8.ValidString(s)
}

// partKeyInput returns the shape's chain as a validator's input, the case's key
// on the deepest part.
func partKeyInput(shape partKeyShape, key []any, depth int) map[string]any {
	name := shape.types[depth]
	input := partKeyProps(name, key).Clone()
	if depth < len(shape.types)-1 {
		input[strings.ToLower(shape.path[depth+1])] = []any{partKeyInput(shape, key, depth+1)}
	}
	return input
}

// partKeyParts returns the shape's chain as a document's parts, the case's key on
// the deepest part and an empty key on every part above it.
func partKeyParts(t *testing.T, s schema.Schema, shape partKeyShape, key []any, depth int) snapparts.Instance {
	t.Helper()
	typ, _ := s.Type(shape.types[depth])
	if depth == len(shape.types)-1 {
		return snapparts.Instance{TypeID: typ.ID(), PrimaryKey: immutable.WrapKey(key), Properties: partKeyProps(typ.Name(), key)}
	}
	return snapparts.Instance{
		TypeID: typ.ID(), PrimaryKey: immutable.WrapKey(nil), Properties: partKeyProps(typ.Name(), nil),
		Composed: map[string][]snapparts.Instance{shape.path[depth+1]: {partKeyParts(t, s, shape, key, depth+1)}},
	}
}

// partKeyStoredKeys returns every stored key of snap's roots and their composed
// children at every depth.
func partKeyStoredKeys(snap graph.Snapshot) []immutable.Key {
	var keys []immutable.Key
	var walk func(graph.Instance)
	walk = func(inst graph.Instance) {
		keys = append(keys, inst.PrimaryKey())
		for _, rel := range inst.ComposedRelations() {
			for child := range inst.Composed(rel) {
				walk(child)
			}
		}
	}
	for inst := range snap.AllInstances() {
		walk(inst)
	}
	return keys
}

// TestPartKey_EverySnapshotLoadsBack holds both ways to a snapshot, Graph.Add and
// the .ys reader, to the reader, over every key of a table on every part shape. A
// snapshot either builds is one Marshal writes and Load reads, every stored key of
// which graph.ParseKey reads back. The reader refuses exactly the keys holding a
// component ParseKey cannot read, keyed or not. Graph.Add takes only the keys a
// validator states.
func TestPartKey_EverySnapshotLoadsBack(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s, res := schema.LoadString(ctx, partKeySchema, "partkey.yammm")
	if res.HasErrors() {
		t.Fatalf("schema: %s", res)
	}
	docT, _ := s.Type("Doc")
	docKey := immutable.WrapKey([]any{"d1"})
	docProps := immutable.WrapProperties(map[string]any{"id": "d1"})

	type built struct {
		snap graph.Snapshot
		res  diag.Result
	}
	builders := map[string]func(partKeyShape, []any) built{
		"Graph.Add": func(shape partKeyShape, key []any) built {
			doc := instancetest.Valid(t, s, "Doc", map[string]any{
				"id": "d1", strings.ToLower(shape.path[0]): []any{partKeyInput(shape, key, 0)},
			})
			g := graph.New(s)
			r := g.Add(ctx, doc)
			return built{g.Snapshot(), r}
		},
		"snapshot.Load": func(shape partKeyShape, key []any) built {
			snap, r := snapshot.Load(ctx, snappartstest.Document(t, s, snapparts.Parts{
				Types: []schema.TypeID{docT.ID()},
				Instances: []snapparts.Instance{{
					TypeID: docT.ID(), PrimaryKey: docKey, Properties: docProps,
					Composed: map[string][]snapparts.Instance{shape.path[0]: {partKeyParts(t, s, shape, key, 0)}},
				}},
			}), s)
			return built{snap, r}
		},
	}

	accepted, refused := 0, 0
	for name, build := range builders {
		for _, shape := range partKeyShapes {
			for _, kc := range partKeyCases {
				if !kc.applies(shape) || (name == "Graph.Add" && !kc.validatorStates(shape)) {
					continue
				}
				at := name + ", " + shape.name + ", " + kc.name
				b := build(shape, kc.components)
				if !b.res.OK() {
					refused++
					if kc.readable {
						t.Errorf("%s: refused a key graph.ParseKey reads back: %s", at, b.res)
					}
					if !b.res.HasCode(diag.E_SNAPSHOT_MALFORMED) {
						t.Errorf("%s: %s, want %s", at, b.res, diag.E_SNAPSHOT_MALFORMED)
					}
					continue
				}
				accepted++
				if !kc.readable {
					t.Errorf("%s: accepted a key with a component graph.ParseKey cannot read back", at)
				}
				snaps := map[string]graph.Snapshot{name: b.snap}
				if g, r := graph.NewFromSnapshot(s, b.snap); r.OK() {
					snaps["NewFromSnapshot of "+name] = g.Snapshot()
				} else {
					t.Errorf("%s: NewFromSnapshot refused the snapshot: %s", at, r)
				}
				for from, snap := range snaps {
					data, mr := snapshot.Marshal(ctx, snap)
					if !mr.OK() {
						t.Errorf("%s: Marshal of the %s snapshot: %s", at, from, mr)
						continue
					}
					loaded, lr := snapshot.Load(ctx, data, s)
					if !lr.OK() {
						t.Errorf("%s: Load refused what Marshal wrote from the %s snapshot: %s", at, from, lr)
						continue
					}
					for _, k := range partKeyStoredKeys(loaded) {
						if _, err := graph.ParseKey(k.String()); err != nil {
							t.Errorf("%s: the loaded key %s does not read back: %v", at, k, err)
						}
					}
				}
			}
		}
	}
	if accepted == 0 || refused == 0 {
		t.Fatalf("the table drew %d acceptances and %d refusals; each needs one", accepted, refused)
	}
}
