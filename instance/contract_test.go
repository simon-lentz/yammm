package instance_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/schema"
)

// contractFixture loads a testdata/contract schema+data pair. The data file
// holds {"TypeName": [instances...]}; every instance validates as that type.
func contractFixture(t *testing.T, schemaFile, dataFile string) (*schema.Schema, map[string][]map[string]any) {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("testdata", "contract", schemaFile))
	if err != nil {
		t.Fatal(err)
	}
	s, result := schema.LoadString(t.Context(), string(src), schemaFile)
	if result.HasErrors() {
		t.Fatalf("load %s: %s", schemaFile, result)
	}

	raw, err := os.ReadFile(filepath.Join("testdata", "contract", dataFile))
	if err != nil {
		t.Fatal(err)
	}
	var data map[string][]map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	return s, data
}

// TestContract_PassCorpus pins the invariant expression language's behavioral
// contract: every invariant in the corpus states one documented behavior and
// must hold. The invariant-count floor makes an emptied fixture fail loudly
// instead of passing vacuously.
func TestContract_PassCorpus(t *testing.T) {
	t.Parallel()
	s, data := contractFixture(t, "contract_pass.yammm", "data_pass.json")

	typ, ok := s.Type("T")
	if !ok {
		t.Fatal("contract_pass.yammm declares no type T")
	}
	if n := len(typ.InvariantsSlice()); n < 100 {
		t.Fatalf("contract_pass.yammm declares %d invariants, floor is 100 — fixture emptied?", n)
	}

	v := instance.NewValidator(s)
	validated := 0
	for typeName, instances := range data {
		for _, props := range instances {
			valid, result := v.ValidateOne(t.Context(), typeName, instance.RawInstance{Properties: props})
			if !result.OK() {
				t.Errorf("pass corpus drew diagnostics: %s", result)
			}
			if valid == nil {
				t.Error("pass corpus returned nil instance")
			}
			validated++
		}
	}

	// The floor above guards the schema fixture; this guards the data one,
	// whose emptying would leave every assertion above unreached.
	if validated == 0 {
		t.Fatal("data_pass.json drove no instance — every assertion above was skipped")
	}
	for typeName, typ := range s.Types() {
		// Abstract and part types have no ValidateOne entry point, so requiring
		// an instance for them would fail on a fixture that is entirely correct.
		if typ.IsAbstract() || typ.IsPart() {
			continue
		}
		if len(data[typeName]) == 0 {
			t.Errorf("type %q is declared in the corpus but has no instance in data_pass.json", typeName)
		}
	}
}

// TestContract_ErrCorpus pins the error side of the contract: each probe draws
// exactly its predicted diagnostic, as an exact multiset over (code, message).
// A defect the static checker refuses at load — an undefined named variable, a
// member read through an association — cannot appear here; those are pinned
// by the schema package's static-invariant tests.
// This arm doubles as the instrument-can-fail proof for the pass corpus — if
// invariant evaluation stopped running, the expected diagnostics vanish.
func TestContract_ErrCorpus(t *testing.T) {
	t.Parallel()
	s, data := contractFixture(t, "contract_err.yammm", "data_err.json")

	want := []string{
		"E_EVAL_ERROR: invariant evaluation error: All expects slice or array input, got int64",
		"E_EVAL_ERROR: invariant evaluation error: Contains expects slice or array input, got string",
		"E_EVAL_ERROR: invariant evaluation error: Upper() expects string argument, got int64",
		"E_EVAL_ERROR: invariant evaluation error: division by zero",
		"E_EVAL_ERROR: invariant evaluation error: expected boolean, got int64",
		"E_EVAL_ERROR: invariant evaluation error: min of empty sequence",
		"E_EVAL_ERROR: invariant evaluation error: modulo by zero",
		"E_EVAL_ERROR: invariant evaluation error: reduce of empty sequence with no initial value",
		"E_EVAL_ERROR: invariant evaluation error: slice access accepts exactly one index",
		"E_EVAL_ERROR: invariant evaluation error: slice access requires an index",
		"E_INVARIANT_FAIL: f_nil_result_is_falsey",
	}

	v := instance.NewValidator(s)
	var got []string
	for typeName, instances := range data {
		for _, props := range instances {
			valid, result := v.ValidateOne(t.Context(), typeName, instance.RawInstance{Properties: props})
			if valid != nil {
				t.Error("err corpus returned a valid instance")
			}
			for issue := range result.Issues() {
				if c := issue.Code(); c != diag.E_EVAL_ERROR && c != diag.E_INVARIANT_FAIL {
					t.Errorf("unexpected code %s: %s", c, issue.Message())
				}
				got = append(got, issue.Code().String()+": "+issue.Message())
			}
		}
	}
	slices.Sort(got)
	yammmtest.Diff(t, want, got)
}
