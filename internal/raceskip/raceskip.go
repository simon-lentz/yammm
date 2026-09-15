// Package raceskip lets a test skip under the race detector without leaving the
// suite. A test that calls [Skip] still runs: the test summary recognizes the
// skip by its reason, and scripts/test.sh runs each such test again without
// -race and fails unless it passes. Use it for a test whose measurement the
// detector's instrumentation distorts, such as a performance ratio, never to
// hide a race.
package raceskip

import "testing"

// Reason is the message [Skip] skips with. The test summary recognizes a
// race-detector skip by it.
const Reason = "skipped under the race detector; the suite runs this test again without -race"

// Skip skips tb when the test binary was built with the race detector.
func Skip(tb testing.TB) {
	tb.Helper()
	if Enabled {
		tb.Skip(Reason)
	}
}
