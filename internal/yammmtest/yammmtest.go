package yammmtest

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// update gates golden-file regeneration. See the package documentation for
// the per-package invocation requirement.
var update = flag.Bool("update", false, "rewrite testdata/*.golden files with current output")

// Update reports whether the test binary was invoked with -update. Exposed
// so other regeneration mechanisms (e.g. testscript's UpdateScripts) can
// share the one flag.
func Update() bool { return *update }

// Golden compares got against testdata/<name>.golden in the calling
// package's directory, rewriting the file first when -update is set.
// The comparison is byte-exact; a mismatch reports a (-want +got) diff of
// the contents as text.
func Golden(tb testing.TB, name string, got []byte) {
	tb.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			tb.Fatalf("yammmtest.Golden: create %s: %v", filepath.Dir(path), err)
			return
		}
		if err := os.WriteFile(path, got, 0o644); err != nil { //nolint:gosec // golden test fixture, not sensitive
			tb.Fatalf("yammmtest.Golden: write %s: %v", path, err)
			return
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		tb.Fatalf("yammmtest.Golden: read %s (run this package's tests with -update to create it): %v", path, err)
		return
	}
	if d := cmp.Diff(string(want), string(got)); d != "" {
		tb.Errorf("yammmtest.Golden: %s mismatch (-want +got):\n%s", name, d)
	}
}

// GoldenJSON marshals got as indented JSON with a trailing newline and
// compares it against testdata/<name>.golden via [Golden]. Marshaling must
// be deterministic: pass values whose iteration order is already canonical,
// or a test-only projection that emits a stable shape with only the
// behaviorally meaningful fields.
func GoldenJSON(tb testing.TB, name string, got any) {
	tb.Helper()
	b, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		tb.Fatalf("yammmtest.GoldenJSON: marshal %s: %v", name, err)
		return
	}
	Golden(tb, name, append(b, '\n'))
}

// Diff compares want and got with go-cmp and reports any difference as a
// test error. Options are forwarded to cmp.Diff (comparers, transformers,
// cmpopts helpers).
func Diff(tb testing.TB, want, got any, opts ...cmp.Option) {
	tb.Helper()
	if d := cmp.Diff(want, got, opts...); d != "" {
		tb.Errorf("mismatch (-want +got):\n%s", d)
	}
}

// RunConcurrent runs body from n goroutines, iters times each, and waits
// for all of them — the scaffold for concurrent-read-safety tests over
// immutable structures. Assertions belong after it returns (or in
// thread-safe collectors captured by body).
func RunConcurrent(n, iters int, body func()) {
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			for range iters {
				body()
			}
		})
	}
	wg.Wait()
}

// AssertPanics runs fn and reports a test error unless it panicked.
// Returns the recovered value for callers that assert on its content.
func AssertPanics(tb testing.TB, fn func()) (recovered any) {
	tb.Helper()
	defer func() {
		recovered = recover()
		if recovered == nil {
			tb.Errorf("expected panic, got none")
		}
	}()
	fn()
	return nil
}
