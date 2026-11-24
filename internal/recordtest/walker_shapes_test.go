package recordtest_test

import (
	"iter"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/simon-lentz/yammm/internal/recordtest"
	"github.com/simon-lentz/yammm/internal/yammmtest"
)

type shape struct {
	a, b     []rec
	two      string
	m        map[string]string
	nilSlice []string
	nilMap   map[string]string
	bits     uint
	frac     float64
	on       bool
	deep     *d1
	loop     *cycle
	lock     *sync.Mutex
	fn       func()
	calls    *int
}

type (
	d1    struct{ n *d2 }
	d2    struct{ n *d3 }
	d3    struct{ n *d4 }
	d4    struct{ n *d5 }
	d5    struct{ v string }
	cycle struct{ next *cycle }
	rec   struct{ *shape }
)

func (r rec) IsZero() bool      { return r.shape == nil }
func (r rec) AA() iter.Seq[rec] { return slices.Values(r.a) }
func (r rec) ZZ() iter.Seq[rec] { return slices.Values(r.b) }
func (r rec) Pair() iter.Seq2[string, string] {
	return func(y func(string, string) bool) { y("k", r.two) }
}
func (r rec) Both() (string, string)    { return "x", r.two }
func (r rec) Map() map[string]string    { return r.m }
func (r rec) NilSlice() []string        { return r.nilSlice }
func (r rec) NilMap() map[string]string { return r.nilMap }
func (r rec) Bits() uint                { return r.bits }
func (r rec) Frac() float64             { return r.frac }
func (r rec) On() bool                  { return r.on }
func (r rec) Deep() *d1                 { return r.deep }
func (r rec) Loop() *cycle              { return r.loop }
func (r rec) Lock() *sync.Mutex         { return r.lock }
func (r rec) Fn() func()                { return r.fn }
func (r rec) Effect()                   { *r.calls++ }

func leaf() rec { return rec{&shape{calls: new(int)}} }

func base() rec {
	x, y := leaf(), leaf()
	loop := &cycle{}
	loop.next = loop
	return rec{&shape{
		a: []rec{x, y}, b: []rec{x, y}, two: "t", m: map[string]string{"k": "v"},
		deep: &d1{&d2{&d3{&d4{&d5{"v"}}}}}, loop: loop, lock: &sync.Mutex{}, calls: new(int),
	}}
}

func TestWalk_EveryRenderedDifferenceShows(t *testing.T) {
	for what, change := range map[string]func(r rec){
		"an element past the first":               func(r rec) { r.a[1].two = "other" },
		"an iter.Seq2 value":                      func(r rec) { r.two = "other" },
		"two records swapped in a later iterator": func(r rec) { r.b[0], r.b[1] = r.b[1], r.b[0] },
		"a map value":                             func(r rec) { r.m["k"] = "other" },
		"a map key":                               func(r rec) { r.m["j"] = r.m["k"]; delete(r.m, "k") },
		"a nil slice from an empty one":           func(r rec) { r.nilSlice = []string{} },
		"a nil map from an empty one":             func(r rec) { r.nilMap = map[string]string{} },
		"a uint":                                  func(r rec) { r.bits = 1 },
		"a float":                                 func(r rec) { r.frac = 1 },
		"a bool":                                  func(r rec) { r.on = true },
		"a value five pointers down":              func(r rec) { r.deep.n.n.n.n.v = "other" },
		"a nil pointer":                           func(r rec) { r.deep = nil },
	} {
		a, b := base(), base()
		change(b)
		if slices.Equal(recordtest.Walk(a, recordtest.Options{}), recordtest.Walk(b, recordtest.Options{})) {
			t.Errorf("%s must show", what)
		}
	}
}

func TestWalk_AnAccessorAfterASkipIsCompared(t *testing.T) {
	skip := recordtest.Options{SkipMethod: func(_, method string) bool { return method == "AA" }}
	a, b := base(), base()
	b.on = true
	if slices.Equal(recordtest.Walk(a, skip), recordtest.Walk(b, skip)) {
		t.Error("an accessor sorted after a skipped one must show")
	}
}

func TestWalk_RendersEachAnswerAndCallsNoOtherMethod(t *testing.T) {
	r := base()
	lines := recordtest.Walk(r, recordtest.Options{})
	if slices.Contains(lines, "root.IsZero/0 = false") {
		t.Error("IsZero must not render")
	}
	if *r.calls != 0 {
		t.Errorf("a method with no answer ran %d times", *r.calls)
	}
	wantLine(t, lines, `root.Both/1 = "t"`)
	wantLine(t, lines, `root.Pair/0[0/1] = "t"`)
	yammmtest.Diff(t, []string{"root = invalid"}, recordtest.Walk(nil, recordtest.Options{}))

	r.deep = nil
	wantLine(t, recordtest.Walk(r, recordtest.Options{}), "root.Deep/0 = nil")
}

func TestWalk_ALockRendersByNameSoItsStateDoesNotShow(t *testing.T) {
	r := base()
	unlocked := recordtest.Walk(r, recordtest.Options{})
	r.lock.Lock()
	defer r.lock.Unlock()
	yammmtest.Diff(t, unlocked, recordtest.Walk(r, recordtest.Options{}))
}

type (
	noIsZero   struct{ p *d5 }
	D5         struct{ V string }
	openHandle struct{ *D5 }
	twoField   struct {
		p *d5
		x int
	}
	shapes      struct{ *shapesState }
	shapesState struct {
		plain noIsZero
		open  openHandle
		two   twoField
	}
)

func (openHandle) IsZero() bool           { return false }
func (twoField) IsZero() bool             { return false }
func (s shapes) IsZero() bool             { return s.shapesState == nil }
func (s shapes) Plain() noIsZero          { return s.plain }
func (s shapes) Open() openHandle         { return s.open }
func (s shapes) Two() twoField            { return s.two }
func (s shapes) NilSeq() iter.Seq[string] { return nil }
func (s shapes) Seqs() []iter.Seq[string] { return []iter.Seq[string]{slices.Values([]string{"s"})} }

func (s shapes) Fresh() []rec    { return []rec{{&shape{two: "only here", calls: new(int)}}} }
func (s shapes) NoneHeld() []rec { return nil }

func shapesOf(v string, x int) shapes {
	return shapes{&shapesState{noIsZero{&d5{v}}, openHandle{&D5{v}}, twoField{&d5{"same"}, x}}}
}

func TestWalk_OnlyTheHandleShapeIsAHandle(t *testing.T) {
	a := recordtest.Walk(shapesOf("a", 1), recordtest.Options{})
	if slices.Equal(a, recordtest.Walk(shapesOf("b", 1), recordtest.Options{})) {
		t.Error("a pointer struct with no IsZero or an exported field must render its fields")
	}
	if slices.Equal(a, recordtest.Walk(shapesOf("a", 2), recordtest.Options{})) {
		t.Error("a struct of two fields must render them")
	}
	for _, want := range []string{
		"root.NilSeq/0 = nil iterator",
		"root.Seqs/0 = len 1",
		"root.Seqs/0[0] = yields 1",
		`root.Fresh/0[0].Both/1 = "only here"`,
		"root.NoneHeld/0 = nil slice",
	} {
		wantLine(t, a, want)
	}
}

func TestWalk_AnExportedPointerFieldWithIsZeroRendersItsFields(t *testing.T) {
	a := recordtest.Walk(openHandle{&D5{"a"}}, recordtest.Options{})
	if slices.Equal(a, recordtest.Walk(openHandle{&D5{"b"}}, recordtest.Options{})) {
		t.Errorf("a struct whose one pointer field is exported is no handle, so a difference below it must show: %q", a)
	}
}

type (
	holds       struct{ h rec }
	holder      struct{ *holderState }
	holderState struct {
		first, second rec
		walk          bool
	}
)

func (h holder) IsZero() bool      { return h.holderState == nil }
func (h holder) ABelow() holds     { return holds{h.first} }
func (h holder) BBelow() holds     { return holds{h.second} }
func (h holder) CBelowZero() holds { return holds{} }
func (h holder) DWalked() []rec {
	if !h.walk {
		return nil
	}
	return []rec{h.first, h.second}
}

func named(name string) rec { return rec{&shape{two: name, calls: new(int)}} }

func TestWalk_ARecordFirstMetBelowAValueIsWalkedOnItsWalkedPath(t *testing.T) {
	x, y := named("x"), named("y")
	lines := recordtest.Walk(holder{&holderState{first: x, second: y, walk: true}}, recordtest.Options{})
	wantLine(t, lines, `root.DWalked/0[0].Both/1 = "x"`)
	wantLine(t, lines, `root.DWalked/0[1].Both/1 = "y"`)
	wantLine(t, lines, "root.CBelowZero/0 = recordtest_test.holds{h:zero}")

	shared := recordtest.Walk(holder{&holderState{first: x, second: x, walk: true}}, recordtest.Options{})
	if slices.Equal(lines, shared) {
		t.Error("two records below values must show as two")
	}
}

func TestWalk_ARecordReachedOnlyBelowAValueFails(t *testing.T) {
	yammmtest.AssertPanics(t, func() {
		recordtest.Walk(holder{&holderState{first: named("x"), second: named("y")}}, recordtest.Options{})
	})
}

type (
	mapped      struct{ *mappedState }
	mappedState struct {
		byName map[string]holds
		kids   []rec
	}
	byRecord      struct{ *byRecordState }
	byRecordState struct{ m map[rec]string }
)

func (m mapped) IsZero() bool           { return m.mappedState == nil }
func (m mapped) AMap() map[string]holds { return m.byName }
func (m mapped) BKids() []rec           { return m.kids }
func (b byRecord) IsZero() bool         { return b.byRecordState == nil }
func (b byRecord) Map() map[rec]string  { return b.m }

func TestWalk_AMapThatHoldsRecordsRendersOneWay(t *testing.T) {
	kids := []rec{named("a"), named("b"), named("c"), named("d")}
	m := mapped{&mappedState{
		byName: map[string]holds{"a": {kids[0]}, "b": {kids[1]}, "c": {kids[2]}, "d": {kids[3]}},
		kids:   kids,
	}}
	first := recordtest.Walk(m, recordtest.Options{})
	for range 50 {
		yammmtest.Diff(t, first, recordtest.Walk(m, recordtest.Options{}))
	}
}

func TestWalk_AMapKeyedByRecordsItCannotOrderFails(t *testing.T) {
	m := byRecord{&byRecordState{m: map[rec]string{named("a"): "1", named("b"): "2"}}}
	yammmtest.AssertPanics(t, func() { recordtest.Walk(m, recordtest.Options{}) })
}

type (
	mapBeforeKids      struct{ *mapBeforeKidsState }
	mapBeforeKidsState struct {
		m    map[rec]string
		kids []rec
	}
)

func (b mapBeforeKids) IsZero() bool         { return b.mapBeforeKidsState == nil }
func (b mapBeforeKids) AMap() map[rec]string { return b.m }
func (b mapBeforeKids) Kids() []rec          { return b.kids }

func TestWalk_AMapKeyedByRecordsAnAccessorWalksLaterFails(t *testing.T) {
	a, b := named("a"), named("b")
	m := mapBeforeKids{&mapBeforeKidsState{m: map[rec]string{a: "1", b: "2"}, kids: []rec{a, b}}}
	got := yammmtest.AssertPanics(t, func() { recordtest.Walk(m, recordtest.Options{}) })
	if text, ok := got.(string); !ok || !strings.Contains(text, "order is not fixed") {
		t.Errorf("Walk panicked with %v, want the map's order named", got)
	}
}

type (
	otherView     struct{ *shape }
	twoViews      struct{ *twoViewsState }
	twoViewsState struct{ state *shape }
)

func (o otherView) IsZero() bool { return o.shape == nil }
func (o otherView) Text() string { return o.two }
func (v twoViews) IsZero() bool  { return v.twoViewsState == nil }
func (v twoViews) A() rec        { return rec{v.state} }
func (v twoViews) B() otherView  { return otherView{v.state} }

func TestWalk_TwoHandleTypesOverOneStateAreTwoRecords(t *testing.T) {
	lines := recordtest.Walk(twoViews{&twoViewsState{state: named("x").shape}}, recordtest.Options{})
	wantLine(t, lines, `root.B/0.Text/0 = "x"`)
}

type (
	minted      struct{ *mintedState }
	mintedState struct{ count *int }
)

func (m minted) IsZero() bool { return m.mintedState == nil }
func (m minted) Next() rec {
	*m.count++
	runtime.GC()
	return named("minted")
}

func (m minted) Other() rec {
	runtime.GC()
	return named("other")
}

func TestWalk_ARecordMadePerCallTakesItsOwnOrdinal(t *testing.T) {
	lines := recordtest.Walk(minted{&mintedState{count: new(int)}}, recordtest.Options{})
	wantLine(t, lines, `root.Next/0.Both/1 = "minted"`)
	wantLine(t, lines, `root.Other/0.Both/1 = "other"`)
}

func TestWalk_OmitTypesHidesAHandleType(t *testing.T) {
	omit := recordtest.Options{OmitTypes: []reflect.Type{reflect.TypeFor[rec]()}}
	yammmtest.Diff(t, []string{"root = recordtest_test.rec"}, recordtest.Walk(named("secret"), omit))
}

func TestWalk_ARawPointerCycleEnds(t *testing.T) {
	lines := recordtest.Walk(base(), recordtest.Options{})
	if !slices.ContainsFunc(lines, func(l string) bool { return len(l) > 12 && l[:12] == "root.Loop/0 " }) {
		t.Error("no line for the cycle")
	}
}

func TestWalk_AnIteratorYieldedByAnIteratorComparesOnEachRange(t *testing.T) {
	calls := 0
	differs := func(yield func(iter.Seq[int]) bool) {
		calls++
		yield(slices.Values([]int{calls}))
	}
	yammmtest.AssertPanics(t, func() { recordtest.Walk(iter.Seq[iter.Seq[int]](differs), recordtest.Options{}) })

	same := func(yield func(iter.Seq[int]) bool) { yield(slices.Values([]int{1})) }
	yammmtest.Diff(t,
		[]string{"root = yields 1", "root[0/0] = yields 1", "root[0/0][0/0] = 1"},
		recordtest.Walk(iter.Seq[iter.Seq[int]](same), recordtest.Options{}))
}

type heldIterator struct{ Each iter.Seq[int] }

func TestWalk_AnIteratorHeldBelowAValueFails(t *testing.T) {
	yammmtest.AssertPanics(t, func() {
		recordtest.Walk(heldIterator{Each: slices.Values([]int{1})}, recordtest.Options{})
	})
	yammmtest.Diff(t,
		[]string{"root = recordtest_test.heldIterator{Each:nil func}"},
		recordtest.Walk(heldIterator{}, recordtest.Options{}))
}

type chain struct{ Next *chain }

func chainOf(n int) *chain {
	var c *chain
	for range n {
		c = &chain{Next: c}
	}
	return c
}

func TestWalk_AValueNestedDeeperThanItRendersFails(t *testing.T) {
	yammmtest.AssertPanics(t, func() { recordtest.Walk(chainOf(40), recordtest.Options{}) })
	if lines := recordtest.Walk(chainOf(4), recordtest.Options{}); len(lines) != 1 {
		t.Errorf("Walk of a short chain = %q, want one line", lines)
	}
}

func TestWalk_ACycleRendersTheDepthItReturnsTo(t *testing.T) {
	loop := &chain{}
	loop.Next = &chain{Next: loop}
	want := []string{"root = *recordtest_test.chain(recordtest_test.chain{Next:*recordtest_test.chain(recordtest_test.chain{Next:cycle to depth 0})})"}
	yammmtest.Diff(t, want, recordtest.Walk(loop, recordtest.Options{}))
}

type heldFunc struct{ F func() int }

type heldChan struct{ C chan int }

func TestWalk_AFuncOrChannelHeldBelowAValueFails(t *testing.T) {
	yammmtest.AssertPanics(t, func() {
		recordtest.Walk(heldFunc{F: func() int { return 1 }}, recordtest.Options{})
	})
	yammmtest.AssertPanics(t, func() {
		recordtest.Walk(heldChan{C: make(chan int)}, recordtest.Options{})
	})
	yammmtest.Diff(t,
		[]string{"root = recordtest_test.heldFunc{F:nil func}"},
		recordtest.Walk(heldFunc{}, recordtest.Options{}))
}

type selfYielding func(func(selfYielding) bool)

func TestWalk_AnIteratorThatYieldsItselfFails(t *testing.T) {
	var self selfYielding
	self = func(yield func(selfYielding) bool) { yield(self) }
	got := yammmtest.AssertPanics(t, func() { recordtest.Walk(self, recordtest.Options{}) })
	if text, ok := got.(string); !ok || !strings.Contains(text, "nested deeper") {
		t.Errorf("Walk panicked with %v, want the nesting named", got)
	}
}

type namedBool bool

func TestWalk_AFuncShapedNearlyLikeAnIteratorIsNoIterator(t *testing.T) {
	for name, notIterator := range map[string]any{
		"a yield that returns a named bool": func(func(int) namedBool) {},
		"a yield that returns no bool":      func(func() int) {},
		"a result beside the yield":         func(func(int) bool) int { return 0 },
	} {
		got := yammmtest.AssertPanics(t, func() { recordtest.Walk(notIterator, recordtest.Options{}) })
		if text, ok := got.(string); !ok || !strings.Contains(text, "held below a value") {
			t.Errorf("%s: Walk panicked with %v, want the func named", name, got)
		}
	}
}
