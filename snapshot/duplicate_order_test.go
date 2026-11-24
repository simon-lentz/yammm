package snapshot_test

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/instance/path"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

func duplicateOrderSchema(t *testing.T) schema.Schema {
	t.Helper()
	s, err := schema.LoadString(t.Context(), "schema \"dup\"\n\ntype T {\n\tid String primary\n}\n", "dup.yammm")
	if err != nil {
		t.Fatalf("load: %s", err)
	}
	return s
}

// marshalDuplicates adds one root and then a rejected copy of it per provenance,
// in the order given, and returns the document.
func marshalDuplicates(t *testing.T, s schema.Schema, provs []instance.Provenance) []byte {
	t.Helper()
	g := graph.New(s)
	v := instance.NewValidator(s)
	add := func(prov instance.Provenance) {
		t.Helper()
		valid, err := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{"id": "k"}, Provenance: prov})
		if valid.IsZero() {
			t.Fatalf("validate: %s", err)
		}
		if err := g.Add(t.Context(), valid); err != nil && !errors.Is(err, diag.E_DUPLICATE_PK) {
			t.Errorf("Add: %v", err)
		}
	}
	add(instance.NewProvenance("root.json", path.Root, location.Span{}))
	for _, prov := range provs {
		add(prov)
	}
	data, err := snapshot.Marshal(t.Context(), g.Snapshot())
	if err != nil {
		t.Fatalf("marshal: %s", err)
	}
	return data
}

// A document writes a duplicate's source name and path and no span, so those two
// decide its place: arrival order must not reach the bytes.
func TestMarshal_DuplicateOrderIgnoresAddOrder(t *testing.T) {
	t.Parallel()
	s := duplicateOrderSchema(t)
	for name, span := range map[string]func(int) location.Span{
		"no span":  func(int) location.Span { return location.Span{} },
		"one span": func(int) location.Span { return location.Span{Start: location.Position{Line: 1, Column: 1}} },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var provs []instance.Provenance
			for i := range 30 {
				provs = append(provs, instance.NewProvenance("rows.json", path.Root.Index(i), span(i)))
			}
			want := marshalDuplicates(t, s, provs)
			slices.Reverse(provs)
			if got := marshalDuplicates(t, s, provs); !bytes.Equal(got, want) {
				t.Errorf("the document moved with Add's order:\n%s\n---\n%s", want, got)
			}
		})
	}
}

// A load holds no span, so a document's order must be one a load derives again.
func TestMarshal_ALoadKeepsTheBytesOfManyDuplicates(t *testing.T) {
	t.Parallel()
	s := duplicateOrderSchema(t)
	var provs []instance.Provenance
	for i := range 30 {
		provs = append(provs, instance.NewProvenance("rows.json", path.Root.Index(i),
			location.Span{Start: location.Position{Line: i + 1, Column: 1}}))
	}
	want := marshalDuplicates(t, s, provs)
	loaded, err := snapshot.Unmarshal(t.Context(), want, s)
	if err != nil {
		t.Fatalf("load: %s", err)
	}
	got, err := snapshot.Marshal(t.Context(), loaded)
	if err != nil {
		t.Fatalf("marshal: %s", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("a load moved the duplicates:\n%s\n---\n%s", fmt.Sprintf("%.2000s", want), fmt.Sprintf("%.2000s", got))
	}
}
