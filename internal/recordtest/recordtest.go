// Package recordtest renders everything a record handle answers, so a test can
// compare two implementations of one record accessor by accessor.
//
// Its reader for a handle is the method set, not a list: an accessor added to a
// handle is compared without a change here. A value that is not a handle renders
// from its fields, so two implementations must also agree on its representation.
//
// Two walks by one walker lose power together, so a comparison cannot see what the
// walker cannot. [Walk] panics on the inputs it cannot compare: a record it reached
// only below a value, an iterator that answers differently on its second range, a
// func or channel held below a value, a value or iterator nested deeper than it
// renders, and a map whose keys hold records it cannot order.
//
// It is test support and must never be imported by non-test code.
package recordtest

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

const maxDepth = 24

// Options narrows what [Walk] renders.
type Options struct {
	// SkipMethod names an accessor two implementations may answer differently.
	SkipMethod func(owner, method string) bool
	// OmitTypes lists types rendered by name alone, at any depth.
	OmitTypes []reflect.Type
}

// Walk returns one line for each answer of each no-argument accessor reachable
// from v, in method order. A handle renders as the ordinal of its first visit, so
// two walks agree only if the records share the same identities. An iterator
// renders as its elements, and it is ranged twice.
func Walk(v any, opts Options) []string {
	w := &walker{opts: opts, ordinals: map[identity]int{}, walked: map[identity]bool{}, dumping: map[reference]int{}}
	w.walk("root", reflect.ValueOf(v))
	for id := range w.ordinals {
		if !w.walked[id] {
			panic("recordtest: a " + id.typ.String() + " record was reached only below a value, so its accessors were not compared")
		}
	}
	return w.lines
}

// identity names one record. The type is part of it because two handle types can
// name one state and answer differently.
type identity struct {
	typ reflect.Type
	ptr uintptr
}

// dumpMode says how [walker.dump] renders a handle.
type dumpMode int

const (
	assignOrdinals dumpMode = iota // a new record takes the next ordinal
	readOrdinals                   // a new record renders with no ordinal and takes none
	readAddresses                  // a record renders as its address, for a comparison inside one walk
)

type walker struct {
	opts     Options
	ordinals map[identity]int
	walked   map[identity]bool
	// held keeps every visited record reachable, so no address comes back under
	// another record while the walk runs.
	held []reflect.Value
	// dumping maps each reference on the current dump path to its depth, so a cycle
	// renders as the depth it returns to.
	dumping map[reference]int
	mode    dumpMode
	// iterators counts the iterators on the current path.
	iterators int
	lines     []string
}

// reference names what a pointer, map or slice value refers to.
type reference struct {
	typ reflect.Type
	ptr uintptr
	len int
}

// referenceOf returns the reference a value holds, if it holds one a cycle can pass through.
func referenceOf(v reflect.Value) (reference, bool) {
	switch v.Kind() {
	case reflect.Pointer, reflect.Map:
		return reference{typ: v.Type(), ptr: v.Pointer()}, !v.IsNil()
	case reflect.Slice:
		return reference{typ: v.Type(), ptr: v.Pointer(), len: v.Len()}, v.Len() > 0
	default:
		return reference{}, false
	}
}

// isHandle reports the handle shape: a struct of one unexported pointer, with IsZero.
func isHandle(t reflect.Type) bool {
	if t.Kind() != reflect.Struct || t.NumField() != 1 {
		return false
	}
	f := t.Field(0)
	if f.IsExported() || f.Type.Kind() != reflect.Pointer {
		return false
	}
	_, ok := t.MethodByName("IsZero")
	return ok
}

// isIterator reports the shape of iter.Seq and iter.Seq2.
func isIterator(t reflect.Type) bool {
	if t.Kind() != reflect.Func || t.NumIn() != 1 || t.NumOut() != 0 {
		return false
	}
	y := t.In(0)
	return y.Kind() == reflect.Func && y.NumOut() == 1 && y.Out(0) == reflect.TypeFor[bool]()
}

func identityOf(v reflect.Value) identity {
	return identity{typ: v.Type(), ptr: v.Field(0).Pointer()}
}

func (w *walker) ordinal(v reflect.Value) int {
	id := identityOf(v)
	n, ok := w.ordinals[id]
	if !ok {
		n = len(w.ordinals) + 1
		w.ordinals[id] = n
		w.held = append(w.held, v)
	}
	return n
}

func (w *walker) emit(path, text string) {
	w.lines = append(w.lines, path+" = "+text)
}

func (w *walker) walk(path string, v reflect.Value) {
	switch {
	case !v.IsValid():
		w.emit(path, "invalid")
	case slices.Contains(w.opts.OmitTypes, v.Type()):
		w.emit(path, v.Type().String())
	case isHandle(v.Type()):
		w.handle(path, v)
	case isIterator(v.Type()):
		if v.IsNil() {
			w.emit(path, "nil iterator")
			return
		}
		w.iterator(path, v)
	case v.Kind() == reflect.Slice && !v.IsNil() && holdsWalkable(v.Type().Elem()):
		w.emit(path, fmt.Sprintf("len %d", v.Len()))
		for i := range v.Len() {
			w.walk(fmt.Sprintf("%s[%d]", path, i), v.Index(i))
		}
	default:
		w.emit(path, w.dump(v, 0))
	}
}

func holdsWalkable(t reflect.Type) bool {
	return isHandle(t) || isIterator(t)
}

func (w *walker) handle(path string, v reflect.Value) {
	field := v.Field(0)
	if field.IsNil() {
		w.emit(path, "zero")
		return
	}
	w.emit(path, fmt.Sprintf("record #%d of %s", w.ordinal(v), v.Type().Name()))
	id := identityOf(v)
	if w.walked[id] {
		return
	}
	w.walked[id] = true
	t := v.Type()
	for i := range t.NumMethod() {
		m := t.Method(i)
		if m.Name == "IsZero" || m.Type.NumIn() != 1 || m.Type.NumOut() == 0 {
			continue
		}
		if w.opts.SkipMethod != nil && w.opts.SkipMethod(t.Name(), m.Name) {
			continue
		}
		for r, out := range v.Method(i).Call(nil) {
			w.walk(fmt.Sprintf("%s.%s/%d", path, m.Name, r), out)
		}
	}
}

func (w *walker) iterator(path string, v reflect.Value) {
	defer w.enterIterator(v)()
	first, second := rangeOf(v), rangeOf(v)
	if w.addressText(first) != w.addressText(second) {
		panic("recordtest: the iterator at " + path + " answered differently on its second range")
	}
	w.emit(path, fmt.Sprintf("yields %d", len(first)))
	for i, args := range first {
		for j, a := range args {
			w.walk(fmt.Sprintf("%s[%d/%d]", path, i, j), a)
		}
	}
}

// rangeOf returns the answers of one range of an iterator.
func rangeOf(v reflect.Value) [][]reflect.Value {
	var out [][]reflect.Value
	v.Call([]reflect.Value{reflect.MakeFunc(v.Type().In(0), func(args []reflect.Value) []reflect.Value {
		out = append(out, args)
		return []reflect.Value{reflect.ValueOf(true)}
	})})
	return out
}

// addressText renders one range of an iterator with each record as its address, so
// two ranges compare by identity and the comparison assigns no ordinal.
func (w *walker) addressText(answers [][]reflect.Value) string {
	saved := w.mode
	w.mode = readAddresses
	defer func() { w.mode = saved }()
	var b strings.Builder
	for _, args := range answers {
		for _, a := range args {
			b.WriteString(w.dump(a, 0))
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// mapEntries returns the entries of a map in the order of their keys' text. A key
// is rendered before any ordinal is assigned, so map iteration order reaches no ordinal.
func (w *walker) mapEntries(v reflect.Value) []mapEntry {
	saved := w.mode
	if w.mode == assignOrdinals {
		w.mode = readOrdinals
	}
	entries := make([]mapEntry, 0, v.Len())
	for it := v.MapRange(); it.Next(); {
		entries = append(entries, mapEntry{text: w.dump(it.Key(), 0), key: it.Key(), value: it.Value()})
	}
	w.mode = saved
	slices.SortFunc(entries, func(a, b mapEntry) int { return strings.Compare(a.text, b.text) })
	for i := 1; i < len(entries); i++ {
		if entries[i].text == entries[i-1].text {
			panic("recordtest: a map holds two keys that render as " + entries[i].text + ", so its order is not fixed")
		}
	}
	return entries
}

type mapEntry struct {
	text       string
	key, value reflect.Value
}

// iteratorText renders an iterator that an iterator yielded, for the comparison of
// two ranges. A walk reaches no other iterator here, because [walker.walk] takes an
// iterator it can range and a value holds the rest.
func (w *walker) iteratorText(v reflect.Value) string {
	if w.mode != readAddresses || !v.CanInterface() {
		panic("recordtest: a " + v.Type().String() + " is held below a value, so its answers are not compared")
	}
	defer w.enterIterator(v)()
	return "[" + w.addressText(rangeOf(v)) + "]"
}

// enterIterator counts one iterator on the current path and returns its exit. A
// stack overflow cannot be recovered and names no path, so an iterator that yields
// itself must panic first.
func (w *walker) enterIterator(v reflect.Value) func() {
	w.iterators++
	if w.iterators > maxDepth {
		panic(fmt.Sprintf("recordtest: a %s is nested deeper than %d iterators, so a difference below it is not compared", v.Type(), maxDepth))
	}
	return func() { w.iterators-- }
}

// handleText renders a handle below a value under the walker's mode.
func (w *walker) handleText(v reflect.Value) string {
	switch {
	case v.Field(0).IsNil():
		return "zero"
	case w.mode == readAddresses:
		return fmt.Sprintf("record at %x of %s", v.Field(0).Pointer(), v.Type())
	case w.mode == readOrdinals:
		if n, ok := w.ordinals[identityOf(v)]; ok {
			return fmt.Sprintf("record #%d", n)
		}
		return "record with no ordinal"
	}
	return fmt.Sprintf("record #%d", w.ordinal(v))
}

// dump renders a value that is not walked. It reads unexported fields, so it holds
// no method call: a handle below this point renders as its ordinal, and [Walk]
// requires a walked path to the same record.
func (w *walker) dump(v reflect.Value, depth int) string {
	if !v.IsValid() {
		return "invalid"
	}
	t := v.Type()
	if slices.Contains(w.opts.OmitTypes, t) {
		return t.String()
	}
	if depth > maxDepth {
		panic(fmt.Sprintf("recordtest: a %s is nested deeper than %d levels, so a difference below it is not compared", t, maxDepth))
	}
	if isHandle(t) {
		return w.handleText(v)
	}
	if ref, ok := referenceOf(v); ok {
		if at, onPath := w.dumping[ref]; onPath {
			return fmt.Sprintf("cycle to depth %d", at)
		}
		w.dumping[ref] = depth
		defer delete(w.dumping, ref)
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return "nil"
		}
		return t.String() + "(" + w.dump(v.Elem(), depth+1) + ")"
	case reflect.Struct:
		if p := t.PkgPath(); p == "sync" || p == "sync/atomic" {
			return t.String()
		}
		parts := make([]string, 0, t.NumField())
		for i := range t.NumField() {
			parts = append(parts, t.Field(i).Name+":"+w.dump(v.Field(i), depth+1))
		}
		return t.String() + "{" + strings.Join(parts, " ") + "}"
	case reflect.Slice:
		if v.IsNil() {
			return "nil slice"
		}
		fallthrough
	case reflect.Array:
		parts := make([]string, 0, v.Len())
		for i := range v.Len() {
			parts = append(parts, w.dump(v.Index(i), depth+1))
		}
		return "[" + strings.Join(parts, " ") + "]"
	case reflect.Map:
		if v.IsNil() {
			return "nil map"
		}
		parts := make([]string, 0, v.Len())
		for _, e := range w.mapEntries(v) {
			parts = append(parts, w.dump(e.key, depth+1)+":"+w.dump(e.value, depth+1))
		}
		return "map[" + strings.Join(parts, " ") + "]"
	case reflect.Func, reflect.Chan, reflect.UnsafePointer:
		if v.IsNil() {
			return "nil " + v.Kind().String()
		}
		if isIterator(t) {
			return w.iteratorText(v)
		}
		panic("recordtest: a " + t.String() + " is held below a value, so its answers are not compared")
	case reflect.String:
		return fmt.Sprintf("%q", v.String())
	case reflect.Bool:
		return strconv.FormatBool(v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return strconv.FormatUint(v.Uint(), 10)
	case reflect.Float32, reflect.Float64:
		return fmt.Sprint(v.Float())
	default:
		return v.Kind().String()
	}
}
