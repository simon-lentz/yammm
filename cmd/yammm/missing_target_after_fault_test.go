package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const missingTargetSchema = `schema "t"

type Team {
    id String primary
}

type Person {
    id String primary
    --> TEAM (one) Team
    --> CLUBS (one:many) Team
}
`

// After a data file that draws an E_DATA_PARSE, a target the data lacks is not
// reported missing, as go build checks no types after a syntax error: the
// fault stopped the read or refused an element, and that element may be the
// target's instance. A refused type tag names no declared type, so its element
// is no target and the report stays. An association an instance leaves out or
// holds empty is reported either way, and the fault of one file holds for
// every file the command reads, in either order.
func TestCheck_ReportsNoMissingTargetAfterAParseFault(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "t.yammm")
	if err := os.WriteFile(schemaPath, []byte(missingTargetSchema), 0o600); err != nil {
		t.Fatal(err)
	}
	write := func(name, data string) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	const people = `"Person":[{"id":"p","team":{"_target_id":"t"},"clubs":[{"_target_id":"t"}]},{"id":"q","clubs":[]}]`
	whole := write("whole.json", `{"Person":[{"id":"r","team":{"_target_id":"zzz"},"clubs":[{"_target_id":"s"}]}],"Team":[{"id":"s"}]}`)
	cut := write("cut.json", `{"Team":[{"id":`)
	for _, c := range []struct {
		name  string
		files []string
		fault string
		// missing is whether a missing target is reported.
		missing bool
	}{
		{"a document cut short", []string{write("short.json", `{`+people+`,"Team":[{"id":`)}, "E_DATA_PARSE", false},
		{"a string that is not valid UTF-8", []string{write("utf8.json", "{"+people+",\"Team\":[{\"id\":\"t\xff\"}]}")}, "E_DATA_PARSE", false},
		{"a repeated member", []string{write("repeat.json", `{`+people+`,"Team":[{"id":"t","id":"t"}]}`)}, "E_DATA_PARSE", false},
		{"a refused type tag", []string{write("tag.json", `{`+people+`,"team":[{"id":"t"}]}`)}, "E_INVALID_TYPE_TAG", true},
		{"a whole file before a cut one", []string{whole, cut}, "E_DATA_PARSE", false},
		{"a cut file before a whole one", []string{cut, whole}, "E_DATA_PARSE", false},
	} {
		code, _, stderr := executeCmdOutput(t, append([]string{"check", schemaPath}, c.files...)...)
		if code != 1 || !strings.Contains(stderr, c.fault) {
			t.Fatalf("%s: exit %d, want 1 with %s: %s", c.name, code, c.fault, stderr)
		}
		if got := strings.Contains(stderr, "target instance not found"); got != c.missing {
			t.Errorf("%s: reports a missing target %v, want %v: %s", c.name, got, c.missing, stderr)
		}
		if len(c.files) > 1 {
			continue
		}
		for _, reason := range []string{`"TEAM" is unresolved: association field is absent`, `"CLUBS" is unresolved: association array is empty`} {
			if !strings.Contains(stderr, reason) {
				t.Errorf("%s: %s is not reported: %s", c.name, reason, stderr)
			}
		}
	}
}
