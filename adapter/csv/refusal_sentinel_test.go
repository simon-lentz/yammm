package csv

import (
	"context"
	"errors"
	"fmt"
	"io"
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
		snap func(t *testing.T) (*graph.Snapshot, *schema.Schema)
	}{
		{
			name: "an instance holding a composed child",
			snap: func(t *testing.T) (*graph.Snapshot, *schema.Schema) {
				t.Helper()
				return compositionSnapshot(t, map[string][]map[string]any{
					"Order": {{"order_id": "o1", "lines": lines("a")}},
				})
			},
		},
		{
			name: "a lone association target rendering every cell empty",
			snap: func(t *testing.T) (*graph.Snapshot, *schema.Schema) {
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
			snap: func(t *testing.T) (*graph.Snapshot, *schema.Schema) {
				t.Helper()
				s := loadListRenderingSchema(t)
				return emptyCellSnapshot(t, s, []typedRow{
					{"Grid", map[string]any{"id": "g1", "tags": []any{"a\r\nb"}}},
				}), s
			},
		},
		{
			name: "a list holding one empty element",
			snap: func(t *testing.T) (*graph.Snapshot, *schema.Schema) {
				t.Helper()
				s := loadListRenderingSchema(t)
				return emptyCellSnapshot(t, s, []typedRow{
					{"Grid", map[string]any{"id": "g1", "tags": []any{""}}},
				}), s
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
			err = a.WriteSnapshot(context.Background(), func(string) (io.Writer, error) {
				return io.Discard, nil
			}, snap)
			if !errors.Is(err, ErrUnrepresentable) {
				t.Errorf("WriteSnapshot: %v does not match ErrUnrepresentable", err)
			}
		})
	}
}

// A separator the parser cannot find again is the adapter's own configuration,
// not the snapshot's data, and the write side says so with the class whose
// parse-side twin is E_CSV_CONFIG.
func TestWriters_ARefusedSettingIsAConfigurationRefusal(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "basic.yammm")
	snap := buildSnapshot(t, s, map[string][]map[string]any{
		"Entity": {{"id": "e1", "name": "Ann"}},
	})

	// Every setting the adapter cannot use is refused before a writer is
	// requested: a caller that creates a file per type must not be left with an
	// empty one for a fault that was knowable before any output.
	for _, c := range []struct {
		name string
		opt  Option
	}{
		{`the separator \|`, WithListSeparator(`\|`)},
		{"the separator a CR LF b", WithListSeparator("a\r\nb")},
		{"the delimiter a quote", WithDelimiter('"')},
		{"the delimiter NUL", WithDelimiter(0)},
		{"the delimiter CR", WithDelimiter('\r')},
		{"the delimiter LF", WithDelimiter('\n')},
		{"the delimiter U+FFFD", WithDelimiter('\uFFFD')},
		{"the delimiter beyond Unicode", WithDelimiter(0x110000)},
	} {
		requested := 0
		err := New(WithSchema(s), c.opt).WriteSnapshot(context.Background(), func(string) (io.Writer, error) {
			requested++
			return io.Discard, nil
		}, snap)
		if !errors.Is(err, ErrConfig) {
			t.Errorf("%s: WriteSnapshot: %v does not match ErrConfig", c.name, err)
		}
		if requested != 0 {
			t.Errorf("%s: %d writers requested before the refusal", c.name, requested)
		}
		if _, err := New(WithSchema(s), c.opt).MarshalSnapshot(context.Background(), snap); !errors.Is(err, ErrConfig) {
			t.Errorf("%s: MarshalSnapshot: %v does not match ErrConfig", c.name, err)
		}

		// A snapshot denoting no type walks no per-type loop, so a check that
		// lived inside one would let a refused setting through here.
		empty := buildSnapshot(t, s, nil)
		if _, err := New(WithSchema(s), c.opt).MarshalSnapshot(context.Background(), empty); !errors.Is(err, ErrConfig) {
			t.Errorf("%s: MarshalSnapshot of an empty snapshot: %v does not match ErrConfig", c.name, err)
		}
		err = New(WithSchema(s), c.opt).WriteSnapshot(context.Background(), func(string) (io.Writer, error) {
			return io.Discard, nil
		}, empty)
		if !errors.Is(err, ErrConfig) {
			t.Errorf("%s: WriteSnapshot of an empty snapshot: %v does not match ErrConfig", c.name, err)
		}
	}

	// Where the adapter's setting and the snapshot's data are both at fault the
	// setting is reported: it is knowable before the data is looked at, and
	// fixing it is what lets the data's fault be seen at all.
	composed, cs := compositionSnapshot(t, map[string][]map[string]any{
		"Order": {{"order_id": "o1", "lines": lines("a")}},
	})
	_, err := New(WithSchema(cs), WithListSeparator(`\|`)).MarshalSnapshot(context.Background(), composed)
	if !errors.Is(err, ErrConfig) || errors.Is(err, ErrUnrepresentable) {
		t.Errorf("both faults: %v, want ErrConfig alone", err)
	}

	for _, sep := range []string{`\|`, "a\r\nb"} {
		a := New(WithSchema(s), WithListSeparator(sep))

		_, err := a.MarshalSnapshot(context.Background(), snap)
		if !errors.Is(err, ErrConfig) {
			t.Errorf("%q: MarshalSnapshot: %v does not match ErrConfig", sep, err)
		}
		if errors.Is(err, ErrUnrepresentable) {
			t.Errorf("%q: a configuration refusal must not read as a data refusal: %v", sep, err)
		}
		err = a.WriteSnapshot(context.Background(), func(string) (io.Writer, error) {
			return io.Discard, nil
		}, snap)
		if !errors.Is(err, ErrConfig) {
			t.Errorf("%q: WriteSnapshot: %v does not match ErrConfig", sep, err)
		}
	}
}

// The classes separate what a message cannot: an I/O failure, a nil argument
// and a refusal of the data reach a caller through one return value, and only
// the last two say which they are.
func TestWriters_TheRefusalClassesDoNotCoverIOOrANilSnapshot(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "basic.yammm")

	// All THREE of the writer's I/O checks, because csv.Writer buffers through
	// bufio and which one reports depends on how much text the call pushes past
	// the buffer: a narrow type reports at the flush alone and would leave the
	// header and row checks unguarded.
	var wide strings.Builder
	wide.WriteString("schema \"wide\"\n\ntype W {\n\tidentifier String primary\n")
	for i := range 320 {
		fmt.Fprintf(&wide, "\tcolumn_number_%04d String\n", i)
	}
	wide.WriteString("}\n")
	wideSchema, res := schema.LoadString(context.Background(), wide.String(), "wide.yammm")
	if res.HasErrors() {
		t.Fatalf("load wide schema: %s", res)
	}

	many := make([]map[string]any, 0, 200)
	for i := range 200 {
		many = append(many, map[string]any{"id": fmt.Sprintf("e%03d", i), "name": strings.Repeat("x", 40)})
	}

	for _, c := range []struct {
		where  string
		schema *schema.Schema
		rows   map[string][]map[string]any
	}{
		{"csv flush", s, map[string][]map[string]any{"Entity": {{"id": "e1", "name": "Alice"}}}},
		{"csv write header", wideSchema, map[string][]map[string]any{"W": {{"identifier": "w1"}}}},
		{"csv write row", s, map[string][]map[string]any{"Entity": many}},
	} {
		_, ioErr := writeToFailingWriter(t, c.schema, c.rows)
		if !strings.Contains(ioErr.Error(), c.where) {
			t.Fatalf("the fixture did not reach %s, so it guards nothing: %v", c.where, ioErr)
		}
		if errors.Is(ioErr, ErrUnrepresentable) || errors.Is(ioErr, ErrConfig) {
			t.Errorf("%s: an I/O failure matched a refusal class: %v", c.where, ioErr)
		}
	}

	nilErr := New().WriteSnapshot(context.Background(), func(string) (io.Writer, error) {
		return io.Discard, nil
	}, nil)
	if !errors.Is(nilErr, ErrNilSnapshot) {
		t.Errorf("a nil snapshot = %v, want ErrNilSnapshot", nilErr)
	}
	if errors.Is(nilErr, ErrUnrepresentable) || errors.Is(nilErr, ErrConfig) {
		t.Errorf("a nil snapshot matched a refusal class: %v", nilErr)
	}
}
