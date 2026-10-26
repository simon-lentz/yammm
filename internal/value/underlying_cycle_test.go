package value

import (
	"reflect"
	"testing"
)

type selfPointer *selfPointer

// A pointer that points to itself has no value at its end, so the readers
// that look through pointers answer that it holds none, rather than follow it
// forever.
func TestGetString_APointerThatPointsToItselfHoldsNoValue(t *testing.T) {
	t.Parallel()
	var p selfPointer
	p = &p
	if s, ok := GetString(p); ok {
		t.Errorf("GetString read %q through a pointer that points to itself", s)
	}
	if _, ok := Scalar(p); ok {
		t.Error("Scalar found a value at the end of a pointer that points to itself")
	}
}

// Every pointer walk of the value layer stops at the same bound: a pointer that
// points to itself classifies as no kind, takes no strata and orders against
// nothing, rather than hang.
func TestWalk_EveryWalkStopsAtAPointerThatPointsToItself(t *testing.T) {
	t.Parallel()
	var p selfPointer
	p = &p
	if kind, _ := Classify(p); kind != UnspecifiedKind {
		t.Errorf("Classify = %v, want UnspecifiedKind", kind)
	}
	if s := TypeStrata(p); s != InvalidStrata {
		t.Errorf("TypeStrata = %v, want InvalidStrata", s)
	}
	if _, err := Order(p, p); err == nil {
		t.Error("Order compared two pointers that point to themselves")
	}
	if _, ok := walk(p); ok {
		t.Error("walk followed a pointer that points to itself")
	}
	n := 1
	chain := reflect.ValueOf(&n)
	for range maxPointerDepth - 1 {
		next := reflect.New(chain.Type())
		next.Elem().Set(chain)
		chain = next
	}
	if d, ok := walk(chain.Interface()); !ok || d.Interface() != 1 {
		t.Errorf("walk through %d pointers = %v, %v; want 1", maxPointerDepth, d, ok)
	}
	past := reflect.New(chain.Type())
	past.Elem().Set(chain)
	if _, ok := walk(past.Interface()); ok {
		t.Errorf("walk followed %d pointers, past its bound", maxPointerDepth+1)
	}
}
