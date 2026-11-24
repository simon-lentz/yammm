// Package diagtest reads the issues a call of the module draws, for tests of
// any package above diag: each warning a call's warnings callback received and
// each failure its *[diag.ResultError] holds, as one [diag.Result]. It imports
// diag alone, so a package's own tests can use it without an import cycle.
package diagtest

import (
	"errors"
	"testing"

	"github.com/simon-lentz/yammm/diag"
)

// Capture runs call, which must pass warn as its call's warnings callback, and
// returns the call's value beside every issue it drew: each warning warn
// received and each failure its error holds, in one Result. An error holding no
// [diag.ResultError], such as a refused option, fails tb.
func Capture[T any](tb testing.TB, call func(warn func(diag.Issue)) (T, error)) (T, diag.Result) {
	tb.Helper()
	issues := diag.NewCollector(diag.NoLimit)
	v, err := call(issues.Collect)
	merge(tb, issues, err)
	return v, issues.Result()
}

// CaptureErr is [Capture] for a call that returns an error alone.
func CaptureErr(tb testing.TB, call func(warn func(diag.Issue)) error) diag.Result {
	tb.Helper()
	issues := diag.NewCollector(diag.NoLimit)
	merge(tb, issues, call(issues.Collect))
	return issues.Result()
}

// Issues runs call, a call that draws no issue below failure severity, and
// returns its value beside the failures its error holds.
func Issues[T any](tb testing.TB, call func() (T, error)) (T, diag.Result) {
	tb.Helper()
	v, err := call()
	return v, Failures(tb, err)
}

// Failures returns the failures err holds as one Result, empty for a nil err.
// An error holding no [diag.ResultError] fails tb.
func Failures(tb testing.TB, err error) diag.Result {
	tb.Helper()
	issues := diag.NewCollector(diag.NoLimit)
	merge(tb, issues, err)
	return issues.Result()
}

// Err returns nil when r saw no Fatal or Error issue, stored or dropped, and
// otherwise the failure [diag.Collector.Err] builds from r: its failures alone.
func Err(r diag.Result) error {
	c := diag.NewCollector(diag.NoLimit)
	c.Merge(r)
	return c.Err()
}

func merge(tb testing.TB, issues *diag.Collector, err error) {
	tb.Helper()
	if re, ok := errors.AsType[*diag.ResultError](err); ok {
		issues.Merge(re.Result)
	} else if err != nil {
		tb.Errorf("diagtest: the call failed with no diagnostics: %v", err)
	}
}
