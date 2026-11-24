package schema_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

const unionLayoutEntry = `schema "app"

import "lib/item" as x

type Account {
	id String primary
	*-> LOCAL (_:one) Local
	*-> REMOTE (_:one) x.Item
	! "either" (LOCAL -> Default(REMOTE)).missing != ""
}

part type Local {
	id String primary
}
`

const unionLayoutLib = `schema "lib"

part type Item {
	id String primary
}
`

// unionLayoutMessages loads the union fixture with its entry file at entryPath
// and returns the messages of the issues the load refuses it with.
func unionLayoutMessages(t *testing.T, entryPath string) []string {
	t.Helper()
	_, err := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{
		entryPath:        []byte(unionLayoutEntry),
		"lib/item.yammm": []byte(unionLayoutLib),
	}, entryPath, schema.WithSourcesOnly(true), schema.WithModuleRoot("/project"))
	var re *diag.ResultError
	if !errors.As(err, &re) {
		t.Fatalf("load with the entry at %s: %v, want a *diag.ResultError", entryPath, err)
	}
	var messages []string
	for issue := range re.Result.Issues() {
		messages = append(messages, issue.Message())
	}
	return messages
}

// TestLoad_ClosureLayoutDoesNotMoveAUnionsMessage pins that a union of instance
// types reads its alternatives by schema name, then type name: one closure
// loaded with its entry file at two places refuses with one message.
func TestLoad_ClosureLayoutDoesNotMoveAUnionsMessage(t *testing.T) {
	t.Parallel()

	atRoot, inDir := unionLayoutMessages(t, "main.yammm"), unionLayoutMessages(t, "a/main.yammm")
	want := `unknown property "missing" on type "Local", one of the types the value may be (Local or Item), in invariant "either" on type "Account"`
	if !slices.Contains(atRoot, want) {
		t.Errorf("entry at the root: messages %q, want one %q", atRoot, want)
	}
	if !slices.Equal(atRoot, inDir) {
		t.Errorf("one closure in two layouts refuses with two message lists:\n%q\n---\n%q", atRoot, inDir)
	}
}

const oneNameEntry = `schema "s"

import "lib/s" as x

type Account {
	id String primary
	*-> LOCAL (_:one) T
	*-> REMOTE (_:one) x.T
	! "lr" (LOCAL -> Default(REMOTE)).foo != ""
	! "rl" (REMOTE -> Default(LOCAL)).foo != ""
}

part type T {
	id String primary
	*-> FOO (_:one) P
}

part type P {
	id String primary
}
`

const oneNameLib = `schema "s"

part type T {
	id String primary
}
`

// TestLoad_UnionOfTwoSchemasOfOneNameIsOneValue pins that a union is one value
// in either operand order when its alternatives share a schema name and a type
// name. The entry's invariants are checked before the load refuses its second
// schema of one name, so the order must not rest on that refusal.
func TestLoad_UnionOfTwoSchemasOfOneNameIsOneValue(t *testing.T) {
	t.Parallel()
	_, err := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{
		"main.yammm":  []byte(oneNameEntry),
		"lib/s.yammm": []byte(oneNameLib),
	}, "main.yammm", schema.WithSourcesOnly(true), schema.WithModuleRoot("/project"))
	var re *diag.ResultError
	if !errors.As(err, &re) {
		t.Fatalf("load: %v, want a *diag.ResultError", err)
	}
	codes := map[string]diag.Code{}
	for issue := range re.Result.Issues() {
		for _, name := range []string{"lr", "rl"} {
			if strings.Contains(issue.Message(), `in invariant "`+name+`"`) {
				codes[name] = issue.Code()
			}
		}
	}
	if len(codes) != 2 {
		t.Fatalf("issues %v name invariants %v, want both lr and rl", slices.Collect(re.Result.Issues()), codes)
	}
	if codes["lr"] != codes["rl"] {
		t.Errorf("one union read in two operand orders draws %s and %s", codes["lr"], codes["rl"])
	}
}
