package schema

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/parse"
	"github.com/simon-lentz/yammm/internal/raceskip"
	"github.com/simon-lentz/yammm/location"
)

// The loader's Timestamp holds the parser's format with no fault, and one with
// no format is built with no allocation, as the zero value it is.
func TestSchemaParser_BuildsATimestampWithNoFormatWithoutAllocating(t *testing.T) {
	src := "schema \"s\"\n\ntype At = Timestamp\ntype On = Timestamp[\"2006-01-02\"]\n"
	id := location.MustNewSourceID("test://s.yammm")
	file, issues := parse.Parse([]byte(src), id)
	if len(issues) != 0 {
		t.Fatalf("parse: %v", issues)
	}
	plain, formatted := file.DataTypes[0].Constraint, file.DataTypes[1].Constraint
	p := newParser(id, diag.NewCollector(0))
	if c, _ := p.constraint(plain); c != (TimestampConstraint{}) {
		t.Fatalf("constraint = %#v; want the zero Timestamp", c)
	}
	if c, _ := p.constraint(formatted); c != (TimestampConstraint{format: "2006-01-02"}) {
		t.Fatalf("constraint = %#v; want the format and no fault", c)
	}

	raceskip.Skip(t)
	var built Constraint
	if allocs := testing.AllocsPerRun(100, func() { built, _ = p.constraint(plain) }); allocs != 0 {
		t.Errorf("the loader allocates %v building a Timestamp with no format; want 0", allocs)
	}
	_ = built
}

// The judge reads the verdict the constructor recorded and never the format.
// A Timestamp holding a format that is not UTF-8 and no recorded fault, a value
// no constructor makes, is judged sound, and one holding a sound format and a
// recorded fault is refused, alone and as a List's element.
func TestConstraintFault_ReadsATimestampsRecordedVerdictAlone(t *testing.T) {
	t.Parallel()
	unjudged := TimestampConstraint{format: "2006\xff"}
	recorded := TimestampConstraint{format: "2006-01-02", badFormat: true}
	for name, wrap := range map[string]func(Constraint) Constraint{
		"alone":             func(c Constraint) Constraint { return c },
		"as a List element": func(c Constraint) Constraint { return NewListConstraint(c) },
	} {
		if fault, refused := constraintFault(wrap(unjudged)); refused {
			t.Errorf("%s: a format with no recorded fault is refused: %s", name, fault)
		}
		if _, refused := constraintFault(wrap(recorded)); !refused {
			t.Errorf("%s: a recorded fault is not refused", name)
		}
	}
}
