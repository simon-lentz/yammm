// Package sourcetest gives a test a synthetic source identity in one call. It is
// apart from internal/yammmtest because location's own tests import that
// package, and an import of location there would close a cycle.
//
// # Dependencies
//
//	internal/yammmtest/sourcetest  ──imports──▶  location
package sourcetest

import "github.com/simon-lentz/yammm/location"

// ID returns the synthetic identity named name, and panics where
// [location.NewSourceID] refuses the name. It is for a test's own fixed names,
// which a refusal would mean the test itself is wrong.
func ID(name string) location.SourceID {
	id, err := location.NewSourceID(name)
	if err != nil {
		panic(err)
	}
	return id
}
