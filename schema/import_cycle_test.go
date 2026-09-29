package schema_test

import (
	"cmp"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// issueSite is a diagnostic reduced to its code and the source line it is on.
type issueSite struct {
	Code   string
	Source string
	Line   int
}

func sortIssueSites(sites []issueSite) {
	slices.SortFunc(sites, func(a, b issueSite) int {
		return cmp.Or(
			cmp.Compare(a.Source, b.Source),
			cmp.Compare(a.Line, b.Line),
			cmp.Compare(a.Code, b.Code),
		)
	})
}

// TestImportCycle_ReportedOnTheClosingImport holds an import cycle to one
// E_IMPORT_CYCLE on the declaration that closes it, and asserts every
// diagnostic the load reports. A cycle through another file adds only that
// file's E_UPSTREAM_FAIL, and a second declaration of a cyclic file adds only
// its E_DUPLICATE_IMPORT.
func TestImportCycle_ReportedOnTheClosingImport(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)

	type site struct {
		code diag.Code
		file string
		line int
	}
	rows := []struct {
		name  string
		files map[string]string
		entry string
		want  []site
	}{
		{
			name: "a two-file cycle is reported on the second file's import",
			files: map[string]string{
				"a.yammm": "schema \"a\"\n\nimport \"./b\" as b\n\ntype A {\n\tid String primary\n\t--> USES (one) b.B\n}\n",
				"b.yammm": "schema \"b\"\n\nimport \"./a\" as a\n\ntype B {\n\tid String primary\n\t--> USES (one) a.A\n}\n",
			},
			entry: "a.yammm",
			want: []site{
				{diag.E_UPSTREAM_FAIL, "a.yammm", 3},
				{diag.E_IMPORT_CYCLE, "b.yammm", 3},
			},
		},
		{
			name: "a self-import is reported on its declaration",
			files: map[string]string{
				"s.yammm": "schema \"s\"\n\nimport \"./s\" as s\n\ntype S {\n\tid String primary\n\t--> USES (one) s.S\n}\n",
			},
			entry: "s.yammm",
			want: []site{
				{diag.E_IMPORT_CYCLE, "s.yammm", 3},
			},
		},
		{
			name: "a second declaration of a cyclic file is a duplicate import",
			files: map[string]string{
				"a.yammm": "schema \"a\"\n\nimport \"./b\" as b\n\ntype A {\n\tid String primary\n\t--> USES (one) b.B\n}\n",
				"b.yammm": "schema \"b\"\n\nimport \"./a\" as a\nimport \"./a.yammm\" as a2\n\ntype B {\n\tid String primary\n\t--> USES (one) a.A\n}\n",
			},
			entry: "a.yammm",
			want: []site{
				{diag.E_UPSTREAM_FAIL, "a.yammm", 3},
				{diag.E_IMPORT_CYCLE, "b.yammm", 3},
				{diag.E_DUPLICATE_IMPORT, "b.yammm", 4},
			},
		},
		{
			name: "a second declaration of a self-import is a duplicate import",
			files: map[string]string{
				"t.yammm": "schema \"t\"\n\nimport \"./t\" as t\nimport \"./t.yammm\" as t2\n\ntype T {\n\tid String primary\n\t--> USES (one) t.T\n}\n",
			},
			entry: "t.yammm",
			want: []site{
				{diag.E_IMPORT_CYCLE, "t.yammm", 3},
				{diag.E_DUPLICATE_IMPORT, "t.yammm", 4},
			},
		},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			for name, content := range row.files {
				writeHostPathFile(t, filepath.Join(root, name), content)
			}

			_, res := schema.Load(t.Context(), filepath.Join(root, row.entry), schema.WithModuleRoot(root))

			fileID := func(name string) location.SourceID {
				id, err := location.SourceIDFromPath(canonicalPath(t, filepath.Join(root, name)))
				if err != nil {
					t.Fatal(err)
				}
				return id
			}
			want := make([]issueSite, 0, len(row.want))
			for _, w := range row.want {
				want = append(want, issueSite{Code: w.code.String(), Source: fileID(w.file).String(), Line: w.line})
			}
			var got []issueSite
			for issue := range res.Issues() {
				got = append(got, issueSite{Code: issue.Code().String(), Source: issue.Span().Source.String(), Line: issue.Span().Start.Line})
				if issue.Code() != diag.E_IMPORT_CYCLE {
					continue
				}
				for name := range row.files {
					if strings.Contains(issue.Message(), name) {
						t.Errorf("E_IMPORT_CYCLE message %q names the source file %s", issue.Message(), name)
					}
				}
			}
			sortIssueSites(want)
			sortIssueSites(got)
			yammmtest.Diff(t, want, got)
		})
	}
}

// TestBuilder_RefusesAnImportThatClosesACycle pins that a built schema whose
// import resolves to its own source ID, or to a registered schema whose
// closure holds that ID, is refused with one E_IMPORT_CYCLE on that import, as
// a load refuses a cycle; an import of another source builds.
func TestBuilder_RefusesAnImportThatClosesACycle(t *testing.T) {
	t.Parallel()

	x, res := schema.LoadSourcesWithEntry(t.Context(), sourcesOf(map[string]string{"x.yammm": xSrc, "geo_a.yammm": geoA}), "x.yammm", ".", schema.WithSourcesOnly(true))
	if res.HasErrors() {
		t.Fatalf("load x: %v", res.Err())
	}
	geo, res := schema.LoadString(t.Context(), geoB, "geo_b.yammm")
	if res.HasErrors() {
		t.Fatalf("load geo: %v", res.Err())
	}
	reg := schema.NewRegistry()
	for _, s := range []*schema.Schema{x, geo} {
		if err := reg.Register(s); err != nil {
			t.Fatal(err)
		}
	}
	var geoAID location.SourceID
	for _, member := range x.Closure() {
		if member.Name() == "geo" {
			geoAID = member.SourceID()
		}
	}
	if geoAID.IsZero() {
		t.Fatal("x's closure holds no geo")
	}
	build := func(id location.SourceID, imp string) (*schema.Schema, diag.Result) {
		b := schema.NewBuilder().WithName("main").WithSourceID(id).WithRegistry(reg).AddImport(imp, "i")
		b.AddType("Local").WithPrimaryKey("id", schema.NewStringConstraint())
		return b.Build()
	}

	for label, tc := range map[string]struct {
		id  location.SourceID
		imp string
	}{
		"an import of the built schema's own source": {geo.SourceID(), "geo"},
		"an import whose closure holds that source":  {geoAID, "x"},
	} {
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			s, res := build(tc.id, tc.imp)
			if s != nil {
				t.Fatal("an import that closes a cycle built")
			}
			issues := slices.Collect(res.Issues())
			if len(issues) != 1 || issues[0].Code() != diag.E_IMPORT_CYCLE || issues[0].Severity() != diag.Error {
				t.Fatalf("result = %v, want one Error E_IMPORT_CYCLE", res.Err())
			}
			for _, want := range []diag.Detail{
				{Key: diag.DetailKeyImportPath, Value: tc.imp},
				{Key: diag.DetailKeyModuleRootOrigin, Value: diag.ModuleRootNone},
			} {
				if !slices.Contains(issues[0].Details(), want) {
					t.Errorf("details = %v, want %v", issues[0].Details(), want)
				}
			}
		})
	}

	t.Run("an import declared twice", func(t *testing.T) {
		t.Parallel()
		b := schema.NewBuilder().WithName("main").WithSourceID(geo.SourceID()).WithRegistry(reg).AddImport("geo", "i0").AddImport("geo", "i1")
		b.AddType("Local").WithPrimaryKey("id", schema.NewStringConstraint())
		_, res := b.Build()
		var codes []string
		for issue := range res.Issues() {
			codes = append(codes, issue.Code().String())
		}
		slices.Sort(codes)
		if want := []string{diag.E_DUPLICATE_IMPORT.String(), diag.E_IMPORT_CYCLE.String()}; !slices.Equal(codes, want) {
			t.Errorf("codes = %v, want %v, as a load reports a cyclic import declared twice", codes, want)
		}
	})

	t.Run("an import whose closure holds that source two imports deep", func(t *testing.T) {
		t.Parallel()
		y, res := schema.LoadSourcesWithEntry(t.Context(), sourcesOf(map[string]string{
			"y.yammm":     "schema \"y\"\n\nimport \"./x.yammm\" as x\n\ntype Yt {\n\ty_id String primary\n\t--> TO (one) x.Xt\n}\n",
			"x.yammm":     xSrc,
			"geo_a.yammm": geoA,
		}), "y.yammm", ".", schema.WithSourcesOnly(true))
		if res.HasErrors() {
			t.Fatalf("load y: %v", res.Err())
		}
		deep := schema.NewRegistry()
		if err := deep.Register(y); err != nil {
			t.Fatal(err)
		}
		b := schema.NewBuilder().WithName("main").WithSourceID(geoAID).WithRegistry(deep).AddImport("y", "i")
		b.AddType("Local").WithPrimaryKey("id", schema.NewStringConstraint())
		if s, res := b.Build(); s != nil || !res.HasErrors() {
			t.Fatalf("an import whose closure holds the built source two imports deep built: %v", res.Err())
		} else if issue := slices.Collect(res.Issues()); len(issue) != 1 || issue[0].Code() != diag.E_IMPORT_CYCLE {
			t.Errorf("result = %v, want one E_IMPORT_CYCLE", res.Err())
		}
	})

	if s, res := build(location.MustNewSourceID("test://built.yammm"), "x"); s == nil || res.HasErrors() {
		t.Errorf("an import of another source is refused: %v", res.Err())
	}
}
