package immutable

import "reflect"

// startDetectingCyclesAfter is encoding/json's threshold, and is here for its
// reason: a value nested less deeply than this cannot be cyclic in practice, so
// an ordinary wrap pays a depth counter and nothing else.
const startDetectingCyclesAfter = 1000

// cycleGuard is one walk's state: how deep it is, and past
// [startDetectingCyclesAfter] the maps and slices on the path it is walking.
type cycleGuard struct {
	depth int
	seen  map[cyclePtr]struct{}
}

// cyclePtr identifies one map or slice by the memory it holds. Two slices can
// share a backing array, so the length is part of the identity.
type cyclePtr struct {
	ptr uintptr
	len int
}

// push records rv on the walk's path and returns the state to restore when it
// leaves. It panics when rv is already on the path: the value refers to itself,
// and every walk here would otherwise recurse until the stack is gone — which
// is a fatal runtime error a caller cannot recover from, where this panic is.
func (g *cycleGuard) push(rv reflect.Value) func() {
	g.depth++
	if g.depth <= startDetectingCyclesAfter {
		return func() { g.depth-- }
	}

	p := cyclePtr{ptr: rv.Pointer(), len: rv.Len()}
	if _, ok := g.seen[p]; ok {
		panic("immutable: cycle detected: a value refers to itself")
	}
	if g.seen == nil {
		g.seen = make(map[cyclePtr]struct{})
	}
	g.seen[p] = struct{}{}
	return func() {
		g.depth--
		delete(g.seen, p)
	}
}
