package markdown

import (
	"strings"
	"testing"
)

// TestPropertyTable_MergedAnnotationProperty pins the "from <Owner>" provenance
// of a property whose annotations were merged across ancestors. Such a row is a
// synthesized copy that appears in no type's own property slice, so a
// pointer-keyed owner lookup misses it and silently falls back to the declaring
// scope's bare name — "A" instead of the document's display name "base.A".
func TestPropertyTable_MergedAnnotationProperty(t *testing.T) {
	s := loadSources(t, map[string][]byte{
		"entry.yammm": []byte(`schema "main"

import "base.yammm" as base

abstract type Audited {
	amount String @writeOnce
}

type C extends base.A, Audited {
	name String primary
}
`),
		"base.yammm": []byte(`schema "base"

abstract type A {
	amount String
}
`),
	})

	got := sectionFor(t, s, "C")
	if !strings.Contains(got, "from base.A") {
		t.Errorf("merged amount row should name the declaring ancestor as displayed (base.A); got:\n%s", got)
	}
}
