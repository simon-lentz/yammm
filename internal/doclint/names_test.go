package doclint_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/simon-lentz/yammm/internal/doclint"
)

// nameFixture writes a module outside any git work tree, so the gate walks the
// filesystem, and returns its root. Each file's name is the row under test.
func nameFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"go.mod":                                   "module example.com/names\n\ngo 1.26\n",
		"pkg/pkg.go":                               "package pkg\n",
		"pkg/residue_group1_test.go":               "package pkg\n",
		"pkg/tier1_group4_test.go":                 "package pkg\n",
		"pkg/gate_fixes_test.go":                   "package pkg\n",
		"pkg/step5_fixes_test.go":                  "package pkg\n",
		"pkg/roundtrip_p2_test.go":                 "package pkg\n",
		"pkg/slate_rows_test.go":                   "package pkg\n",
		"pkg/testdata/g11_wraps.yammm":             "",
		"pkg/testdata/cases/a01_empty.yammm":       "",
		"pkg/testdata/group_3/input.yammm":         "",
		"pkg/hash_v2_test.go":                      "package pkg\n",
		"pkg/roundtrip_test.go":                    "package pkg\n",
		"pkg/testdata/fuzz/FuzzX/7fa2d0e7b207d272": "",
		"pkg/names_test.go": `package pkg

import "testing"

func TestRoundTripP2_Identity(t *testing.T) {}
func TestCheck_Group3Rows(t *testing.T)     {}
func TestFixPass_Repair(t *testing.T)       {}
func TestGetFold_O1Performance(t *testing.T) {}
func TestWireV3_Table(t *testing.T)          {}
func TestUTF8Int64Keys(t *testing.T)         {}
func TestRoundTrip_Identity(t *testing.T)    {}
func helperGroup3() {}
`,
	}
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestAssertProcessFreeNames_ReportsEveryProcessName(t *testing.T) {
	t.Parallel()
	r := &recorder{}
	paths, tests := doclint.AssertProcessFreeNames(r, nameFixture(t))
	for _, want := range []string{
		`"residue_group1_test.go" carries the process reference "residue"`,
		`"tier1_group4_test.go" carries the process reference "tier1"`,
		`"gate_fixes_test.go" carries the process reference "gate_fixes"`,
		`"step5_fixes_test.go" carries the process reference "step5"`,
		`"roundtrip_p2_test.go" carries the process reference "p2"`,
		`"slate_rows_test.go" carries the process reference "slate"`,
		`"g11_wraps.yammm" carries the process reference "g11"`,
		`"a01_empty.yammm" carries the process reference "a01"`,
		`"group_3" carries the process reference "group_3"`,
		`TestRoundTripP2_Identity carries the process reference "p2"`,
		`TestCheck_Group3Rows carries the process reference "group3"`,
		`TestFixPass_Repair carries the process reference "fix_pass"`,
	} {
		if !r.reports(want) {
			t.Errorf("not reported: %s\ngot: %v", want, r.msgs)
		}
	}
	for _, quiet := range []string{
		"hash_v2_test.go", `"roundtrip_test.go"`, "7fa2d0e7b207d272",
		"TestGetFold_O1Performance", "TestWireV3_Table", "TestUTF8Int64Keys",
		"TestRoundTrip_Identity", "helperGroup3",
	} {
		if r.reports(quiet) {
			t.Errorf("a legitimate name was reported: %s\ngot: %v", quiet, r.msgs)
		}
	}
	if paths != 15 || tests != 7 {
		t.Errorf("read %d paths and %d test functions; the fixture holds 15 and 7", paths, tests)
	}
}

func TestAssertProcessFreeNames_MissingRootIsReported(t *testing.T) {
	t.Parallel()
	r := &recorder{}
	doclint.AssertProcessFreeNames(r, filepath.Join(t.TempDir(), "absent"))
	if len(r.msgs) == 0 {
		t.Error("a root that does not exist was not reported")
	}
}
