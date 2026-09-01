package neo4j

import (
	"errors"
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
			name:       "leading digit fails validation and is quoted",
			identifier: "1st_index",
			constraint: "DROP CONSTRAINT `1st_index` IF EXISTS",
			index:      "DROP INDEX `1st_index` IF EXISTS",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := DropConstraintStatement(tt.identifier)
			if err != nil {
				t.Fatalf("DropConstraintStatement(%q) error: %v", tt.identifier, err)
			}
			if got != tt.constraint {
				t.Errorf("DropConstraintStatement(%q) = %q, want %q", tt.identifier, got, tt.constraint)
			}
			got, err = DropIndexStatement(tt.identifier)
			if err != nil {
				t.Fatalf("DropIndexStatement(%q) error: %v", tt.identifier, err)
			}
			if got != tt.index {
				t.Errorf("DropIndexStatement(%q) = %q, want %q", tt.identifier, got, tt.index)
			}
		})
	}
}

func TestDropStatements_EmptyNameIsAnError(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"", " ", "\t\n  "} {
		t.Run(strings.ReplaceAll(name, "\t", "tab"), func(t *testing.T) {
			t.Parallel()
			if got, err := DropConstraintStatement(name); !errors.Is(err, ErrEmptyIdentifier) {
				t.Errorf("DropConstraintStatement(%q) = %q, %v; want ErrEmptyIdentifier", name, got, err)
			}
			if got, err := DropIndexStatement(name); !errors.Is(err, ErrEmptyIdentifier) {
				t.Errorf("DropIndexStatement(%q) = %q, %v; want ErrEmptyIdentifier", name, got, err)
			}
		})
	}
}

// A quoted name must not be able to close its own quoting and append Cypher.
func TestDropStatements_QuotingContainsInjection(t *testing.T) {
	t.Parallel()
	got, err := DropConstraintStatement("x` IF EXISTS; DROP CONSTRAINT y")
	if err != nil {
		t.Fatalf("DropConstraintStatement error: %v", err)
	}
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
	got, err := c.DropStatement()
	if err != nil {
		t.Fatalf("Constraint.DropStatement error: %v", err)
	}
	if want := "DROP CONSTRAINT `app__Person_id_unique` IF EXISTS"; got != want {
		t.Errorf("Constraint.DropStatement() = %q, want %q", got, want)
	}
}

// WithNamedConstraints(false) leaves Name empty, and an anonymous constraint has
// no portable name to drop it by.
func TestConstraintDropStatement_AnonymousConstraintIsAnError(t *testing.T) {
	t.Parallel()
	adapter := New(WithNamedConstraints(false))
	s := loadSchema(t, "basic.yammm")
	constraints, result := adapter.ConstraintsStructured(t.Context(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("generating constraints: %v", err)
	}
	if len(constraints) == 0 {
		t.Fatal("fixture produced no constraints, so this asserts nothing")
	}
	for _, c := range constraints {
		if c.Name != "" {
			t.Fatalf("WithNamedConstraints(false) emitted the name %q", c.Name)
		}
		if _, err := c.DropStatement(); !errors.Is(err, ErrEmptyIdentifier) {
			t.Errorf("Constraint.DropStatement() error = %v; want ErrEmptyIdentifier", err)
		}
	}
}

func TestIndexDropStatement(t *testing.T) {
	t.Parallel()
	i := Index{Name: "app__Person_state_idx"}
	got, err := i.DropStatement()
	if err != nil {
		t.Fatalf("Index.DropStatement error: %v", err)
	}
	if want := "DROP INDEX `app__Person_state_idx` IF EXISTS"; got != want {
		t.Errorf("Index.DropStatement() = %q, want %q", got, want)
	}
}

// Every name the emitters generate must interpolate bare, or a routine DROP
// would carry quoting nothing needs. The index half of this assertion already
// exists beside indexName; this is the constraint half.
func TestConstraintName_IsAValidIdentifier(t *testing.T) {
	t.Parallel()
	for _, kind := range []ConstraintKind{ConstraintUnique, ConstraintNotNull, ConstraintType, ConstraintNodeKey} {
		name := constraintName("app__Person", []string{"id", "region"}, kind)
		if err := ValidateIdentifier(name, "constraint name"); err != nil {
			t.Errorf("constraintName(...) = %q, which fails validation: %v", name, err)
		}
	}
}
