package recordtest_test

import (
	"iter"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/recordtest"
	"github.com/simon-lentz/yammm/internal/yammmtest"
)

type state struct {
	name     string
	children []node
	self     node
	noise    int
}

type node struct{ *state }

func (n node) IsZero() bool               { return n.state == nil }
func (n node) Name() string               { return n.name }
func (n node) Children() iter.Seq[node]   { return slices.Values(n.children) }
func (n node) Self() node                 { return n.self }
func (n node) Noise() int                 { return n.noise }
func (n node) WithArgument(string) string { return "never called" }
func (n node) NoAnswer()                  {}
func (n node) Once() iter.Seq[string]     { return onceOf(n.name) }
func (n node) Stored() *payload           { return &payload{hidden: []string{n.name}} }
func (n node) Handles() []node            { return n.children }

type payload struct{ hidden []string }

func onceOf(s string) iter.Seq[string] {
	return func(yield func(string) bool) { yield(s) }
}

func tree(name string, noise int) node {
	child := node{&state{name: name + "-child", noise: noise}}
	root := node{&state{name: name, children: []node{child}, noise: noise}}
	root.self = root
	return root
}

func TestWalk_EqualRecordsRenderEqualAndADifferenceShows(t *testing.T) {
	a := recordtest.Walk(tree("a", 1), recordtest.Options{})
	yammmtest.Diff(t, a, recordtest.Walk(tree("a", 1), recordtest.Options{}))
	if slices.Equal(a, recordtest.Walk(tree("b", 1), recordtest.Options{})) {
		t.Error("a name below the root must show")
	}
	if slices.Equal(a, recordtest.Walk(tree("a", 2), recordtest.Options{})) {
		t.Error("a number must show")
	}
}

func TestWalk_SkipMethodDropsTheAccessorOnly(t *testing.T) {
	skip := recordtest.Options{SkipMethod: func(owner, method string) bool { return owner == "node" && method == "Noise" }}
	yammmtest.Diff(t, recordtest.Walk(tree("a", 1), skip), recordtest.Walk(tree("a", 2), skip))
	if slices.Equal(recordtest.Walk(tree("a", 1), skip), recordtest.Walk(tree("b", 2), skip)) {
		t.Error("a skip must leave every other accessor compared")
	}
}

func wantLine(t *testing.T, lines []string, want string) {
	t.Helper()
	if !slices.Contains(lines, want) {
		t.Errorf("no line %q in %q", want, lines)
	}
}

func TestWalk_OmitTypesHidesAPayloadAtAnyDepth(t *testing.T) {
	omit := recordtest.Options{OmitTypes: []reflect.Type{reflect.TypeFor[payload]()}}
	for _, l := range recordtest.Walk(tree("a", 1), omit) {
		if strings.Contains(l, "hidden") {
			t.Errorf("an omitted type shows in %q", l)
		}
	}
	wantLine(t, recordtest.Walk(tree("a", 1), recordtest.Options{}), `root.Stored/0 = *recordtest_test.payload(recordtest_test.payload{hidden:["a"]})`)
}

func TestWalk_ASharedRecordRendersAsItsOrdinalSoIdentityCompares(t *testing.T) {
	shared := tree("a", 1)
	lines := recordtest.Walk(shared, recordtest.Options{})
	wantLine(t, lines, "root.Self/0 = record #1 of node")

	apart := tree("a", 1)
	copied := *apart.state
	apart.self = node{&copied}
	if slices.Equal(lines, recordtest.Walk(apart, recordtest.Options{})) {
		t.Error("a copy is not the record")
	}
}

func TestWalk_AnIteratorThatAnswersDifferentlyOnItsSecondRangeFails(t *testing.T) {
	wantLine(t, recordtest.Walk(tree("a", 1), recordtest.Options{}), "root.Children/0 = yields 1")

	var spent bool
	once := func(yield func(string) bool) {
		if !spent {
			spent = true
			yield("x")
		}
	}
	yammmtest.AssertPanics(t, func() { recordtest.Walk(iter.Seq[string](once), recordtest.Options{}) })

	var ranges int
	otherContent := func(yield func(string) bool) {
		ranges++
		yield(strings.Repeat("x", ranges))
	}
	yammmtest.AssertPanics(t, func() { recordtest.Walk(iter.Seq[string](otherContent), recordtest.Options{}) })

	var recordRanges int
	first, second := node{&state{name: "same"}}, node{&state{name: "same"}}
	otherRecord := func(yield func(node) bool) {
		recordRanges++
		if recordRanges == 1 {
			yield(first)
		} else {
			yield(second)
		}
	}
	yammmtest.AssertPanics(t, func() { recordtest.Walk(iter.Seq[node](otherRecord), recordtest.Options{}) })
}

func TestWalk_AZeroHandleAndAWalkedSliceRender(t *testing.T) {
	yammmtest.Diff(t, []string{"root = zero"}, recordtest.Walk(node{}, recordtest.Options{}))
	wantLine(t, recordtest.Walk(tree("a", 1), recordtest.Options{}), "root.Handles/0 = len 1")
}
