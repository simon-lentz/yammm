package snapshot_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

const storedKeySchema = `schema "keys"

type Company {
	id String primary
}

type Token {
	u UUID primary
}

type Event {
	at Timestamp primary
}

type Pair {
	a String primary
	b Date primary
}

type Holder {
	id String primary
	*-> PARTS (many) Part
}

part type Part {
	code String primary
	at Timestamp primary
}
`

// keyShape names one way a stored key can stand beside the key properties.
type keyShape struct {
	name  string
	legal bool
	// key and props build the instance from its declared key values.
	key   func(vals []any) []any
	props func(names []string, vals []any) map[string]any
}

func propsOf(names []string, vals []any) map[string]any {
	m := make(map[string]any, len(names))
	for i, n := range names {
		m[n] = vals[i]
	}
	return m
}

const (
	lowerUUID = "123e4567-e89b-12d3-a456-426614174000"
	upperUUID = "123E4567-E89B-12D3-A456-426614174000"
)

// differ returns vals with its last component replaced by another valid value
// of the same kind.
func differ(vals []any) []any {
	out := append([]any(nil), vals...)
	if v, ok := out[len(out)-1].(string); ok {
		switch v {
		case canonicalStamp:
			out[len(out)-1] = "2021-01-02T03:04:05Z"
		case lowerUUID:
			out[len(out)-1] = "223e4567-e89b-12d3-a456-426614174000"
		case "2020-01-02":
			out[len(out)-1] = "2021-01-02"
		default:
			out[len(out)-1] = v + "x"
		}
	}
	return out
}

// respell writes every Timestamp and UUID component in another spelling of
// the same value.
func respell(vals []any) []any {
	out := append([]any(nil), vals...)
	for i, v := range out {
		switch v {
		case canonicalStamp:
			out[i] = rawStamp
		case lowerUUID:
			out[i] = upperUUID
		}
	}
	return out
}

func keyShapes() []keyShape {
	same := func(vals []any) []any { return vals }
	return []keyShape{
		{"agree", true, same, propsOf},
		{"respelled", true, respell, propsOf},
		{"key_property_absent", false, same, func(names []string, vals []any) map[string]any {
			return propsOf(names[:len(names)-1], vals)
		}},
		{"key_property_null", false, same, func(names []string, vals []any) map[string]any {
			m := propsOf(names, vals)
			m[names[len(names)-1]] = nil
			return m
		}},
		{"differs", false, differ, propsOf},
		{"short", false, func(v []any) []any { return v[:len(v)-1] }, propsOf},
		{"long", false, func(v []any) []any { return append(append([]any(nil), v...), "extra") }, propsOf},
		{"empty", false, func([]any) []any { return nil }, propsOf},
	}
}

// keyedType names a keyed type, its key properties, and one value for each.
type keyedType struct {
	name     string
	composed bool
	names    []string
	vals     []any
}

func keyedTypes() []keyedType {
	return []keyedType{
		{"Company", false, []string{"id"}, []any{"c1"}},
		{"Token", false, []string{"u"}, []any{lowerUUID}},
		{"Event", false, []string{"at"}, []any{canonicalStamp}},
		{"Pair", false, []string{"a", "b"}, []any{"p", "2020-01-02"}},
		{"Part", true, []string{"code", "at"}, []any{"k1", canonicalStamp}},
	}
}

type storedKeyCase struct {
	typ   keyedType
	shape keyShape
}

func (c storedKeyCase) key() []any            { return c.shape.key(c.typ.vals) }
func (c storedKeyCase) props() map[string]any { return c.shape.props(c.typ.names, c.typ.vals) }
func (c storedKeyCase) String() string        { return c.typ.name + "/" + c.shape.name }
func holderProps() map[string]any             { return map[string]any{"id": "h1"} }
func holderKey() immutable.Key                { return immutable.WrapKey([]any{"h1"}) }
func loadStoredKeySchema(t *testing.T) *schema.Schema {
	t.Helper()
	s, res := schema.LoadString(t.Context(), storedKeySchema, "keys.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	return s
}

func (c storedKeyCase) parts(t *testing.T, s *schema.Schema) graph.SnapshotParts {
	t.Helper()
	id := mustTypeID(t, s, c.typ.name)
	ip := graph.InstanceParts{TypeID: id, PrimaryKey: immutable.WrapKey(c.key()), Properties: immutable.WrapProperties(c.props())}
	if !c.typ.composed {
		return graph.SnapshotParts{Types: []schema.TypeID{id}, Instances: []graph.InstanceParts{ip}}
	}
	holder := mustTypeID(t, s, "Holder")
	return graph.SnapshotParts{Types: []schema.TypeID{holder}, Instances: []graph.InstanceParts{{
		TypeID: holder, PrimaryKey: holderKey(), Properties: immutable.WrapProperties(holderProps()),
		Composed: map[string][]graph.InstanceParts{"PARTS": {ip}},
	}}}
}

func (c storedKeyCase) add(t *testing.T, s *schema.Schema) bool {
	t.Helper()
	id := mustTypeID(t, s, c.typ.name)
	inst := instance.NewValidInstance(c.typ.name, id, immutable.WrapKey(c.key()), immutable.WrapProperties(c.props()), nil, nil, nil)
	if c.typ.composed {
		inst = instance.NewValidInstance("Holder", mustTypeID(t, s, "Holder"), holderKey(), immutable.WrapProperties(holderProps()),
			nil, map[string]immutable.Value{"PARTS": immutable.Wrap([]any{inst})}, nil)
	}
	return graph.New(s).Add(context.Background(), inst).OK()
}

type keyWireItem struct {
	Key        []any                    `json:"key"`
	Type       *int                     `json:"type,omitempty"`
	Properties map[string]any           `json:"properties"`
	Composed   map[string][]keyWireItem `json:"composed,omitempty"`
	Provenance any                      `json:"provenance"`
}

type keyWireGroup struct {
	Type  int           `json:"type"`
	Items []keyWireItem `json:"items"`
}

// keyWireFrame marshals a legal snapshot holding every type, whose header and
// types table each wire case reuses with its own instances section.
func keyWireFrame(t *testing.T, s *schema.Schema) (head, tail string, rows map[string]int) {
	t.Helper()
	var ids []schema.TypeID
	var insts []graph.InstanceParts
	for _, kt := range keyedTypes() {
		c := storedKeyCase{kt, keyShapes()[0]}
		p := c.parts(t, s)
		ids = append(ids, p.Types...)
		insts = append(insts, p.Instances...)
	}
	built, res := graph.RebuildSnapshot(s, graph.SnapshotParts{Types: ids, Instances: insts})
	if res.HasErrors() {
		t.Fatalf("the legal frame was refused: %s", res)
	}
	data, mres := snapshot.Marshal(context.Background(), built)
	if mres.HasErrors() {
		t.Fatalf("marshal: %s", mres)
	}
	var doc struct {
		Types []struct{ Name string } `json:"types"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	rows = map[string]int{}
	for i, e := range doc.Types {
		rows[e.Name] = i
	}
	text := string(data)
	a := strings.Index(text, `"instances":`)
	b := strings.Index(text, `,"diagnostics":`)
	if a < 0 || b < a {
		t.Fatalf("frame shape changed: %s", data)
	}
	return text[:a+len(`"instances":`)], text[b:], rows
}

func (c storedKeyCase) wire(t *testing.T, head, tail string, rows map[string]int) []byte {
	t.Helper()
	item := keyWireItem{Key: c.key(), Properties: c.props()}
	if item.Key == nil {
		item.Key = []any{}
	}
	group := keyWireGroup{Type: rows[c.typ.name], Items: []keyWireItem{item}}
	if c.typ.composed {
		row := rows["Part"]
		item.Type = &row
		group = keyWireGroup{Type: rows["Holder"], Items: []keyWireItem{{
			Key: []any{"h1"}, Properties: holderProps(), Composed: map[string][]keyWireItem{"PARTS": {item}},
		}}}
	}
	body, err := json.Marshal([]keyWireGroup{group})
	if err != nil {
		t.Fatal(err)
	}
	return []byte(head + string(body) + tail)
}

// TestStoredKey_EveryImplementationAgrees holds the key rule over the class of
// key kinds (a String, a UUID and a Timestamp, both canonicalizing, and a
// composite key ending in a Date)
// at a root and at a composed child, crossed with every way a stored key can
// stand beside its key properties. Graph.Add, graph.RebuildSnapshot,
// snapshot.Load and snapshot.Verify each accept exactly the legal shapes; a key
// property that is absent or null is refused whatever the stored key holds.
func TestStoredKey_EveryImplementationAgrees(t *testing.T) {
	t.Parallel()
	s := loadStoredKeySchema(t)
	head, tail, rows := keyWireFrame(t, s)
	n := 0
	for _, kt := range keyedTypes() {
		for _, shape := range keyShapes() {
			c := storedKeyCase{kt, shape}
			n++
			t.Run(c.String(), func(t *testing.T) {
				t.Parallel()
				if got := c.add(t, s); got != shape.legal {
					t.Errorf("Graph.Add accepted=%v, want %v", got, shape.legal)
				}
				_, rres := graph.RebuildSnapshot(s, c.parts(t, s))
				if got := !rres.HasErrors(); got != shape.legal {
					t.Errorf("RebuildSnapshot accepted=%v, want %v: %s", got, shape.legal, rres)
				}
				data := c.wire(t, head, tail, rows)
				_, lres := snapshot.Load(context.Background(), data, s, snapshot.WithIntegrityCheck(false))
				if got := !lres.HasErrors(); got != shape.legal {
					t.Errorf("Load accepted=%v, want %v: %s", got, shape.legal, lres)
				}
				vres := snapshot.Verify(context.Background(), data, s, snapshot.WithIntegrityCheck(false))
				if got := !vres.HasErrors(); got != shape.legal {
					t.Errorf("Verify accepted=%v, want %v: %s", got, shape.legal, vres)
				}
			})
		}
	}
	if n != 40 {
		t.Fatalf("the class holds %d cases, want 40", n)
	}
}
