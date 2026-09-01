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

// TestGraph_TimestampKeyIdentityIsCanonical pins that a key denotes a VALUE,
// not a spelling: a Timestamp key written as text and the same instant written
// as a time.Time are one instance, whichever text the caller chose.
//
// It formerly pinned the opposite — that identity follows the rendering, so
// "…T03:04:05+00:00" and "…T03:04:05Z" were two instances. That contradicted
// checkInstanceKey's own rule, which compares a key against its backing
// property through "the canonical rendering, the form that decides key
// equality on the wire", and it let one entity occupy two addresses in the
// index, in every edge that referenced it, and in the document.
func TestGraph_TimestampKeyIdentityIsCanonical(t *testing.T) {
	tests := []struct {
		spelling      string
		wantDuplicate bool
	}{
		{"2020-01-02T03:04:05Z", true},
		{"2020-01-02T03:04:05+00:00", true},
		{"2020-01-02T03:04:05.500Z", true},
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
