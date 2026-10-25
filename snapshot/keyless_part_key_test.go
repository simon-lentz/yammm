package snapshot_test

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
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

// partKeyCase is one key a bypass-built part carries, and whether graph.ParseKey
// reads each of its components back as itself.
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
	{"a whole float", []any{float64(5)}, true},
	{"a bool", []any{true}, true},
	{"a null", []any{nil}, true},
	{"two scalars", []any{"a", int64(1)}, true},
	{"a JSON number", []any{json.Number("12")}, true},
	{"a uint64 past int64, which reads back as a float64", []any{uint64(math.MaxUint64)}, false},
	{"a JSON number past int64", []any{json.Number("12345678901234567890")}, false},
	{"a negative zero, which reads back as 0", []any{math.Copysign(0, -1)}, false},
	{"a list", []any{[]any{"x"}}, false},
	{"an empty list", []any{[]any{}}, false},
	{"a map", []any{map[string]any{"a": int64(1)}}, false},
	{"a number no float64 holds", []any{json.Number("1e400")}, false},
	{"a string that is not UTF-8", []any{"\xff"}, false},
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

// partKeyChain returns the shape's chain as validated instances, the case's key
// on the deepest part and an empty key on every part above it.
func partKeyChain(t *testing.T, s *schema.Schema, shape partKeyShape, key []any, depth int) *instance.ValidInstance {
	t.Helper()
	typ, ok := s.Type(shape.types[depth])
	if !ok {
		t.Fatalf("no type %s", shape.types[depth])
	}
	if depth == len(shape.types)-1 {
		return instance.NewValidInstance(typ.Name(), typ.ID(), immutable.WrapKey(key), partKeyProps(typ.Name(), key), nil, nil, nil)
	}
	child := partKeyChain(t, s, shape, key, depth+1)
	return instance.NewValidInstance(typ.Name(), typ.ID(), immutable.WrapKey(nil), partKeyProps(typ.Name(), nil), nil,
		map[string]immutable.Value{shape.path[depth+1]: immutable.Wrap([]any{child})}, nil)
}

// partKeyParts is partKeyChain as RebuildSnapshot's parts.
func partKeyParts(t *testing.T, s *schema.Schema, shape partKeyShape, key []any, depth int) graph.InstanceParts {
	t.Helper()
	typ, _ := s.Type(shape.types[depth])
	if depth == len(shape.types)-1 {
		return graph.InstanceParts{TypeID: typ.ID(), PrimaryKey: immutable.WrapKey(key), Properties: partKeyProps(typ.Name(), key)}
	}
	return graph.InstanceParts{
		TypeID: typ.ID(), PrimaryKey: immutable.WrapKey(nil), Properties: partKeyProps(typ.Name(), nil),
		Composed: map[string][]graph.InstanceParts{shape.path[depth+1]: {partKeyParts(t, s, shape, key, depth+1)}},
	}
}

// partKeyStoredKeys returns every stored key of snap's roots and their composed
// children at every depth.
func partKeyStoredKeys(snap *graph.Snapshot) []immutable.Key {
	var keys []immutable.Key
	var walk func(*graph.Instance)
	walk = func(inst *graph.Instance) {
		keys = append(keys, inst.PrimaryKey())
		for _, rel := range inst.ComposedRelations() {
			for _, child := range inst.Composed(rel) {
				walk(child)
			}
		}
	}
	for inst := range snap.AllInstances() {
		walk(inst)
	}
	return keys
}

// TestPartKey_EveryConstructorsSnapshotLoadsBack holds every snapshot constructor to
// the .ys reader, a second implementation of the stored-key rule, over every
// key of a table on every part shape. A constructor that accepts a key gives a
// snapshot Marshal writes and Load reads, every stored key of which
// graph.ParseKey reads back; a constructor refuses with E_GRAPH_INVALID_PK
// exactly the keys holding a component ParseKey cannot read, keyed or not.
func TestPartKey_EveryConstructorsSnapshotLoadsBack(t *testing.T) {
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
		snap *graph.Snapshot
		res  diag.Result
	}
	constructors := map[string]func(partKeyShape, []any) built{
		"Graph.Add": func(shape partKeyShape, key []any) built {
			child := partKeyChain(t, s, shape, key, 0)
			doc := instance.NewValidInstance("Doc", docT.ID(), docKey, docProps, nil,
				map[string]immutable.Value{shape.path[0]: immutable.Wrap([]any{child})}, nil)
			g := graph.New(s)
			r := g.Add(ctx, doc)
			return built{g.Snapshot(), r}
		},
		"Graph.AddComposed": func(shape partKeyShape, key []any) built {
			g := graph.New(s)
			if r := g.Add(ctx, instance.NewValidInstance("Doc", docT.ID(), docKey, docProps, nil, nil, nil)); !r.OK() {
				t.Fatalf("Add the root: %s", r)
			}
			r := g.AddComposed(ctx, docT.ID(), docKey.String(), shape.path[0], partKeyChain(t, s, shape, key, 0))
			return built{g.Snapshot(), r}
		},
		"RebuildSnapshot": func(shape partKeyShape, key []any) built {
			snap, r := graph.RebuildSnapshot(s, graph.SnapshotParts{
				Types: []schema.TypeID{docT.ID()},
				Instances: []graph.InstanceParts{{
					TypeID: docT.ID(), PrimaryKey: docKey, Properties: docProps,
					Composed: map[string][]graph.InstanceParts{shape.path[0]: {partKeyParts(t, s, shape, key, 0)}},
				}},
			})
			return built{snap, r}
		},
	}

	accepted, refused := 0, 0
	for name, construct := range constructors {
		for _, shape := range partKeyShapes {
			for _, kc := range partKeyCases {
				if !kc.applies(shape) {
					continue
				}
				at := name + ", " + shape.name + ", " + kc.name
				b := construct(shape, kc.components)
				if !b.res.OK() {
					refused++
					if kc.readable {
						t.Errorf("%s: refused a key graph.ParseKey reads back: %s", at, b.res)
					}
					if !b.res.HasCode(diag.E_GRAPH_INVALID_PK) || !strings.Contains(b.res.String(), "graph.ParseKey reads back") {
						t.Errorf("%s: %s, want E_GRAPH_INVALID_PK naming the ParseKey rule", at, b.res)
					}
					continue
				}
				accepted++
				if !kc.readable {
					t.Errorf("%s: accepted a key with a component graph.ParseKey cannot read back", at)
				}
				snaps := map[string]*graph.Snapshot{name: b.snap}
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
