package csv

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	jsonadapter "github.com/simon-lentz/yammm/adapter/json"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// A list cell is one string, so a nested collection renders by recursion: an
// inner list renders as a list cell does, and the outer list escapes that text
// as one element. The parser splits and unescapes the same way at every depth,
// so the two are inverses, and every Float renders through the one scalar
// renderer wherever it sits.

const listRenderingSchema = `schema "list_rendering"

type V2 = Vector[2]

type Station {
	code String primary
}

type Grid {
	id String primary
	rows List<List<String>>
	cube List<List<List<Integer>>>
	points List<Vector[2]>
	weight Float
	weights List<Float>
	at Vector[2]
	av V2
	tags List<String>
	n Integer
	counts List<Integer>
	ok Boolean
	flags List<Boolean>
	--> STOPS (_:many) Station {
		w Float
		note String
	}
}
`

func loadListRenderingSchema(t *testing.T) *schema.Schema {
	t.Helper()
	s, res := schema.LoadString(t.Context(), listRenderingSchema, "list_rendering.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res.String())
	}
	return s
}

// csvRoundTrip writes snap through a, parses every type back through a in
// order, validates and rebuilds the graph, failing on any error.
func csvRoundTrip(t *testing.T, a *Adapter, s *schema.Schema, snap *graph.Snapshot, order []string) (map[string][]byte, *graph.Snapshot) {
	t.Helper()
	ctx := context.Background()
	files, err := a.MarshalSnapshot(ctx, snap)
	if err != nil {
		t.Fatalf("MarshalSnapshot: %v", err)
	}
	v := instance.NewValidator(s)
	g := graph.New(s)
	for _, typeName := range order {
		if _, written := files[typeName]; !written {
			continue
		}
		typ, _ := s.Type(typeName)
		raws, pres := a.ParseTyped(ctx, location.NewSourceID(typeName+".csv"), typeName, bytes.NewReader(files[typeName]), typ)
		if pres.HasErrors() {
			t.Fatalf("ParseTyped rejected the writer's own %s output: %s\n%s", typeName, pres.String(), files[typeName])
		}
		for i, raw := range raws {
			vi, vres := v.ValidateOne(ctx, typeName, raw)
			if vres.HasErrors() {
				t.Fatalf("validator rejected re-parsed %s[%d] %v: %s\n%s", typeName, i, raw.Properties, vres.String(), files[typeName])
			}
			if res := g.Add(ctx, vi); !res.OK() {
				t.Fatalf("re-add %s[%d]: %s", typeName, i, res.String())
			}
		}
	}
	if res := g.Check(ctx); !res.OK() {
		t.Fatalf("re-built graph incomplete: %s", res.String())
	}
	return files, g.Snapshot()
}

// assertSameGraph compares two snapshots through the JSON writer, the other
// implementation of the data contract, which carries nested lists on its own
// wire.
func assertSameGraph(t *testing.T, want, got *graph.Snapshot, files map[string][]byte) {
	t.Helper()
	ctx := context.Background()
	w, err := jsonadapter.New().MarshalObject(ctx, want)
	if err != nil {
		t.Fatalf("MarshalObject(original): %v", err)
	}
	g, err := jsonadapter.New().MarshalObject(ctx, got)
	if err != nil {
		t.Fatalf("MarshalObject(round trip): %v", err)
	}
	if !bytes.Equal(w, g) {
		t.Fatalf("the CSV round trip changed the graph\nbefore: %s\nafter:  %s\nCSV:\n%s", w, g, files)
	}
}

func TestListRendering_NestedCollectionsSurviveTheRoundTrip(t *testing.T) {
	t.Parallel()
	s := loadListRenderingSchema(t)
	snap := emptyCellSnapshot(t, s, []typedRow{
		{"Station", map[string]any{"code": "sA"}},
		{"Station", map[string]any{"code": `s|\B`}},
		{"Grid", map[string]any{
			"id":     "g1",
			"rows":   []any{[]any{"a", "b"}, []any{"c"}, []any{}, []any{"p|q", `back\slash`, ""}},
			"cube":   []any{[]any{[]any{int64(1), int64(2)}, []any{}}, []any{[]any{int64(3)}}},
			"points": []any{[]any{1.5, -2.0}, []any{0.25, 1e21}},
			"tags":   []any{"x|y", ""},
			"stops": []any{
				map[string]any{"_target_code": "sA", "w": 1e-7},
				map[string]any{"_target_code": `s|\B`, "w": 2.5},
			},
		}},
		{"Grid", map[string]any{"id": "g2", "rows": []any{[]any{}, []any{"only"}}}},
		{"Grid", map[string]any{"id": "g3", "rows": []any{[]any{}, []any{}}, "tags": []any{"", ""}}},
	})

	files, again := csvRoundTrip(t, New(WithSchema(s)), s, snap, []string{"Station", "Grid"})
	assertSameGraph(t, snap, again, files)

	second, err := New(WithSchema(s)).MarshalSnapshot(context.Background(), again)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if !bytes.Equal(files["Grid"], second["Grid"]) {
		t.Fatalf("round trip is not the identity\nfirst:\n%s\nsecond:\n%s", files["Grid"], second["Grid"])
	}
}

// cellsOf reads a writer's output back with encoding/csv and returns the named
// column's cell in each record.
func cellsOf(t *testing.T, data []byte, column string) []string {
	t.Helper()
	records, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
	if err != nil {
		t.Fatalf("read the writer's output: %v\n%s", err, data)
	}
	col := -1
	for i, name := range records[0] {
		if name == column {
			col = i
		}
	}
	if col < 0 {
		t.Fatalf("no column %q in header %v", column, records[0])
	}
	var cells []string
	for _, r := range records[1:] {
		cells = append(cells, r[col])
	}
	return cells
}

// A scalar has one spelling in a cell, in a list cell, in a Vector cell and in
// an edge segment: the one scalar renderer's. A Float is positional and keeps
// every float64 digit; an Integer is decimal.
func TestListRendering_AScalarHasOneSpellingInEveryCell(t *testing.T) {
	t.Parallel()
	s := loadListRenderingSchema(t)
	big, small, fine := 1e21, 1e-7, 0.30000000000000004
	spell := func(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
	snap := emptyCellSnapshot(t, s, []typedRow{
		{"Station", map[string]any{"code": "sA"}},
		{"Grid", map[string]any{
			"id":      "g1",
			"weight":  big,
			"weights": []any{big, small, 0.5, fine},
			"at":      []any{big, small},
			"points":  []any{[]any{big, small}},
			"n":       int64(255),
			"counts":  []any{int64(255), int64(-7)},
			"ok":      true,
			"flags":   []any{true, false},
			"stops":   []any{map[string]any{"_target_code": "sA", "w": big}},
		}},
	})

	files, err := New(WithSchema(s)).MarshalSnapshot(context.Background(), snap)
	if err != nil {
		t.Fatalf("MarshalSnapshot: %v", err)
	}
	grid := files["Grid"]
	for column, want := range map[string]string{
		"weight":  spell(big),
		"weights": spell(big) + "|" + spell(small) + "|0.5|0.30000000000000004",
		"at":      spell(big) + "|" + spell(small),
		"points":  spell(big) + `\|` + spell(small),
		"n":       "255",
		"counts":  "255|-7",
		"ok":      "true",
		"flags":   "true|false",
		"stops.w": spell(big),
	} {
		if got := cellsOf(t, grid, column)[0]; got != want {
			t.Errorf("column %q: got %q, want %q", column, got, want)
		}
	}
}

// A float32 reaches the writer only through a bypass-built instance, which
// nothing validates. A Vector's elements render through the Float constraint,
// so it is widened to float64 as a Float and a List<Float> element are: in a
// Vector, an aliased Vector, a nested list and an edge property alike.
func TestListRendering_ABypassBuiltFloat32RendersAsAFloatDoes(t *testing.T) {
	t.Parallel()
	s := loadListRenderingSchema(t)
	grid, ok := s.Type("Grid")
	if !ok {
		t.Fatal("Grid missing from the fixture schema")
	}
	station, ok := s.Type("Station")
	if !ok {
		t.Fatal("Station missing from the fixture schema")
	}
	g := graph.New(s)
	if res := g.Add(context.Background(), instancetest.VI("Station", instancetest.TypeID(station.ID()), instancetest.PK("sA"),
		instancetest.Props(map[string]any{"code": "sA"}))); res.HasErrors() {
		t.Fatalf("add Station: %s", res)
	}
	vi := instancetest.VI("Grid", instancetest.TypeID(grid.ID()), instancetest.PK("g1"),
		instancetest.Props(map[string]any{
			"id":      "g1",
			"weight":  float32(0.1),
			"weights": []any{float32(0.1)},
			"at":      []any{float32(1e7), float32(0.1)},
			"av":      []any{float32(1e7), float32(0.1)},
			"points":  []any{[]any{float32(1e7), float32(1e-7)}},
		}),
		instancetest.Edges(map[string]*instance.ValidEdgeData{
			"STOPS": instance.NewValidEdgeData([]instance.ValidEdgeTarget{
				instance.NewValidEdgeTarget(immutable.WrapKey([]any{"sA"}), immutable.WrapProperties(map[string]any{"w": float32(0.1)})),
			}),
		}))
	if res := g.Add(context.Background(), vi); res.HasErrors() {
		t.Fatalf("add: %s", res)
	}
	files, err := New().MarshalSnapshot(context.Background(), g.Snapshot())
	if err != nil {
		t.Fatalf("MarshalSnapshot: %v", err)
	}
	wide := func(f float32) string { return strconv.FormatFloat(float64(f), 'f', -1, 64) }
	for column, want := range map[string]string{
		"weight":  wide(0.1),
		"weights": wide(0.1),
		"at":      wide(1e7) + "|" + wide(0.1),
		"av":      wide(1e7) + "|" + wide(0.1),
		"points":  wide(1e7) + `\|` + wide(1e-7),
		"stops.w": wide(0.1),
	} {
		if got := cellsOf(t, files["Grid"], column)[0]; got != want {
			t.Errorf("column %q: got %q, want %q", column, got, want)
		}
	}
}

// A value its constraint cannot render reaches the writer only through a
// bypass-built instance. It is written as it arrived, through Go's default
// format, at the top of a cell and inside a list alike.
func TestListRendering_AValueItsConstraintCannotRenderIsWrittenAsItArrived(t *testing.T) {
	t.Parallel()
	s := loadListRenderingSchema(t)
	grid, ok := s.Type("Grid")
	if !ok {
		t.Fatal("Grid missing from the fixture schema")
	}
	when := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	g := graph.New(s)
	vi := instancetest.VI("Grid", instancetest.TypeID(grid.ID()), instancetest.PK("g1"),
		instancetest.Props(map[string]any{"id": "g1", "tags": []any{when, "b"}}))
	if res := g.Add(context.Background(), vi); res.HasErrors() {
		t.Fatalf("add: %s", res)
	}
	files, err := New().MarshalSnapshot(context.Background(), g.Snapshot())
	if err != nil {
		t.Fatalf("MarshalSnapshot: %v", err)
	}
	if got, want := cellsOf(t, files["Grid"], "tags")[0], fmt.Sprint(when)+"|b"; got != want {
		t.Errorf("tags: got %q, want %q", got, want)
	}
}

// An inner list holding one empty element renders the text an empty inner list
// renders, so it would read back as an empty list. The writer refuses it, as it
// refuses the same list at the top of a cell; an empty inner list among others
// is not ambiguous and is written.
func TestListRendering_AnInnerListOfOneEmptyElementIsRefused(t *testing.T) {
	t.Parallel()
	s := loadListRenderingSchema(t)
	for _, c := range []struct {
		rows  []any
		where string
	}{
		{[]any{[]any{""}, []any{"a"}}, `"rows": list element 0: a list`},
		{[]any{[]any{"a"}, []any{""}}, `"rows": list element 1: a list`},
		{[]any{[]any{}}, `"rows": a list`},
		{[]any{[]any{""}}, `"rows": list element 0: a list`},
	} {
		snap := emptyCellSnapshot(t, s, []typedRow{{"Grid", map[string]any{"id": "g1", "rows": c.rows}}})
		_, err := New(WithSchema(s)).MarshalSnapshot(context.Background(), snap)
		if err == nil || !strings.Contains(err.Error(), "one empty element") || !strings.Contains(err.Error(), `"rows"`) || !strings.Contains(err.Error(), c.where) {
			t.Errorf("rows %v: got %v, want a refusal naming the property and %q", c.rows, err, c.where)
		}
		var sink bytes.Buffer
		err = New(WithSchema(s)).WriteSnapshot(context.Background(), func(string) (io.Writer, error) { return &sink, nil }, snap)
		if err == nil || !strings.Contains(err.Error(), "one empty element") {
			t.Errorf("rows %v: WriteSnapshot: got %v, want the same refusal", c.rows, err)
		}
	}
}

// Under a separator whose end overlaps its start, such as "||", an element that
// ends with the overlapping part — "a|" — would let a separator match begin
// inside the element. Every element survives, in a list cell and in both kinds
// of edge segment, under separators that overlap themselves and ones that do
// not, one of them holding a backslash after its first byte.
func TestListRendering_AMultiByteSeparatorSplitsOnlyBetweenElements(t *testing.T) {
	t.Parallel()
	s := loadListRenderingSchema(t)
	for _, sep := range []string{"||", "|x", "x|", "::", "→", "→→", `|\`, "aba"} {
		snap := emptyCellSnapshot(t, s, []typedRow{
			{"Station", map[string]any{"code": "a|"}},
			{"Station", map[string]any{"code": "|b"}},
			{"Station", map[string]any{"code": "x:"}},
			{"Station", map[string]any{"code": "→"}},
			{"Station", map[string]any{"code": "ab"}},
			{"Grid", map[string]any{
				"id":   "g1",
				"tags": []any{"a|", "b", "|c", "x", ":", "→→", `\`, "ab", "a"},
				"rows": []any{[]any{"a|", "|"}, []any{"x:", ":x"}, []any{"→", "ab"}},
				"stops": []any{
					map[string]any{"_target_code": "a|", "note": ":"},
					map[string]any{"_target_code": "|b", "note": "x:"},
					map[string]any{"_target_code": "x:", "note": "→"},
					map[string]any{"_target_code": "→", "note": "ab"},
					map[string]any{"_target_code": "ab", "note": `\`},
				},
			}},
		})
		a := New(WithSchema(s), WithListSeparator(sep))
		files, again := csvRoundTrip(t, a, s, snap, []string{"Station", "Grid"})
		assertSameGraph(t, snap, again, files)
	}
}

// A separator the parser cannot find again is refused on both sides before a
// byte is read or written: one beginning with the backslash, which the parser
// reads as an escape, and one holding a CR LF, which encoding/csv reads back as
// LF. The parse refusal is an Error.
func TestListRendering_ASeparatorTheParserCannotFindIsRefused(t *testing.T) {
	t.Parallel()
	s := loadListRenderingSchema(t)
	typ, _ := s.Type("Grid")
	snap := emptyCellSnapshot(t, s, []typedRow{{"Grid", map[string]any{"id": "g1", "tags": []any{"a", "b"}}}})
	for _, sep := range []string{`\`, `\|`, "\r\n", ";\r\n"} {
		a := New(WithSchema(s), WithListSeparator(sep))

		if _, err := a.MarshalSnapshot(context.Background(), snap); err == nil || !strings.Contains(err.Error(), "list separator") {
			t.Errorf("%q: MarshalSnapshot: got %v, want a refusal naming the list separator", sep, err)
		}
		requested := false
		err := a.WriteSnapshot(context.Background(), func(string) (io.Writer, error) {
			requested = true
			return io.Discard, nil
		}, snap)
		if err == nil || !strings.Contains(err.Error(), "list separator") || requested {
			t.Errorf("%q: WriteSnapshot: got %v (writer requested: %v), want a refusal before any writer", sep, err, requested)
		}

		input := &countingReader{r: strings.NewReader("id,tags\ng1,a|b\n")}
		raws, res := a.ParseTyped(context.Background(), location.NewSourceID("g.csv"), "Grid", input, typ)
		if len(raws) != 0 || !hasSeverity(res, diag.Error, E_CSV_CONFIG, "list separator") || input.reads != 0 {
			t.Errorf("%q: ParseTyped: got %d instances, %d reads and %s, want none, no read and an Error E_CSV_CONFIG naming the list separator", sep, len(raws), input.reads, res.String())
		}
		byType, res := New(WithSchema(s), WithListSeparator(sep), WithTypeColumn("type")).ParseWithTypeColumn(context.Background(), location.NewSourceID("g.csv"),
			strings.NewReader("type,id,tags\nGrid,g1,a|b\n"), func(string) *schema.Type { return typ })
		if len(byType) != 0 || !hasSeverity(res, diag.Error, E_CSV_CONFIG, "list separator") {
			t.Errorf("%q: ParseWithTypeColumn: got %v and %s, want none and an Error E_CSV_CONFIG naming the list separator", sep, byType, res.String())
		}
	}
}

// countingReader counts the reads a parse makes of its input.
type countingReader struct {
	r     io.Reader
	reads int
}

func (c *countingReader) Read(p []byte) (int, error) {
	c.reads++
	return c.r.Read(p)
}

func hasSeverity(res diag.Result, severity diag.Severity, code diag.Code, mention string) bool {
	for issue := range res.Issues() {
		if issue.Severity() == severity && issue.Code() == code && strings.Contains(issue.Message(), mention) {
			return true
		}
	}
	return false
}

// WithListSeparator ignores the empty separator and keeps "|". An Adapter that
// never went through New holds the empty one, which the splitter cannot use;
// its zero delimiter refuses first, on both sides, before any list is escaped
// or split, which is why no rule refuses the empty separator itself.
func TestListRendering_TheEmptySeparatorIsNeverUsed(t *testing.T) {
	t.Parallel()
	s := loadListRenderingSchema(t)
	typ, _ := s.Type("Grid")
	snap := emptyCellSnapshot(t, s, []typedRow{
		{"Station", map[string]any{"code": "sA"}},
		{"Grid", map[string]any{"id": "g1", "tags": []any{"a", "b"}, "stops": []any{map[string]any{"_target_code": "sA", "note": "n"}}}},
	})

	files, err := New(WithListSeparator("")).MarshalSnapshot(context.Background(), snap)
	if err != nil {
		t.Fatalf("MarshalSnapshot: %v", err)
	}
	if got := cellsOf(t, files["Grid"], "tags")[0]; got != "a|b" {
		t.Errorf("tags: got %q, want the default separator's %q", got, "a|b")
	}

	var zero Adapter
	if _, err := zero.MarshalSnapshot(context.Background(), snap); err == nil || !strings.Contains(err.Error(), "delimiter") {
		t.Errorf("zero Adapter: MarshalSnapshot: got %v, want the delimiter's refusal", err)
	}
	if err := zero.WriteSnapshot(context.Background(), func(string) (io.Writer, error) { return io.Discard, nil }, snap); err == nil || !strings.Contains(err.Error(), "delimiter") {
		t.Errorf("zero Adapter: WriteSnapshot: got %v, want the delimiter's refusal", err)
	}
	raws, res := zero.ParseTyped(context.Background(), location.NewSourceID("g.csv"), "Grid", strings.NewReader("id,tags\ng1,a|b\n"), typ)
	if len(raws) != 0 || !hasSeverity(res, diag.Error, E_CSV_CONFIG, "delimiter") {
		t.Errorf("zero Adapter: ParseTyped: got %d instances and %s, want none and the delimiter's Error", len(raws), res.String())
	}
}

// encoding/csv's reader turns every CR LF into LF, inside a quoted field too, so
// a cell holding one would read back changed. Both writers refuse it, whether
// the value holds it or a separator joins a CR to an LF; a lone CR is written.
func TestListRendering_ACellHoldingACRLFIsRefused(t *testing.T) {
	t.Parallel()
	s := loadListRenderingSchema(t)
	for _, c := range []struct {
		sep   string
		props map[string]any
	}{
		{"|", map[string]any{"id": "g1", "tags": []any{"a\r\nb"}}},
		{"|", map[string]any{"id": "g1\r\n"}},
		{"\n", map[string]any{"id": "g1", "tags": []any{"a\r", "b"}}},
		{"\r", map[string]any{"id": "g1", "tags": []any{"a", "\nb"}}},
	} {
		snap := emptyCellSnapshot(t, s, []typedRow{{"Grid", c.props}})
		a := New(WithSchema(s), WithListSeparator(c.sep))
		if _, err := a.MarshalSnapshot(context.Background(), snap); err == nil || !strings.Contains(err.Error(), "CR LF") {
			t.Errorf("%q %v: MarshalSnapshot: got %v, want a refusal naming the CR LF", c.sep, c.props, err)
		}
		if err := a.WriteSnapshot(context.Background(), func(string) (io.Writer, error) { return io.Discard, nil }, snap); err == nil || !strings.Contains(err.Error(), "CR LF") {
			t.Errorf("%q %v: WriteSnapshot: got %v, want the same refusal", c.sep, c.props, err)
		}
	}

	snap := emptyCellSnapshot(t, s, []typedRow{{"Grid", map[string]any{"id": "g1", "tags": []any{"a\rb", "c\r", "\nd"}}}})
	files, again := csvRoundTrip(t, New(WithSchema(s)), s, snap, []string{"Grid"})
	assertSameGraph(t, snap, again, files)
}

// A cell the writer never produces still reads as the splitter defines it: a
// file written under the whole-match escape, where only a whole separator was
// escaped, reads back as it always did, and a trailing lone backslash is text.
func TestListRendering_HandWrittenCellsReadAsTheSplitterDefines(t *testing.T) {
	t.Parallel()
	s := loadListRenderingSchema(t)
	typ, _ := s.Type("Grid")
	for _, c := range []struct {
		sep, cell string
		want      []any
	}{
		{"||", `a\||b||c`, []any{"a||b", "c"}},
		{"||", `a\|b`, []any{"a|b"}},
		{"|", `a|b\`, []any{"a", `b\`}},
		{"|", `\`, []any{`\`}},
	} {
		in := "id,tags\ng1," + c.cell + "\n"
		raws, res := New(WithSchema(s), WithListSeparator(c.sep)).ParseTyped(context.Background(), location.NewSourceID("g.csv"), "Grid", strings.NewReader(in), typ)
		if res.HasErrors() || len(raws) != 1 {
			t.Fatalf("%q %q: got %d instances and %s", c.sep, c.cell, len(raws), res.String())
		}
		if got := raws[0].Properties["tags"]; !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q %q: got %#v, want %#v", c.sep, c.cell, got, c.want)
		}
	}
}
