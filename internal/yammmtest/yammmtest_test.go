package yammmtest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// probeTB records helper failures without stopping the test, so the failure
// paths of the helpers under test can themselves be asserted.
type probeTB struct {
	testing.TB
	errored bool
	fataled bool
	msg     string
}

func (p *probeTB) Helper() {}

func (p *probeTB) Errorf(format string, args ...any) {
	p.errored = true
	p.msg = fmt.Sprintf(format, args...)
}

func (p *probeTB) Fatalf(format string, args ...any) {
	p.fataled = true
	p.msg = fmt.Sprintf(format, args...)
}

// withUpdate flips the package -update flag for the duration of the test.
func withUpdate(t *testing.T, v bool) {
	t.Helper()
	old := *update
	*update = v
	t.Cleanup(func() { *update = old })
}

func TestGolden_UpdateCreatesAndCompareMatches(t *testing.T) {
	t.Chdir(t.TempDir())

	withUpdate(t, true)
	Golden(t, "sample", []byte("hello\nworld\n"))

	got, err := os.ReadFile(filepath.Join("testdata", "sample.golden"))
	if err != nil {
		t.Fatalf("golden not written: %v", err)
	}
	if string(got) != "hello\nworld\n" {
		t.Fatalf("golden content = %q", got)
	}

	withUpdate(t, false)
	Golden(t, "sample", []byte("hello\nworld\n")) // must pass against the committed bytes
}

func TestGolden_MismatchReportsDiff(t *testing.T) {
	t.Chdir(t.TempDir())

	withUpdate(t, true)
	Golden(t, "sample", []byte("hello\n"))
	withUpdate(t, false)

	probe := &probeTB{TB: t}
	Golden(probe, "sample", []byte("other\n"))
	if !probe.errored || probe.fataled {
		t.Fatalf("mismatch should t.Errorf (errored=%v fataled=%v)", probe.errored, probe.fataled)
	}
	if !strings.Contains(probe.msg, "-want +got") {
		t.Errorf("failure message should carry the diff orientation, got %q", probe.msg)
	}
}

func TestGolden_MissingFileIsFatalWithHint(t *testing.T) {
	t.Chdir(t.TempDir())

	// Pin the flag: under a real `go test -update` run the update path
	// would create the missing golden instead of fataling, and this probe
	// asserts the non-update behavior.
	withUpdate(t, false)

	probe := &probeTB{TB: t}
	Golden(probe, "absent", []byte("x"))
	if !probe.fataled {
		t.Fatal("missing golden should t.Fatalf")
	}
	if !strings.Contains(probe.msg, "-update") {
		t.Errorf("failure message should hint at -update, got %q", probe.msg)
	}
}

func TestGoldenJSON_RoundTrip(t *testing.T) {
	t.Chdir(t.TempDir())

	type shape struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	v := shape{Name: "a", Count: 2}

	withUpdate(t, true)
	GoldenJSON(t, "shape", v)

	got, err := os.ReadFile(filepath.Join("testdata", "shape.golden"))
	if err != nil {
		t.Fatalf("golden not written: %v", err)
	}
	want := "{\n  \"name\": \"a\",\n  \"count\": 2\n}\n"
	if string(got) != want {
		t.Fatalf("canonical JSON form = %q, want %q", got, want)
	}

	withUpdate(t, false)
	GoldenJSON(t, "shape", v)
}

func TestDiff_PassAndFail(t *testing.T) {
	Diff(t, []int{1, 2}, []int{1, 2}) // equal: must not fail the real t

	probe := &probeTB{TB: t}
	Diff(probe, []int{1, 2}, []int{1, 3})
	if !probe.errored {
		t.Fatal("unequal values should t.Errorf")
	}
	if !strings.Contains(probe.msg, "-want +got") {
		t.Errorf("failure message should carry the diff orientation, got %q", probe.msg)
	}
}

func TestUpdate_ReflectsFlag(t *testing.T) {
	withUpdate(t, true)
	if !Update() {
		t.Error("Update() should be true under -update")
	}
	withUpdate(t, false)
	if Update() {
		t.Error("Update() should be false without -update")
	}
}
