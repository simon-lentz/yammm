package csv

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/schema"
)

// Both writers refuse a snapshot CSV cannot carry back, and every one of those
// refusals answers one question: the data cannot be written so that this
// adapter's own parser reads it unchanged. A caller matches the class; the
// message names the instance, the column or the association. Message text is
// pinned by the group that wrote each refusal, so this pins the class alone.
func TestWriters_AnUnrepresentableValueIsRefusedAsOneClass(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name string
		snap func(t *testing.T) (graph.Snapshot, schema.Schema)
	}{
		{
			name: "an instance holding a composed child",
			snap: func(t *testing.T) (graph.Snapshot, schema.Schema) {
				t.Helper()
				return compositionSnapshot(t, map[string][]map[string]any{
					"Order": {{"order_id": "o1", "lines": lines("a")}},
				})
			},
		},
		{
			name: "a lone association target rendering every cell empty",
			snap: func(t *testing.T) (graph.Snapshot, schema.Schema) {
				t.Helper()
				s := loadEmptyCellSchema(t)
				return emptyCellSnapshot(t, s, []typedRow{
					{"Target", map[string]any{"key": ""}},
					{"Holder", map[string]any{
						"id": "h1", "note": "n", "tags": []any{"t"},
						"many": []any{map[string]any{"_target_key": "", "label": ""}},
					}},
				}), s
			},
		},
		{
			name: "a cell whose text holds a CR LF",
			snap: func(t *testing.T) (graph.Snapshot, schema.Schema) {
				t.Helper()
				s := loadListRenderingSchema(t)
				return emptyCellSnapshot(t, s, []typedRow{
					{"Grid", map[string]any{"id": "g1", "tags": []any{"a\r\nb"}}},
				}), s
			},
		},
		{
			name: "a list holding one empty element",
			snap: func(t *testing.T) (graph.Snapshot, schema.Schema) {
				t.Helper()
				s := loadListRenderingSchema(t)
				return emptyCellSnapshot(t, s, []typedRow{
					{"Grid", map[string]any{"id": "g1", "tags": []any{""}}},
				}), s
			},
		},
		{
			name: "a null list element",
			snap: func(t *testing.T) (graph.Snapshot, schema.Schema) {
				t.Helper()
				s := loadListRenderingSchema(t)
				return heldGrid(t, s, `["a",null]`), s
			},
		},
		{
			name: "a map value",
			snap: func(t *testing.T) (graph.Snapshot, schema.Schema) {
				t.Helper()
				s := loadListRenderingSchema(t)
				return heldGrid(t, s, `{"k":"v"}`), s
			},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			snap, s := c.snap(t)
			a := New(WithSchema(s))

			_, err := a.MarshalSnapshot(context.Background(), snap)
			if !errors.Is(err, ErrUnrepresentable) {
				t.Errorf("MarshalSnapshot: %v does not match ErrUnrepresentable", err)
			}
		})
	}
}

// Each value a document can hold that a cell cannot carry back is refused under its
// own sentinel and the class, naming its site, at a property and at an edge property.
func TestWriters_ADocumentValueNoCellCarriesIsRefusedUnderItsSentinel(t *testing.T) {
	t.Parallel()
	s := loadUTF8CSVSchema(t)
	for _, c := range []struct {
		name     string
		props    map[string]any
		label    any
		site     string
		sentinel error
	}{
		{"a number no float64 holds", map[string]any{"text": json.Number("1e400")}, "ok", `property "text"`, errUnreadNumber},
		{"the same in a list", map[string]any{"tags": []any{"ok", json.Number("1e400")}}, "ok", `property "tags": list element 1`, errUnreadNumber},
		{"the same on an edge", nil, json.Number("1e400"), `edge property "label"`, errUnreadNumber},
		{"a map", map[string]any{"text": map[string]any{"k": "v"}}, "ok", `property "text": a map`, errNoCellSpelling},
		{"a map in a list", map[string]any{"tags": []any{map[string]any{"k": "v"}}}, "ok", `property "tags": list element 0: a map`, errNoCellSpelling},
		{"a map on an edge", nil, map[string]any{"k": "v"}, `edge property "label"`, errNoCellSpelling},
		{"a list on an edge", nil, []any{"a", "b"}, `edge property "label"`, errNoCellSpelling},
		{"a null list element", map[string]any{"tags": []any{"ok", nil}}, "ok", `property "tags": list element 1`, errNullListElement},
		{"a CR LF in a cell", map[string]any{"text": "a\r\nb"}, "ok", `column "text"`, errCRLF},
		{"a CR LF in a list element", map[string]any{"tags": []any{"a\r\nb"}}, "ok", `column "tags"`, errCRLF},
		{"a list of one empty element", map[string]any{"tags": []any{""}}, "ok", `property "tags"`, errListOfOneEmptyElement},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, err := New(WithSchema(s)).MarshalSnapshot(t.Context(), heldUTF8(t, s, c.props, c.label))
			if !errors.Is(err, ErrUnrepresentable) || !errors.Is(err, c.sentinel) {
				t.Fatalf("MarshalSnapshot: %v does not match ErrUnrepresentable and %v", err, c.sentinel)
			}
			if !strings.Contains(err.Error(), c.site) {
				t.Errorf("the refusal does not name %s: %v", c.site, err)
			}
		})
	}
}

// A delimiter encoding/csv refuses is the adapter's own configuration, not the
// snapshot's data, and the write side says so with the class whose parse-side
// twin is E_CSV_CONFIG.
func TestWriters_ARefusedSettingIsAConfigurationRefusal(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "basic.yammm")
	snap := buildSnapshot(t, s, map[string][]map[string]any{
		"Entity": {{"id": "e1", "name": "Ann"}},
	})

	for _, c := range []struct {
		name string
		opt  Option
	}{
		{"the delimiter a quote", WithDelimiter('"')},
		{"the delimiter NUL", WithDelimiter(0)},
		{"the delimiter CR", WithDelimiter('\r')},
		{"the delimiter LF", WithDelimiter('\n')},
		{"the delimiter U+FFFD", WithDelimiter('\uFFFD')},
		{"the delimiter beyond Unicode", WithDelimiter(0x110000)},
	} {
		_, err := New(WithSchema(s), c.opt).MarshalSnapshot(context.Background(), snap)
		if !errors.Is(err, ErrConfig) {
			t.Errorf("%s: MarshalSnapshot: %v does not match ErrConfig", c.name, err)
		}
		if errors.Is(err, ErrUnrepresentable) {
			t.Errorf("%s: a configuration refusal must not read as a data refusal: %v", c.name, err)
		}

		// A snapshot denoting no type walks no per-type loop, so a check that
		// lived inside one would let a refused setting through here.
		empty := buildSnapshot(t, s, nil)
		if _, err := New(WithSchema(s), c.opt).MarshalSnapshot(context.Background(), empty); !errors.Is(err, ErrConfig) {
			t.Errorf("%s: MarshalSnapshot of an empty snapshot: %v does not match ErrConfig", c.name, err)
		}
	}

	// Where the adapter's setting and the snapshot's data are both at fault the
	// setting is reported: it is knowable before the data is looked at, and
	// fixing it is what lets the data's fault be seen at all.
	composed, cs := compositionSnapshot(t, map[string][]map[string]any{
		"Order": {{"order_id": "o1", "lines": lines("a")}},
	})
	_, err := New(WithSchema(cs), WithDelimiter('"')).MarshalSnapshot(context.Background(), composed)
	if !errors.Is(err, ErrConfig) || errors.Is(err, ErrUnrepresentable) {
		t.Errorf("both faults: %v, want ErrConfig alone", err)
	}
}

// The classes separate what a message cannot: a nil argument and a refusal of
// the data reach a caller through one return value, and only
// the last two say which they are.
func TestWriters_TheRefusalClassesDoNotCoverAZeroSnapshot(t *testing.T) {
	t.Parallel()
	_, nilErr := New().MarshalSnapshot(context.Background(), graph.Snapshot{})
	if !errors.Is(nilErr, ErrZeroSnapshot) {
		t.Errorf("a zero snapshot = %v, want ErrZeroSnapshot", nilErr)
	}
	if errors.Is(nilErr, ErrUnrepresentable) || errors.Is(nilErr, ErrConfig) {
		t.Errorf("a zero snapshot matched a refusal class: %v", nilErr)
	}
}

// heldGrid reads Grid g1 holding tags, as JSON, from a document: the validator
// refuses a null list element and a map under a List<String>.
func heldGrid(t *testing.T, s schema.Schema, tags string) graph.Snapshot {
	t.Helper()
	return heldDocument(t, s, "Grid", `{"key":["g1"],"properties":{"id":"g1","tags":`+tags+`},"provenance":null}`)
}
