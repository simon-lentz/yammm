package yammm_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// zeroCall is one exported method of a zero handle, called with zero arguments.
type zeroCall struct {
	name     string
	panicked bool
	results  []reflect.Value
}

// callZero calls every exported method of a zero handle but the named ones.
func callZero(t *testing.T, zero any, skip ...string) []zeroCall {
	t.Helper()
	v := reflect.ValueOf(zero)
	var calls []zeroCall
	for i := range v.NumMethod() {
		m := v.Type().Method(i)
		if m.Name == "IsZero" || slices.Contains(skip, m.Name) {
			continue
		}
		args := make([]reflect.Value, m.Type.NumIn()-1)
		for j := range args {
			args[j] = reflect.Zero(m.Type.In(j + 1))
		}
		call := zeroCall{name: m.Name}
		func() {
			defer func() { call.panicked = recover() != nil }()
			call.results = v.Method(i).Call(args)
		}()
		calls = append(calls, call)
	}
	return calls
}

// One moment for every accessor of a schema record: at the call, an iterator's
// included, so no zero handle hands out an iterator that fails at its range.
func TestZeroHandle_ASchemaRecordsAccessorPanicsAtTheCall(t *testing.T) {
	t.Parallel()
	for _, zero := range []any{
		schema.Schema{},
		schema.Type{},
		schema.DataType{},
		schema.Property{},
		schema.Relation{},
		schema.Invariant{},
		schema.Import{},
		schema.Annotation{},
	} {
		// Equal and CanNarrowFrom compare, and a zero handle equals only itself.
		for _, call := range callZero(t, zero, "Equal", "CanNarrowFrom") {
			if !call.panicked {
				t.Errorf("zero %T.%s did not panic at the call", zero, call.name)
			}
		}
	}
}

// A zero record of these answers as holding nothing: no call panics, and no iterator
// panics or yields.
func TestZeroHandle_OtherRecordsAnswerAsHoldingNothing(t *testing.T) {
	t.Parallel()
	for _, zero := range []any{
		schema.Sources{},
		graph.Snapshot{},
		graph.Instance{},
		graph.Edge{},
		graph.Duplicate{},
		graph.UnresolvedEdge{},
		instance.ValidInstance{},
		instance.ValidEdgeData{},
		location.Provenance{},
	} {
		for _, call := range callZero(t, zero) {
			if call.panicked {
				t.Errorf("zero %T.%s panicked, want an empty answer", zero, call.name)
			}
			for _, result := range call.results {
				switch {
				case result.Type().CanSeq() && result.Kind() == reflect.Func:
					for range result.Seq() {
						t.Errorf("zero %T.%s yielded, want nothing", zero, call.name)
					}
				case result.Type().CanSeq2() && result.Kind() == reflect.Func:
					for range result.Seq2() {
						t.Errorf("zero %T.%s yielded, want nothing", zero, call.name)
					}
				}
			}
		}
	}
}
