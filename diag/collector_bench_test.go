package diag

import "testing"

// BenchmarkCollector_Saturated times the eviction path: a small limit fed a
// severity cycle, so every Collect after the first few is an eviction or a
// rejection. Nothing else in the repository times the collector, and a schema
// load takes the limit path once it collects more issues than its limit.
//
// scripts/test.sh runs it once, at every full gate and in CI, which proves it
// still runs; no gate reads its number, which is for comparing an eviction
// strategy against the one it replaces.
func BenchmarkCollector_Saturated(b *testing.B) {
	sevs := []Severity{Warning, Error, Warning, Fatal, Error, Warning, Info, Error}
	issues := make([]Issue, len(sevs))
	for i, sev := range sevs {
		issues[i] = NewIssue(sev, E_INTERNAL, "bench").Build()
	}
	b.ReportAllocs()
	for b.Loop() {
		c := NewCollector(8)
		for i := range 64 {
			c.Collect(issues[i%len(issues)])
		}
	}
}
