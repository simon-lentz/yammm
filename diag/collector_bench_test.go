package diag

import "testing"

// BenchmarkCollector_Saturated times the eviction path: a small limit fed a
// severity cycle, so every Collect after the first few is an eviction or a
// rejection. Nothing else in the repository times the collector, and the
// limit path runs on every schema load.
//
// Not a CI gate. Gate 7 proves it runs; the number is for comparing an
// eviction strategy against the one it replaces.
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
