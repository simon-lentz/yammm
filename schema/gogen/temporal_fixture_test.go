package gogen_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/schema/gogen"
)

// generatedFixturePath is the compiled copy of the temporal fixture's output,
// the one generated package this module builds, so the CLI reader's round trip
// in cmd/yammm/internal/jsondata can decode into it.
const generatedFixturePath = "../../internal/temporal/temporal_gen.go"

// TestTemporal_GeneratedPackageIsCurrent keeps the compiled fixture equal to
// what Marshal emits today; a drift here means the reader's round trip is
// decoding into stale types.
func TestTemporal_GeneratedPackageIsCurrent(t *testing.T) {
	got, err := gogen.Marshal(loadSchema(t, "temporal"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Clean(generatedFixturePath))
	if err != nil {
		t.Fatalf("read %s (restore it from git: this package does not build without it): %v", generatedFixturePath, err)
	}
	if yammmtest.Update() {
		refuseStaleFixture(t, generatedFixturePath, want, got)
	}
	yammmtest.Diff(t, string(want), string(got))
}

// fatalReporter is the part of testing.TB refuseStaleFixture reports through.
type fatalReporter interface {
	Helper()
	Fatalf(format string, args ...any)
}

// refuseStaleFixture rewrites the compiled fixture at path when got differs
// from want, its content, and then fails the run. The running binary is linked
// against the old copy, so only the next run's round trip compiles the new one.
func refuseStaleFixture(tb fatalReporter, path string, want, got []byte) {
	tb.Helper()
	if bytes.Equal(want, got) {
		return
	}
	if err := os.WriteFile(path, got, 0o600); err != nil {
		tb.Fatalf("rewrite %s: %v", path, err)
		return
	}
	tb.Fatalf("rewrote %s; run the tests again so the round trip compiles it", path)
}

// fatalRecorder records Fatalf calls where a testing.T would stop the test.
type fatalRecorder struct{ fatals []string }

func (r *fatalRecorder) Helper() {}

func (r *fatalRecorder) Fatalf(format string, args ...any) {
	r.fatals = append(r.fatals, fmt.Sprintf(format, args...))
}

func TestRefuseStaleFixture(t *testing.T) {
	t.Parallel()

	t.Run("a current fixture is left alone", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "fixture.go")
		if err := os.WriteFile(path, []byte("current"), 0o600); err != nil {
			t.Fatal(err)
		}
		var rec fatalRecorder
		refuseStaleFixture(&rec, path, []byte("current"), []byte("current"))
		if len(rec.fatals) != 0 {
			t.Errorf("fatals = %q, want none", rec.fatals)
		}
	})

	t.Run("a stale fixture is rewritten and the run fails", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "fixture.go")
		if err := os.WriteFile(path, []byte("stale"), 0o600); err != nil {
			t.Fatal(err)
		}
		var rec fatalRecorder
		refuseStaleFixture(&rec, path, []byte("stale"), []byte("fresh"))
		onDisk, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			t.Fatal(err)
		}
		if string(onDisk) != "fresh" {
			t.Errorf("fixture = %q, want it rewritten to %q", onDisk, "fresh")
		}
		if len(rec.fatals) != 1 || !strings.Contains(rec.fatals[0], "rewrote "+path) {
			t.Errorf("fatals = %q, want one naming the rewrite of %s", rec.fatals, path)
		}
	})

	t.Run("a fixture that cannot be written fails the run", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "absent", "fixture.go")
		var rec fatalRecorder
		refuseStaleFixture(&rec, path, []byte("stale"), []byte("fresh"))
		if len(rec.fatals) != 1 || !strings.Contains(rec.fatals[0], "rewrite "+path) {
			t.Errorf("fatals = %q, want one naming the failed rewrite of %s", rec.fatals, path)
		}
	})
}
