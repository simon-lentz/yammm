package instance_test

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/instance"
)

// Build's error names the relation whose EdgeTo call first violated
// cardinality, on every run.
func TestSchemaBuilder_CardinalityErrorIsDeterministic(t *testing.T) {
	s := loadSrc(t, `schema "p"

type C {
    id String primary
}

type P {
    id String primary
    --> R1 (one) C
    --> R2 (one) C
}
`)
	for range 50 {
		b, err := instance.BuilderFor(s, "P")
		if err != nil {
			t.Fatal(err)
		}
		_, err = b.Property("id", "x").EdgeTo("r1", "a").EdgeTo("r1", "b").EdgeTo("r2", "a").EdgeTo("r2", "b").Build()
		if err == nil || !strings.Contains(err.Error(), `"R1"`) {
			t.Fatalf("want R1 first every time, got %v", err)
		}
	}
}
