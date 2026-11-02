package instancetest_test

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/location/path"
	"github.com/simon-lentz/yammm/schema"
)

func loadPerson(t *testing.T) schema.Schema {
	t.Helper()
	s, res := schema.LoadString(t.Context(), `schema "person"

type Person {
	id String primary
	name String required
}
`, "person.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	return s
}

func TestValid_ReturnsTheValidatorsInstance(t *testing.T) {
	vi := instancetest.Valid(t, loadPerson(t), "Person", map[string]any{"id": "p7", "name": "Alice"})
	if got := vi.PrimaryKey().String(); got != `["p7"]` {
		t.Errorf("PrimaryKey() = %s, want [\"p7\"]", got)
	}
	if !vi.Provenance().IsZero() {
		t.Errorf("Provenance() = %v, want the zero Provenance", vi.Provenance())
	}
}

func TestValidRaw_CarriesTheProvenance(t *testing.T) {
	prov := location.NewProvenance("test.json", path.Root(), location.Span{})
	vi := instancetest.ValidRaw(t, loadPerson(t), "Person", instance.RawInstance{
		Properties: map[string]any{"id": "p7", "name": "Alice"},
		Provenance: prov,
	})
	if vi.Provenance().SourceName() != "test.json" {
		t.Errorf("Provenance() = %v, want test.json", vi.Provenance())
	}
}

func TestValid_FailsTheTestOnARefusal(t *testing.T) {
	s := loadPerson(t)
	if !fatalOf(t, func(tb *recordingTB) {
		instancetest.Valid(tb, s, "Person", map[string]any{"id": "p7"})
	}).fatal {
		t.Error("a refused fixture did not fail the test")
	}
}

// A Refused member draws no issue of its own, and the validator returns no instance.
func TestValid_FailsTheTestOnARefusalWithNoIssue(t *testing.T) {
	s := loadPerson(t)
	if !fatalOf(t, func(tb *recordingTB) {
		instancetest.Valid(tb, s, "Person", map[string]any{"id": "p7", "name": instance.Refused{Value: "x"}})
	}).fatal {
		t.Error("a fixture holding a Refused member did not fail the test")
	}
}

func TestTypeID_NamesTheTypeOfTheSourceItWasGiven(t *testing.T) {
	first := instancetest.TypeID(t, "a.yammm", "T")
	if first.IsZero() {
		t.Fatal("TypeID returned the zero identity")
	}
	if first != instancetest.TypeID(t, "a.yammm", "T") {
		t.Error("one source and one name gave two identities")
	}
	if first == instancetest.TypeID(t, "b.yammm", "T") {
		t.Error("two sources gave one identity")
	}
}

func TestTypeID_FailsTheTestOnANameTheLoadRefuses(t *testing.T) {
	got := fatalOf(t, func(tb *recordingTB) { instancetest.TypeID(tb, "a.yammm", "lower") })
	if !strings.Contains(got.message, "instancetest.TypeID: load lower") {
		t.Errorf("Fatalf message = %q, want the load's refusal", got.message)
	}
}

// The name closes the fixture's type and opens another, so the load accepts it
// and declares no type of that name.
func TestTypeID_FailsTheTestOnANameTheSchemaDoesNotDeclare(t *testing.T) {
	const name = "T {\n\tid String primary\n}\n\ntype U"
	got := fatalOf(t, func(tb *recordingTB) { instancetest.TypeID(tb, "a.yammm", name) })
	if !strings.Contains(got.message, "the schema declares no type") {
		t.Errorf("Fatalf message = %q, want the missing type's", got.message)
	}
}

// fatalOf runs run on its own goroutine, since a Fatalf ends the goroutine.
func fatalOf(t *testing.T, run func(*recordingTB)) *recordingTB {
	t.Helper()
	tb := &recordingTB{TB: t}
	done := make(chan struct{})
	go func() {
		defer close(done)
		run(tb)
	}()
	<-done
	return tb
}

// recordingTB records Fatalf and ends the goroutine, as testing.T does.
type recordingTB struct {
	testing.TB

	fatal   bool
	message string
}

func (r *recordingTB) Helper() {}

func (r *recordingTB) Fatalf(format string, args ...any) {
	r.fatal = true
	r.message = fmt.Sprintf(format, args...)
	runtime.Goexit()
}
