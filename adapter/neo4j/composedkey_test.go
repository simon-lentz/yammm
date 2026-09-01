package neo4j

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestComposedKey(t *testing.T) {
	t.Parallel()
	const root = "car__Vehicle"

	tests := []struct {
		name    string
		rootKey []any
		path    []composedStep
		want    string
		wantErr bool
	}{
		{
			name:    "one cardinality carries no child address",
			rootKey: []any{"ABC123"},
			path:    []composedStep{{relation: "ADDRESS"}},
			want:    `["car__Vehicle",["ABC123"],["ADDRESS"]]`,
		},
		{
			name:    "keyed child",
			rootKey: []any{"ABC123"},
			path:    []composedStep{{relation: "WHEELS", keyOrIndex: []any{"front-left"}}},
			want:    `["car__Vehicle",["ABC123"],["WHEELS",["front-left"]]]`,
		},
		{
			name:    "keyless child is positional",
			rootKey: []any{"ABC123"},
			path:    []composedStep{{relation: "NOTES", keyOrIndex: 0}},
			want:    `["car__Vehicle",["ABC123"],["NOTES",0]]`,
		},
		{
			// FLAT: a second hop appends rather than nesting the first hop's
			// rendering inside itself, so nothing escapes anything.
			name:    "two hops append, they do not nest",
			rootKey: []any{"r1"},
			path: []composedStep{
				{relation: "MID", keyOrIndex: []any{"m1"}},
				{relation: "LEAF", keyOrIndex: []any{"l1"}},
			},
			want: `["car__Vehicle",["r1"],["MID",["m1"]],["LEAF",["l1"]]]`,
		},
		{
			name:    "composite root key",
			rootKey: []any{"a", "b"},
			path:    []composedStep{{relation: "P", keyOrIndex: 1}},
			want:    `["car__Vehicle",["a","b"],["P",1]]`,
		},
		{
			// Restored: the delimiter case a rewrite dropped. Nothing else in
			// this table notices if JSON encoding stops.
			name:    "keys carrying delimiters, quotes and brackets",
			rootKey: []any{`a"b`, `c,d`},
			path:    []composedStep{{relation: "R", keyOrIndex: []any{`["x"]`, `y\z`}}},
			want:    `["car__Vehicle",["a\"b","c,d"],["R",["[\"x\"]","y\\z"]]]`,
		},
		{name: "empty root label", rootKey: []any{"ABC"}, path: []composedStep{{relation: "R"}}, wantErr: true},
		{name: "nil root key", rootKey: nil, path: []composedStep{{relation: "ADDR"}}, wantErr: true},
		{name: "empty root key", rootKey: []any{}, path: []composedStep{{relation: "ADDR"}}, wantErr: true},
		{name: "empty path", rootKey: []any{"ABC"}, path: nil, wantErr: true},
		{name: "empty relation", rootKey: []any{"ABC"}, path: []composedStep{{relation: ""}}, wantErr: true},
		{name: "empty child key slice", rootKey: []any{"ABC"}, path: []composedStep{{relation: "W", keyOrIndex: []any{}}}, wantErr: true},
		{name: "negative index", rootKey: []any{"ABC"}, path: []composedStep{{relation: "N", keyOrIndex: -1}}, wantErr: true},
		{name: "invalid keyOrIndex type", rootKey: []any{"ABC"}, path: []composedStep{{relation: "A", keyOrIndex: "invalid"}}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			label := root
			if tt.name == "empty root label" {
				label = ""
			}
			got, err := composedKey(label, tt.rootKey, tt.path)
			if tt.wantErr {
				if err == nil {
					t.Errorf("composedKey(%q, %v, %v) = %q, want an error", label, tt.rootKey, tt.path, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("composedKey: %v", err)
			}
			if got != tt.want {
				t.Errorf("composedKey = %q, want %q", got, tt.want)
			}
			var round []any
			if err := json.Unmarshal([]byte(got), &round); err != nil {
				t.Errorf("composedKey produced invalid JSON %q: %v", got, err)
			}
		})
	}
}

// TestComposedKey_RootLabelDisambiguates pins why the address leads with the
// root's label: the depth-2 parent MATCH is scoped by the PART label, not by
// the root, so two root types sharing a key value and a relation name would
// otherwise mint one address for children under one part label.
func TestComposedKey_RootLabelDisambiguates(t *testing.T) {
	t.Parallel()
	path := []composedStep{{relation: "WHEELS", keyOrIndex: []any{"fl"}}}
	a, err := composedKey("shopA__Vehicle", []any{"v1"}, path)
	if err != nil {
		t.Fatalf("composedKey: %v", err)
	}
	b, err := composedKey("shopB__Vehicle", []any{"v1"}, path)
	if err != nil {
		t.Fatalf("composedKey: %v", err)
	}
	if a == b {
		t.Errorf("two root labels sharing a key value minted one address: %q", a)
	}
}

// TestComposedKey_CarriesNoSourcePath pins the property a [schema.TypeID] root
// could not have: an address that does not change when the schema file moves,
// and does not put the writing machine's directory layout on every part node.
func TestComposedKey_CarriesNoSourcePath(t *testing.T) {
	t.Parallel()
	got, err := composedKey("shop__Order", []any{"o1"},
		[]composedStep{{relation: "SECTIONS", keyOrIndex: []any{"s1"}}})
	if err != nil {
		t.Fatalf("composedKey: %v", err)
	}
	if strings.Contains(got, "/") {
		t.Errorf("composed key carries a path separator: %q", got)
	}
	if want := `["shop__Order",["o1"],["SECTIONS",["s1"]]]`; got != want {
		t.Errorf("composedKey = %q, want %q", got, want)
	}
}
