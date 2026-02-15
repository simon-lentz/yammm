package graph

import (
	"testing"
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
			name:   "empty values",
			values: []any{},
			want:   `[]`,
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
			got := FormatKey(tt.values...)
			if got != tt.want {
				t.Errorf("FormatKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatKey_PanicOnUnmarshalable(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("FormatKey should panic on unmarshalable value")
		}
	}()

	// Channels cannot be JSON-marshaled
	ch := make(chan int)
	FormatKey(ch)
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
			got, err := FormatComposedKey(tt.parentKeyValues, tt.compositionName, tt.childKeyOrIndex)
			if tt.wantErr {
				if err == nil {
					t.Errorf("FormatComposedKey() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("FormatComposedKey() unexpected error: %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("FormatComposedKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseComposedKey(t *testing.T) {
	tests := []struct {
		name             string
		input            string
		wantParent       []any
		wantComposition  string
		wantChildOrIndex any
		wantErr          bool
	}{
		{
			name:            "one cardinality",
			input:           `[["ABC123"],"ADDRESS"]`,
			wantParent:      []any{"ABC123"},
			wantComposition: "ADDRESS",
		},
		{
			name:             "many with PK",
			input:            `[["ABC123"],"WHEELS",["front-left"]]`,
			wantParent:       []any{"ABC123"},
			wantComposition:  "WHEELS",
			wantChildOrIndex: []any{"front-left"},
		},
		{
			name:             "many without PK",
			input:            `[["ABC123"],"NOTES",0]`,
			wantParent:       []any{"ABC123"},
			wantComposition:  "NOTES",
			wantChildOrIndex: 0,
		},
		{
			name:             "composite parent key",
			input:            `[["us",12345],"GRADES",["MATH-101"]]`,
			wantParent:       []any{"us", float64(12345)}, // JSON numbers are float64
			wantComposition:  "GRADES",
			wantChildOrIndex: []any{"MATH-101"},
		},
		{
			name:    "invalid JSON",
			input:   `not json`,
			wantErr: true,
		},
		{
			name:    "too few elements",
			input:   `[["ABC"]]`,
			wantErr: true,
		},
		{
			name:    "too many elements",
			input:   `[["ABC"],"REL","child","extra"]`,
			wantErr: true,
		},
		{
			name:    "parent not array",
			input:   `["ABC","REL"]`,
			wantErr: true,
		},
		{
			name:    "empty parent",
			input:   `[[],"REL"]`,
			wantErr: true,
		},
		{
			name:    "composition not string",
			input:   `[["ABC"],123]`,
			wantErr: true,
		},
		{
			name:    "empty composition",
			input:   `[["ABC"],""]`,
			wantErr: true,
		},
		{
			name:    "empty child key",
			input:   `[["ABC"],"REL",[]]`,
			wantErr: true,
		},
		{
			name:    "negative index",
			input:   `[["ABC"],"REL",-1]`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent, comp, childOrIdx, err := ParseComposedKey(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseComposedKey() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("ParseComposedKey() unexpected error: %v", err)
				return
			}

			// Check parent
			if len(parent) != len(tt.wantParent) {
				t.Errorf("ParseComposedKey() parent = %v, want %v", parent, tt.wantParent)
			} else {
				for i, v := range parent {
					if v != tt.wantParent[i] {
						t.Errorf("ParseComposedKey() parent[%d] = %v (%T), want %v (%T)",
							i, v, v, tt.wantParent[i], tt.wantParent[i])
					}
				}
			}

			// Check composition
			if comp != tt.wantComposition {
				t.Errorf("ParseComposedKey() composition = %q, want %q", comp, tt.wantComposition)
			}

			// Check childOrIndex
			switch want := tt.wantChildOrIndex.(type) {
			case nil:
				if childOrIdx != nil {
					t.Errorf("ParseComposedKey() childOrIndex = %v, want nil", childOrIdx)
				}
			case int:
				got, ok := childOrIdx.(int)
				if !ok || got != want {
					t.Errorf("ParseComposedKey() childOrIndex = %v, want %v", childOrIdx, want)
				}
			case []any:
				got, ok := childOrIdx.([]any)
				if !ok || len(got) != len(want) {
					t.Errorf("ParseComposedKey() childOrIndex = %v, want %v", childOrIdx, want)
				}
			}
		})
	}
}

func TestFormatComposedKeyRoundTrip(t *testing.T) {
	tests := []struct {
		name            string
		parentKeyValues []any
		compositionName string
		childKeyOrIndex any
	}{
		{
			name:            "one cardinality",
			parentKeyValues: []any{"ABC123"},
			compositionName: "ADDRESS",
			childKeyOrIndex: nil,
		},
		{
			name:            "many with PK",
			parentKeyValues: []any{"ABC123"},
			compositionName: "WHEELS",
			childKeyOrIndex: []any{"front-left"},
		},
		{
			name:            "many without PK",
			parentKeyValues: []any{"ABC123"},
			compositionName: "NOTES",
			childKeyOrIndex: 2,
		},
		{
			name:            "special characters",
			parentKeyValues: []any{`key"with"quotes`},
			compositionName: "ITEMS",
			childKeyOrIndex: []any{`child[0]`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := FormatComposedKey(tt.parentKeyValues, tt.compositionName, tt.childKeyOrIndex)
			if err != nil {
				t.Fatalf("FormatComposedKey() error: %v", err)
			}

			parent, comp, childOrIdx, err := ParseComposedKey(encoded)
			if err != nil {
				t.Fatalf("ParseComposedKey() error: %v", err)
			}

			// Verify composition name
			if comp != tt.compositionName {
				t.Errorf("Round-trip composition = %q, want %q", comp, tt.compositionName)
			}

			// Verify parent (string values should match)
			if len(parent) != len(tt.parentKeyValues) {
				t.Errorf("Round-trip parent length = %d, want %d", len(parent), len(tt.parentKeyValues))
			}

			// Verify childOrIndex
			switch want := tt.childKeyOrIndex.(type) {
			case nil:
				if childOrIdx != nil {
					t.Errorf("Round-trip childOrIndex = %v, want nil", childOrIdx)
				}
			case int:
				got, ok := childOrIdx.(int)
				if !ok || got != want {
					t.Errorf("Round-trip childOrIndex = %v, want %v", childOrIdx, want)
				}
			case []any:
				got, ok := childOrIdx.([]any)
				if !ok {
					t.Errorf("Round-trip childOrIndex type = %T, want []any", childOrIdx)
				}
				if len(got) != len(want) {
					t.Errorf("Round-trip childOrIndex len = %d, want %d", len(got), len(want))
				}
			}
		})
	}
}
