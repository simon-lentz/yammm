package schema_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

const (
	hostPathDep   = "schema \"reldep\"\n\ntype Zone {\n\tcode String primary\n}\n"
	hostPathEntry = "schema \"relmain\"\n\nimport \"./rel_dep\" as dep\n\ntype Site {\n\tid String primary\n\t--> IN_ZONE (one) dep.Zone\n}\n"
)

// TestHostPath_Loads holds the loader to the host's path rules: a schema is
// read, and its imports resolved, by the name the host gives each file. A
// name the canonical identity spells differently — a backslash on Unix, a
// decomposed character anywhere — must load like any other.
func TestHostPath_Loads(t *testing.T) {
	t.Parallel()

	rows := []struct {
		name     string
		unixOnly bool
		run      func(t *testing.T, tmp string) (ok bool, detail string)
	}{
		{
			name: "Load, plain directory, explicit root",
			run: func(t *testing.T, tmp string) (bool, string) {
				t.Helper()
				dir := filepath.Join(tmp, "plain")
				return loadOutcome(schema.Load(t.Context(), writeHostPathModule(t, dir, "rel_main.yammm"), schema.WithModuleRoot(dir)))
			},
		},
		{
			name: "Load, composed directory, explicit root",
			run: func(t *testing.T, tmp string) (bool, string) {
				t.Helper()
				dir := filepath.Join(tmp, "caf\u00e9")
				return loadOutcome(schema.Load(t.Context(), writeHostPathModule(t, dir, "rel_main.yammm"), schema.WithModuleRoot(dir)))
			},
		},
		{
			name: "Load, decomposed directory, explicit root",
			run: func(t *testing.T, tmp string) (bool, string) {
				t.Helper()
				dir := filepath.Join(tmp, "cafe\u0301")
				return loadOutcome(schema.Load(t.Context(), writeHostPathModule(t, dir, "rel_main.yammm"), schema.WithModuleRoot(dir)))
			},
		},
		{
			name: "Load, decomposed directory, implicit root",
			run: func(t *testing.T, tmp string) (bool, string) {
				t.Helper()
				dir := filepath.Join(tmp, "cafe\u0301")
				return loadOutcome(schema.Load(t.Context(), writeHostPathModule(t, dir, "rel_main.yammm")))
			},
		},
		{
			name: "LoadSourcesWithEntry, composed directory",
			run: func(t *testing.T, tmp string) (bool, string) {
				t.Helper()
				return loadSourcesOutcome(t, filepath.Join(tmp, "caf\u00e9"))
			},
		},
		{
			name: "LoadSourcesWithEntry, decomposed directory",
			run: func(t *testing.T, tmp string) (bool, string) {
				t.Helper()
				return loadSourcesOutcome(t, filepath.Join(tmp, "cafe\u0301"))
			},
		},
		{
			name:     "Load, import under a directory named x\\y",
			unixOnly: true,
			run: func(t *testing.T, tmp string) (bool, string) {
				t.Helper()
				dir := filepath.Join(tmp, `x\y`)
				return loadOutcome(schema.Load(t.Context(), writeHostPathModule(t, dir, "rel_main.yammm"), schema.WithModuleRoot(dir)))
			},
		},
		{
			name:     "Load, entry named m\\main.yammm",
			unixOnly: true,
			run: func(t *testing.T, tmp string) (bool, string) {
				t.Helper()
				dir := filepath.Join(tmp, "e")
				return loadOutcome(schema.Load(t.Context(), writeHostPathModule(t, dir, `m\main.yammm`), schema.WithModuleRoot(dir)))
			},
		},
		{
			name:     "a\\b.yammm and a/b.yammm are two sources",
			unixOnly: true,
			run: func(t *testing.T, tmp string) (bool, string) {
				t.Helper()
				dir := filepath.Join(tmp, "two")
				writeHostPathFile(t, filepath.Join(dir, `a\b.yammm`), "schema \"one\"\n\ntype One {\n\tid String primary\n}\n")
				writeHostPathFile(t, filepath.Join(dir, "a", "b.yammm"), "schema \"two\"\n\ntype Two {\n\tid String primary\n}\n")
				one, resOne := schema.Load(t.Context(), filepath.Join(dir, `a\b.yammm`), schema.WithModuleRoot(dir))
				two, resTwo := schema.Load(t.Context(), filepath.Join(dir, "a", "b.yammm"), schema.WithModuleRoot(dir))
				if one == nil || two == nil {
					return false, fmt.Sprintf("a load failed: %v / %v", issueCodes(resOne), issueCodes(resTwo))
				}
				return one.SourceID() != two.SourceID(), fmt.Sprintf("SourceIDs %q and %q", one.SourceID(), two.SourceID())
			},
		},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			if row.unixOnly && runtime.GOOS == "windows" {
				t.Skip("a backslash is a separator on Windows")
			}
			if ok, detail := row.run(t, t.TempDir()); !ok {
				t.Errorf("the row fails: %s", detail)
			}
		})
	}
}

// writeHostPathModule writes a two-file module into dir — an entry named entry
// that imports "./rel_dep", and the imported file — and returns the entry path.
func writeHostPathModule(t *testing.T, dir, entry string) string {
	t.Helper()
	writeHostPathFile(t, filepath.Join(dir, "rel_dep.yammm"), hostPathDep)
	p := filepath.Join(dir, entry)
	writeHostPathFile(t, p, hostPathEntry)
	return p
}

func writeHostPathFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// loadSourcesOutcome loads the entry from memory, as the editor does, with its
// import left on disk under the module root.
func loadSourcesOutcome(t *testing.T, dir string) (bool, string) {
	t.Helper()
	entry := writeHostPathModule(t, dir, "rel_main.yammm")
	return loadOutcome(schema.LoadSourcesWithEntry(t.Context(),
		map[string][]byte{entry: []byte(hostPathEntry)}, entry, dir))
}

func loadOutcome(s *schema.Schema, res diag.Result) (bool, string) {
	return s != nil && !res.HasErrors(), fmt.Sprintf("schema loaded: %v, issues: %v", s != nil, issueCodes(res))
}

func issueCodes(res diag.Result) []string {
	var codes []string
	for issue := range res.Issues() {
		codes = append(codes, issue.Code().String())
	}
	return codes
}
