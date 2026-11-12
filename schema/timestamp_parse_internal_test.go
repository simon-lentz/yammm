package schema

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/parse"
	"github.com/simon-lentz/yammm/internal/raceskip"
	"github.com/simon-lentz/yammm/location"
)

// The loader builds a Timestamp with no allocation, as the zero value it is.
func TestSchemaParser_BuildsATimestampWithoutAllocating(t *testing.T) {
	src := "schema \"s\"\n\ntype At = Timestamp\n"
	id := location.NewSourceID("test://s.yammm")
	file, issues := parse.Parse([]byte(src), id)
	if len(issues) != 0 {
		t.Fatalf("parse: %v", issues)
	}
	plain := file.DataTypes[0].Constraint
	p := newParser(id, diag.NewCollector(0))
	if c, _ := p.constraint(plain); c != (TimestampConstraint{}) {
		t.Fatalf("constraint = %#v; want the zero Timestamp", c)
	}

	raceskip.Skip(t)
	var built Constraint
	if allocs := testing.AllocsPerRun(100, func() { built, _ = p.constraint(plain) }); allocs != 0 {
		t.Errorf("the loader allocates %v building a Timestamp; want 0", allocs)
	}
	_ = built
}
