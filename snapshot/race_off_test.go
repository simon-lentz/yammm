//go:build !race

package snapshot_test

// raceEnabled reports whether this test binary was built with the race
// detector. See race_on_test.go.
const raceEnabled = false
