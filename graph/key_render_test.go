package graph_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/schema"
)

// keyRaw spells itself through MarshalJSON, whose bytes encoding/json copies
// into a key's rendering as they stand; it holds no string, so only its
// rendering shows the text.
type keyRaw struct{ s string }

func (k keyRaw) MarshalJSON() ([]byte, error) { return []byte(`"` + k.s + `"`), nil }

// keyUpper is a named string whose MarshalText spells other text than the
// string it is, as keyQuoted's MarshalJSON does.
type (
	keyUpper  string
	keyQuoted string
	keyPlain  string
)

func (k keyUpper) MarshalText() ([]byte, error)  { return []byte("UP-" + k), nil }
func (k keyQuoted) MarshalJSON() ([]byte, error) { return []byte(`"Q-` + k + `"`), nil }

// A key component is read back from its rendering, so one ParseKey would read
// as another value is refused: a struct, which is no form any writer writes,
// whatever it spells, an empty json.Number, which renders as 0, and a named
// type whose method spells other text than its base value, which every writer
// writes. Each could give two keys one address, or a key an address its
// written value does not name.
func TestAdd_RefusesAKeyComponentWhoseRenderingIsAnotherValue(t *testing.T) {
	t.Parallel()
	s, holder, _ := loadKeyUTF8Schema(t)
	for name, key := range map[string]any{
		"a struct spelling invalid UTF-8": keyRaw{"a\xffb"},
		"a struct spelling valid text":    keyRaw{"ok"},
		"an empty json.Number":            json.Number(""),
		"a string spelled by MarshalText": keyUpper("abc"),
		"a string spelled by MarshalJSON": keyQuoted("abc"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			g := graph.New(s)
			res := g.Add(t.Context(), instancetest.VI("Holder", instancetest.TypeID(holder), instancetest.PK(key),
				instancetest.Props(map[string]any{"id": key})))
			if !res.HasCode(diag.E_GRAPH_INVALID_PK) {
				t.Errorf("no %s for a key component whose rendering is another value: %s", diag.E_GRAPH_INVALID_PK, res)
			}
			for inst := range g.Snapshot().AllInstances() {
				t.Errorf("Add installed %s for a refused key", inst.PrimaryKey())
			}
		})
	}
}

const pairKeySchema = `schema "pairkey"

type Pair {
	a String primary
	b String primary
}
`

// The refusal names the component that is not valid UTF-8 by its position.
func TestAdd_NamesTheKeyComponentThatIsNotValidUTF8(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), pairKeySchema, "pairkey.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	pair := mustTypeID(t, s, "Pair")
	g := graph.New(s)
	added := g.Add(t.Context(), instancetest.VI("Pair", instancetest.TypeID(pair), instancetest.PK("ok", "a\xffb"),
		instancetest.Props(map[string]any{"a": "ok", "b": "a\xffb"})))
	requireInvalidKey(t, added)
	if !strings.Contains(added.String(), "component 1") {
		t.Errorf("the refusal does not name component 1: %s", added)
	}
}

// A carrier whose rendering is its form's is read back as the value it is: a
// named string with no method and a pointer to a string are each the string.
func TestAdd_AcceptsAKeyComponentWhoseRenderingIsItsForm(t *testing.T) {
	t.Parallel()
	s, holder, _ := loadKeyUTF8Schema(t)
	text := "abc"
	for name, key := range map[string]any{
		"a named string":        keyPlain("abc"),
		"a pointer to a string": &text,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			g := graph.New(s)
			if res := g.Add(t.Context(), instancetest.VI("Holder", instancetest.TypeID(holder), instancetest.PK(key),
				instancetest.Props(map[string]any{"id": "abc"}))); res.HasErrors() {
				t.Errorf("Add refused a key component whose rendering is its form: %s", res)
			}
		})
	}
}

const renderedKeySchema = `schema "renderedkey"

type Stamp = Timestamp

type Aliased {
	at Stamp primary
}

type Stamped {
	at Timestamp primary
}

type Identified {
	uid UUID primary
}
`

// A time.Time or a uuid.UUID is a key component only where its key property's
// constraint renders it, as every writer writes it: a String key holding one is
// refused as a root's key, a target key and an unresolved record's target key,
// and a Timestamp or a UUID key holding one is accepted, through an alias too.
func TestKeys_ATimeOrAUUIDComponentItsConstraintDoesNotRenderIsRefused(t *testing.T) {
	t.Parallel()
	s, holder, _ := loadKeyUTF8Schema(t)
	at := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	id := uuid.MustParse("12345678-1234-1234-1234-123456789abc")
	for name, key := range map[string]any{"a time": at, "a uuid": id, "a pointer to a uuid": &id} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			text := fmt.Sprint(key)
			root := graph.New(s)
			requireInvalidKey(t, root.Add(t.Context(), instancetest.VI("Holder", instancetest.TypeID(holder), instancetest.PK(key),
				instancetest.Props(map[string]any{"id": text}))))
			target := graph.New(s)
			requireInvalidKey(t, target.Add(t.Context(), holderVI(holder, "h1", instancetest.Edges(map[string]*instance.ValidEdgeData{
				"LINK": instance.NewValidEdgeData([]instance.ValidEdgeTarget{
					instance.NewValidEdgeTarget(immutable.WrapKey([]any{key}), immutable.Properties{}),
				}),
			}))))
			snap, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
				Instances: []graph.InstanceParts{{
					TypeID: holder, PrimaryKey: immutable.WrapKey([]any{"h1"}),
					Properties: immutable.WrapProperties(map[string]any{"id": "h1"}),
				}},
				Unresolved: []graph.UnresolvedParts{{
					SourceType: holder, SourceKey: immutable.WrapKey([]any{"h1"}),
					Relation: "LINK", TargetKey: immutable.WrapKey([]any{key}), Reason: "target_missing",
				}},
			})
			if snap != nil {
				t.Errorf("RebuildSnapshot built a snapshot holding the target key")
			}
			requireInvalidKey(t, res)
		})
	}

	rs, res := schema.LoadString(t.Context(), renderedKeySchema, "renderedkey.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	g := graph.New(rs)
	for _, c := range []struct {
		typ, prop string
		key       any
	}{{"Stamped", "at", at}, {"Stamped", "at", &at}, {"Aliased", "at", &at}, {"Identified", "uid", id}, {"Identified", "uid", &id}} {
		typ := mustTypeID(t, rs, c.typ)
		if r := g.Add(t.Context(), instancetest.VI(c.typ, instancetest.TypeID(typ), instancetest.PK(c.key),
			instancetest.Props(map[string]any{c.prop: c.key}))); r.HasCode(diag.E_GRAPH_INVALID_PK) {
			t.Errorf("%s key %T refused: %s", c.typ, c.key, r)
		}
	}
}

// A time.Time or a uuid.UUID is read against its own key property's constraint
// at every position of a composite key, not only the first.
func TestAdd_AcceptsATimeAndAUUIDPastTheFirstKeyComponent(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s, res := schema.LoadString(ctx, `schema "ev"

type Ev {
	name String primary
	at Timestamp primary
	u UUID primary
}
`, "ev.yammm")
	if res.HasErrors() {
		t.Fatalf("schema: %s", res)
	}
	ev, _ := s.Type("Ev")
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	u := uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
	inst := instance.NewValidInstance("Ev", ev.ID(), immutable.WrapKey([]any{"x", &at, &u}),
		immutable.WrapProperties(map[string]any{"name": "x", "at": &at, "u": &u}), nil, nil, nil)
	if r := graph.New(s).Add(ctx, inst); !r.OK() {
		t.Errorf("Add refused a key whose time and UUID its constraints render: %s", r)
	}
}
