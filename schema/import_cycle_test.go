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
