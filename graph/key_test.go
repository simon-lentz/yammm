package graph_test

import (
	"testing"
	"time"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

func TestFormatKey(t *testing.T) {
	tests := []struct {
		name   string
		values []any
		want   string
	}{
		{
			name:   "single string",
			values: []any{"ABC123"},
			want:   `["ABC123"]`,
		},
		{
			name:   "single int",
			values: []any{42},
			want:   `[42]`,
		},
		{
			name:   "composite string and int",
			values: []any{"us", 12345},
			want:   `["us",12345]`,
		},
		{
			name:   "empty slice spread",
			values: []any{},
			want:   `[]`,
		},
		{
			// The godoc's own no-argument case: a variadic given nothing is a
			// nil slice, which marshals to null rather than to [].
			name:   "no arguments",
			values: nil,
			want:   `null`,
		},
		{
			name:   "string with quotes",
			values: []any{`He said "hello"`},
			want:   `["He said \"hello\""]`,
		},
		{
			name:   "string with backslash",
			values: []any{`path\to\file`},
			want:   `["path\\to\\file"]`,
		},
		{
			name:   "string with brackets",
			values: []any{"[abc]"},
			want:   `["[abc]"]`,
		},
		{
			name:   "float value",
			values: []any{3.14159},
			want:   `[3.14159]`,
		},
		{
			name:   "boolean value",
			values: []any{true},
			want:   `[true]`,
		},
		{
			name:   "nil value",
			values: []any{nil},
			want:   `[null]`,
		},
		{
			name:   "unicode string",
			values: []any{"日本語"},
			want:   `["日本語"]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := graph.FormatKey(tt.values...)
			if got != tt.want {
				t.Errorf("graph.FormatKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatKey_PanicOnUnmarshalable(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("graph.FormatKey should panic on unmarshalable value")
		}
	}()

	// Channels cannot be JSON-marshaled
	ch := make(chan int)
	graph.FormatKey(ch)
}

func TestFormatComposedKey(t *testing.T) {
	t.Parallel()
	root := schema.NewTypeID(location.NewSourceID("test://car.yammm"), "Vehicle")

	tests := []struct {
		name    string
		rootKey []any
		path    []graph.ComposedStep
		want    string
		wantErr bool
	}{
		{
			name:    "one cardinality carries no child address",
			rootKey: []any{"ABC123"},
			path:    []graph.ComposedStep{{Relation: "ADDRESS"}},
			want:    `["test://car.yammm:Vehicle",["ABC123"],["ADDRESS"]]`,
		},
		{
			name:    "keyed child",
			rootKey: []any{"ABC123"},
			path:    []graph.ComposedStep{{Relation: "WHEELS", KeyOrIndex: []any{"front-left"}}},
			want:    `["test://car.yammm:Vehicle",["ABC123"],["WHEELS",["front-left"]]]`,
		},
		{
			name:    "keyless child is positional",
			rootKey: []any{"ABC123"},
			path:    []graph.ComposedStep{{Relation: "NOTES", KeyOrIndex: 0}},
			want:    `["test://car.yammm:Vehicle",["ABC123"],["NOTES",0]]`,
		},
		{
			// The address is FLAT: a second hop appends a segment instead of
			// nesting the first hop's rendering inside itself, so length grows
			// linearly with depth and nothing escapes anything.
			name:    "two hops append, they do not nest",
			rootKey: []any{"r1"},
			path: []graph.ComposedStep{
				{Relation: "MID", KeyOrIndex: []any{"m1"}},
				{Relation: "LEAF", KeyOrIndex: []any{"l1"}},
			},
			want: `["test://car.yammm:Vehicle",["r1"],["MID",["m1"]],["LEAF",["l1"]]]`,
		},
		{
			name:    "composite root key",
			rootKey: []any{"a", "b"},
			path:    []graph.ComposedStep{{Relation: "P", KeyOrIndex: 1}},
			want:    `["test://car.yammm:Vehicle",["a","b"],["P",1]]`,
		},
		{name: "nil root key", rootKey: nil, path: []graph.ComposedStep{{Relation: "ADDR"}}, wantErr: true},
		{name: "empty root key", rootKey: []any{}, path: []graph.ComposedStep{{Relation: "ADDR"}}, wantErr: true},
		{name: "empty path", rootKey: []any{"ABC"}, path: nil, wantErr: true},
		{name: "empty relation", rootKey: []any{"ABC"}, path: []graph.ComposedStep{{Relation: ""}}, wantErr: true},
		{name: "empty child key slice", rootKey: []any{"ABC"}, path: []graph.ComposedStep{{Relation: "W", KeyOrIndex: []any{}}}, wantErr: true},
		{name: "negative index", rootKey: []any{"ABC"}, path: []graph.ComposedStep{{Relation: "N", KeyOrIndex: -1}}, wantErr: true},
		{name: "invalid KeyOrIndex type", rootKey: []any{"ABC"}, path: []graph.ComposedStep{{Relation: "A", KeyOrIndex: "invalid"}}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := graph.FormatComposedKey(root, tt.rootKey, tt.path)
			if tt.wantErr {
				if err == nil {
					t.Errorf("FormatComposedKey(%v, %v) = %q, want an error", tt.rootKey, tt.path, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("FormatComposedKey: %v", err)
			}
			if got != tt.want {
				t.Errorf("FormatComposedKey = %q, want %q", got, tt.want)
			}
		})
	}
}

// A zero root type is refused: a key value is not an identity, and two root
// types sharing a key value and a relation name would otherwise mint the same
// address for children on one part label.
func TestFormatComposedKey_ZeroRootTypeRefused(t *testing.T) {
	t.Parallel()
	_, err := graph.FormatComposedKey(schema.TypeID{}, []any{"r1"},
		[]graph.ComposedStep{{Relation: "R", KeyOrIndex: 0}})
	if err == nil {
		t.Error("a zero root type was accepted")
	}
}

// Two roots of DIFFERENT types sharing a key value and a relation name get
// different addresses. Before the root's identity was part of the address they
// collided on one part label, silently.
func TestFormatComposedKey_RootIdentityDisambiguates(t *testing.T) {
	t.Parallel()
	src := location.NewSourceID("test://a.yammm")
	person := schema.NewTypeID(src, "Person")
	company := schema.NewTypeID(src, "Company")
	step := []graph.ComposedStep{{Relation: "ADDRESSES", KeyOrIndex: 0}}

	a, err := graph.FormatComposedKey(person, []any{"1"}, step)
	if err != nil {
		t.Fatalf("person: %v", err)
	}
	b, err := graph.FormatComposedKey(company, []any{"1"}, step)
	if err != nil {
		t.Fatalf("company: %v", err)
	}
	if a == b {
		t.Errorf("two root types sharing key %q mint the same composed address %q", "1", a)
	}
}

func timestampKeySchema(t *testing.T) *schema.Schema {
	t.Helper()
	s, result := schema.NewBuilder().
		WithName("stamped").
		WithSourceID(location.MustNewSourceID("test://stamped.yammm")).
		AddType("Event").
		WithPrimaryKey("observed_at", schema.TimestampConstraint{}).
		Done().
		Build()
	if result.HasErrors() {
		t.Fatalf("building the timestamp-key schema: %s", result.String())
	}
	return s
}

// Graph identity is Key.String(), so the two representations of one instant
// collide only where they render the same text. A canonical spelling and the
// time.Time parsed from it are one instance; a non-canonical spelling and its
// time.Time are two, which is a key that moves with the caller's choice of
// representation.
func TestGraph_TimestampKeyIdentityFollowsTheRendering(t *testing.T) {
	tests := []struct {
		spelling      string
		wantDuplicate bool
	}{
		{"2020-01-02T03:04:05Z", true},
		{"2020-01-02T03:04:05+00:00", false},
		{"2020-01-02T03:04:05.500Z", false},
	}

	for _, tt := range tests {
		t.Run(tt.spelling, func(t *testing.T) {
			s := timestampKeySchema(t)
			eventType, ok := s.Type("Event")
			if !ok {
				t.Fatal("Event not found in the timestamp-key schema")
			}
			parsed, err := time.Parse(time.RFC3339, tt.spelling)
			if err != nil {
				t.Fatalf("parse %q: %v", tt.spelling, err)
			}

			g := graph.New(s)
			first := instance.NewValidInstance("Event", eventType.ID(),
				immutable.WrapKey([]any{tt.spelling}),
				immutable.WrapProperties(map[string]any{"observed_at": tt.spelling}),
				nil, nil, nil)
			if r := g.Add(t.Context(), first); !r.OK() {
				t.Fatalf("adding the string-keyed instance: %s", r.String())
			}

			second := instance.NewValidInstance("Event", eventType.ID(),
				immutable.WrapKey([]any{parsed}),
				immutable.WrapProperties(map[string]any{"observed_at": parsed}),
				nil, nil, nil)
			result := g.Add(t.Context(), second)

			if gotDuplicate := !result.OK(); gotDuplicate != tt.wantDuplicate {
				t.Errorf("adding the time.Time-keyed instance: duplicate=%v, want %v (%s)",
					gotDuplicate, tt.wantDuplicate, result.String())
			}
		})
	}
}
