//go:build race

package snapshot_test

// raceEnabled reports whether this test binary was built with the race
// detector. Performance-ratio gates skip under it: race instrumentation
// slows the allocation-heavy decode path far more than the byte-splice
// path, so an instrumented ratio neither measures nor gates the documented
// performance claim.
const raceEnabled = true
