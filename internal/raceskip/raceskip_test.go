package raceskip

import (
	"fmt"
	"runtime/debug"
	"testing"
)

// TestEnabled_MatchesTheBuild holds Enabled to the -race setting the go command
// records in the test binary, which the constant cannot influence.
func TestEnabled_MatchesTheBuild(t *testing.T) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		t.Fatal("the test binary carries no build information")
	}
	race := false
	for _, s := range info.Settings {
		if s.Key == "-race" {
			race = s.Value == "true"
		}
	}
	if Enabled != race {
		t.Errorf("Enabled = %v in a binary built with -race=%v", Enabled, race)
	}
}

type recordingTB struct {
	testing.TB
	skipped string
}

func (r *recordingTB) Helper() {}

func (r *recordingTB) Skip(args ...any) { r.skipped = fmt.Sprint(args...) }

func TestSkip_SkipsExactlyWhenBuiltWithRace(t *testing.T) {
	tb := &recordingTB{TB: t}
	Skip(tb)
	want := ""
	if Enabled {
		want = Reason
	}
	if tb.skipped != want {
		t.Errorf("Skip with Enabled=%v skipped with %q, want %q", Enabled, tb.skipped, want)
	}
}
