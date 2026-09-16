package immutable

import "reflect"

// startDetectingCyclesAfter is encoding/json's threshold, and is here for its
// reason: a value nested less deeply than this cannot be cyclic in practice, so
// an ordinary wrap pays a depth counter and nothing else.
const startDetectingCyclesAfter = 1000

// cyclePtr identifies one map or slice by the memory it holds. Two slices can
// share a backing array, so the length is part of the identity.
type cyclePtr struct {
	ptr uintptr
	len int
}

// enterCycle records rv on the walk's path and returns the path to pass down.
// It panics when rv is already on it: the value refers to itself, and the walk
// would otherwise exhaust the stack, which no caller can recover from. Below
// the threshold it allocates nothing.
func enterCycle(rv reflect.Value, depth int, seen map[cyclePtr]struct{}) map[cyclePtr]struct{} {
	if depth < startDetectingCyclesAfter {
		return seen
	}
	p := cyclePtr{ptr: rv.Pointer(), len: rv.Len()}
	if _, ok := seen[p]; ok {
		panic("immutable: cycle detected: a value refers to itself")
	}
	if seen == nil {
		seen = make(map[cyclePtr]struct{}, 1)
	}
	seen[p] = struct{}{}
	return seen
}

// leaveCycle takes rv off the walk's path, so a value reached twice without a
// cycle — a diamond — is wrapped rather than refused.
func leaveCycle(rv reflect.Value, depth int, seen map[cyclePtr]struct{}) {
	if depth < startDetectingCyclesAfter || seen == nil {
		return
	}
	delete(seen, cyclePtr{ptr: rv.Pointer(), len: rv.Len()})
}
