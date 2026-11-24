package cypher

import (
	"strings"
	"testing"
)

func TestDropStatements_RenderByName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		identifier string
		constraint string
		index      string
	}{
		{
			name:       "a generated name is quoted like any other",
			identifier: "book_catalog__Publisher_publisher_id_unique",
			constraint: "DROP CONSTRAINT `book_catalog__Publisher_publisher_id_unique` IF EXISTS",
			index:      "DROP INDEX `book_catalog__Publisher_publisher_id_unique` IF EXISTS",
		},
		{
			name:       "hyphen fails validation and is quoted",
			identifier: "legacy-constraint",
			constraint: "DROP CONSTRAINT `legacy-constraint` IF EXISTS",
			index:      "DROP INDEX `legacy-constraint` IF EXISTS",
		},
		{
			name:       "reserved word is quoted, not rejected",
			identifier: "MATCH",
			constraint: "DROP CONSTRAINT `MATCH` IF EXISTS",
			index:      "DROP INDEX `MATCH` IF EXISTS",
		},
		{
			name:       "embedded backtick is doubled inside the quotes",
			identifier: "we`ird",
			constraint: "DROP CONSTRAINT `we``ird` IF EXISTS",
			index:      "DROP INDEX `we``ird` IF EXISTS",
		},
		{
			name:       "internal spaces survive inside the quotes",
			identifier: "my constraint",
			constraint: "DROP CONSTRAINT `my constraint` IF EXISTS",
			index:      "DROP INDEX `my constraint` IF EXISTS",
		},
		{
			name:       "an empty name renders as the empty quoted name",
			identifier: "",
			constraint: "DROP CONSTRAINT `` IF EXISTS",
			index:      "DROP INDEX `` IF EXISTS",
		},
		{
			name:       "surrounding spaces are kept",
			identifier: " x ",
			constraint: "DROP CONSTRAINT ` x ` IF EXISTS",
			index:      "DROP INDEX ` x ` IF EXISTS",
		},
		{
			name:       "leading digit fails validation and is quoted",
			identifier: "1st_index",
			constraint: "DROP CONSTRAINT `1st_index` IF EXISTS",
			index:      "DROP INDEX `1st_index` IF EXISTS",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := DropConstraintStatement(tt.identifier); got != tt.constraint {
				t.Errorf("DropConstraintStatement(%q) = %q, want %q", tt.identifier, got, tt.constraint)
			}
			if got := DropIndexStatement(tt.identifier); got != tt.index {
				t.Errorf("DropIndexStatement(%q) = %q, want %q", tt.identifier, got, tt.index)
			}
		})
	}
}

// A quoted name must not be able to close its own quoting and append Cypher.
func TestDropStatements_QuotingContainsInjection(t *testing.T) {
	t.Parallel()
	got := DropConstraintStatement("x` IF EXISTS; DROP CONSTRAINT y")
	want := "DROP CONSTRAINT `x`` IF EXISTS; DROP CONSTRAINT y` IF EXISTS"
	if got != want {
		t.Fatalf("DropConstraintStatement = %q, want %q", got, want)
	}
	// One quoted identifier, so an even number of backtick runs and no bare
	// statement separator outside the quotes.
	if strings.Count(got, "`")%2 != 0 {
		t.Errorf("unbalanced backticks in %q", got)
	}
}

func TestConstraintDropStatement(t *testing.T) {
	t.Parallel()
	c := Constraint{Name: "app__Person_id_unique"}
	if got, want := c.DropStatement(), "DROP CONSTRAINT `app__Person_id_unique` IF EXISTS"; got != want {
		t.Errorf("Constraint.DropStatement() = %q, want %q", got, want)
	}
}

func TestIndexDropStatement(t *testing.T) {
	t.Parallel()
	i := Index{Name: "app__Person_state_idx"}
	if got, want := i.DropStatement(), "DROP INDEX `app__Person_state_idx` IF EXISTS"; got != want {
		t.Errorf("Index.DropStatement() = %q, want %q", got, want)
	}
}

// Every name the emitters generate must be a valid identifier, since a CREATE
// statement writes it unquoted. The index half of this assertion already exists
// beside indexName; this is the constraint half.
func TestConstraintName_IsAValidIdentifier(t *testing.T) {
	t.Parallel()
	for _, kind := range []ConstraintKind{ConstraintUnique, ConstraintNotNull, ConstraintType, ConstraintNodeKey} {
		name := constraintName("app__Person", []string{"id", "region"}, kind)
		if !IsIdentifier(name) {
			t.Errorf("constraintName(...) = %q, which IsIdentifier refuses", name)
		}
	}
}
