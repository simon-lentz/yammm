package graph_test

import (
	"context"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/instance/path"
	"github.com/simon-lentz/yammm/internal/diagtest"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// A cancelled Add returns its cancellation and installs nothing, so a graph a
// cancellation interrupted holds only what earlier calls installed.
func TestAdd_ACancelledContextInstallsNothing(t *testing.T) {
	t.Parallel()
	s := testSchemaWithComposition(t)
	g := graph.New(s)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	inst := instancetest.Valid(t, s, "Parent", map[string]any{"id": "p1", "name": "Parent 1"})
	res := diagtest.Failures(t, g.Add(ctx, inst))
	if !res.HasFatal() || !res.HasCode(diag.E_CONTEXT_CANCELLED) {
		t.Fatalf("a cancelled Add returned %s", res.String())
	}
	if n := len(slices.Collect(g.Snapshot().Instances())); n != 0 {
		t.Errorf("a cancelled Add installed %d instances", n)
	}
}

// TestAdd_DuplicatePK_CarriesTheProvenanceSpan pins the span attachment, which
// is what lets the renderer show the offending record's excerpt.
func TestAdd_DuplicatePK_CarriesTheProvenanceSpan(t *testing.T) {
	t.Parallel()
	s := testSchemaWithComposition(t)
	g := graph.New(s)
	ctx := t.Context()

	span := location.Span{
		Start: location.Position{Line: 7, Column: 3},
		End:   location.Position{Line: 7, Column: 9},
	}
	withSpan := func() instance.ValidInstance {
		return instancetest.ValidRaw(t, s, "Parent", instance.RawInstance{
			Properties: map[string]any{"id": "p1", "name": "Parent 1"},
			Provenance: instance.NewProvenance("people.json", path.Root.Key("people").Index(0), span),
		})
	}
	if err := g.Add(ctx, withSpan()); err != nil {
		t.Fatalf("first add: %s", err)
	}
	res := diagtest.Failures(t, g.Add(ctx, withSpan()))
	if diagtest.Err(res) == nil {
		t.Fatal("the duplicate primary key was accepted")
	}
	for issue := range res.Issues() {
		if issue.Code() != diag.E_DUPLICATE_PK {
			continue
		}
		if issue.Span() != span {
			t.Errorf("E_DUPLICATE_PK span = %v, want %v", issue.Span(), span)
		}
		return
	}
	t.Fatal("no E_DUPLICATE_PK issue")
}

// TestAdd_DuplicatePK_NamesTheEarlierHolder pins that a duplicate names the
// key's earlier holder as a related location when that holder carries a span,
// and names none when it carries no span.
func TestAdd_DuplicatePK_NamesTheEarlierHolder(t *testing.T) {
	t.Parallel()
	s := testSchemaWithComposition(t)
	ctx := t.Context()

	source, err := location.NewSourceID("test://people.json")
	if err != nil {
		t.Fatal(err)
	}
	at := func(line int) location.Span {
		return location.Span{
			Source: source,
			Start:  location.Position{Line: line, Column: 3},
			End:    location.Position{Line: line, Column: 9},
		}
	}
	holder := func(span location.Span) instance.ValidInstance {
		return instancetest.ValidRaw(t, s, "Parent", instance.RawInstance{
			Properties: map[string]any{"id": "p1", "name": "Parent 1"},
			Provenance: instance.NewProvenance("people.json", path.Root.Key("people").Index(0), span),
		})
	}
	for _, tc := range []struct {
		name    string
		earlier location.Span
		want    []diag.RelatedInfo
	}{
		{"an earlier holder with a span", at(3), []diag.RelatedInfo{{Span: at(3), Message: "first declared here"}}},
		{"an earlier holder with no span", location.Span{}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := graph.New(s)
			if err := g.Add(ctx, holder(tc.earlier)); err != nil {
				t.Fatalf("first add: %s", err)
			}
			res := diagtest.Failures(t, g.Add(ctx, holder(at(4))))
			var dups []diag.Issue
			for issue := range res.Issues() {
				if issue.Code() == diag.E_DUPLICATE_PK {
					dups = append(dups, issue)
				}
			}
			if len(dups) != 1 {
				t.Fatalf("the duplicate drew %d E_DUPLICATE_PK issues, want 1: %s", len(dups), res.String())
			}
			if got := dups[0].Related(); !slices.Equal(got, tc.want) {
				t.Errorf("related = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestAdd_ComposedRecordAttestsValues pins that a record with a composed child
// keeps the Values attestation true.
func TestAdd_ComposedRecordAttestsValues(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	const src = `schema "fleet"

type Car {
	vin String primary

	*-> WHEELS (many) Wheel
}

part type Wheel {
	position String required
}
`
	s, err := schema.LoadString(ctx, src, "test://attest_composed.yammm")
	if err != nil {
		t.Fatalf("schema load: %s", err)
	}

	valid := instancetest.Valid(t, s, "Car", map[string]any{
		"vin":    "v1",
		"wheels": []any{map[string]any{"position": "left"}},
	})
	g := graph.New(s)
	if err := g.Add(ctx, valid); err != nil {
		t.Fatalf("add: %s", err)
	}
	if att := g.Snapshot().Attestation(); !att.Values {
		t.Fatal("a fully validated record did not attest Values")
	}
}

// TestDiagnosticDetails_CarryBareValues pins two detail surfaces. A consumer
// matches on details, not wording, so a
// detail must carry a value it can compare: the graph's canonical tag form for
// a type name, and an unquoted name for expected / got.
func TestDiagnosticDetails_CarryBareValues(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	detail := func(res diag.Result, code diag.Code, key string) string {
		t.Helper()
		for issue := range res.Issues() {
			if issue.Code() != code {
				continue
			}
			for _, d := range issue.Details() {
				if d.Key == key {
					return d.Value
				}
			}
		}
		return ""
	}

	// type_name must be the tag form the graph renders, not the name the
	// instance carries: a validator over the imported schema names the type
	// bare, and the importing graph qualifies it.
	main, common := testMultiSchemaSetup(t)
	mislabelled := instancetest.Valid(t, common, "Entity", map[string]any{"id": "e1", "name": "n"})
	g := graph.New(main)
	if err := g.Add(ctx, mislabelled); err != nil {
		t.Errorf("Add: %v", err)
	}
	res := diagtest.Failures(t, g.Add(ctx, mislabelled))
	if got := detail(res, diag.E_DUPLICATE_PK, diag.DetailKeyTypeName); got != "c.Entity" {
		t.Errorf("type_name = %q, want the canonical tag form %q", got, "c.Entity")
	}
}
