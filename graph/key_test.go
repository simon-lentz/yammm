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
	tests := []struct {
		name            string
		parentKeyValues []any
		compositionName string
		childKeyOrIndex any
		want            string
		wantErr         bool
	}{
		{
			name:            "one cardinality",
			parentKeyValues: []any{"ABC123"},
			compositionName: "ADDRESS",
			childKeyOrIndex: nil,
			want:            `[["ABC123"],"ADDRESS"]`,
		},
		{
			name:            "many with PK",
			parentKeyValues: []any{"ABC123"},
			compositionName: "WHEELS",
			childKeyOrIndex: []any{"front-left"},
			want:            `[["ABC123"],"WHEELS",["front-left"]]`,
		},
		{
			name:            "many without PK",
			parentKeyValues: []any{"ABC123"},
			compositionName: "NOTES",
			childKeyOrIndex: 0,
			want:            `[["ABC123"],"NOTES",0]`,
		},
		{
			name:            "composite parent key",
			parentKeyValues: []any{"us", 12345},
			compositionName: "GRADES",
			childKeyOrIndex: []any{"MATH-101"},
			want:            `[["us",12345],"GRADES",["MATH-101"]]`,
		},
		{
			name:            "special characters in key",
			parentKeyValues: []any{"order-123"},
			compositionName: "QUOTES",
			childKeyOrIndex: []any{`He said "hello"`},
			want:            `[["order-123"],"QUOTES",["He said \"hello\""]]`,
		},
		{
			name:            "nil parent",
			parentKeyValues: nil,
			compositionName: "ADDR",
			childKeyOrIndex: nil,
			wantErr:         true,
		},
		{
			name:            "empty parent",
			parentKeyValues: []any{},
			compositionName: "ADDR",
			childKeyOrIndex: nil,
			wantErr:         true,
		},
		{
			name:            "empty composition name",
			parentKeyValues: []any{"ABC"},
			compositionName: "",
			childKeyOrIndex: nil,
			wantErr:         true,
		},
		{
			name:            "empty child key slice",
			parentKeyValues: []any{"ABC"},
			compositionName: "WHEELS",
			childKeyOrIndex: []any{},
			wantErr:         true,
		},
		{
			name:            "negative index",
			parentKeyValues: []any{"ABC"},
			compositionName: "NOTES",
			childKeyOrIndex: -1,
			wantErr:         true,
		},
		{
			name:            "invalid childKeyOrIndex type",
			parentKeyValues: []any{"ABC"},
			compositionName: "ADDR",
			childKeyOrIndex: "invalid",
			wantErr:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := graph.FormatComposedKey(tt.parentKeyValues, tt.compositionName, tt.childKeyOrIndex)
			if tt.wantErr {
				if err == nil {
					t.Errorf("graph.FormatComposedKey() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("graph.FormatComposedKey() unexpected error: %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("graph.FormatComposedKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

// timestampKeySchema carries a Timestamp primary key, which the DSL permits
// alongside String, UUID and Date.
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
