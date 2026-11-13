package schema_test

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// A member that does not parse may be the key its author wrote, so its type
// draws the member's E_SYNTAX and no E_NO_PRIMARY_KEY. A type whose body
// parsed whole is still judged.
func TestLoad_ATypeThatLostAMemberIsNotBlamedForItsKey(t *testing.T) {
	t.Parallel()
	for _, member := range []string{
		"id String required primary", "id String Primary", "id String primary\n\tx", "id String[1, primary",
		"id String primary @index(1,\n\t\tb c)", "id String @index primary",
	} {
		t.Run(member, func(t *testing.T) {
			t.Parallel()
			src := "schema \"s\"\ntype T {\n\t" + member + "\n}\n\ntype Keyless {\n\tname String\n}\n"
			_, res := schema.LoadString(t.Context(), src, "s.yammm")
			counts := codeCounts(res)
			if counts[diag.E_SYNTAX] == 0 {
				t.Fatalf("precondition: the member is a syntax error; got %v", res)
			}
			if n := counts[diag.E_NO_PRIMARY_KEY]; n != 1 {
				t.Errorf("want E_NO_PRIMARY_KEY for Keyless alone; got %d: %v", n, res)
			}
			for is := range res.Issues() {
				if is.Code() == diag.E_NO_PRIMARY_KEY && !strings.Contains(is.Message(), `"Keyless"`) {
					t.Errorf("E_NO_PRIMARY_KEY names the type that lost a member: %s", is.Message())
				}
			}
		})
	}
}

// A body that ends at end of input may lack its key, so its type draws the
// E_SYNTAX alone.
func TestLoad_ATypeCutOffAtEndOfInputIsNotBlamedForItsKey(t *testing.T) {
	t.Parallel()
	_, res := schema.LoadString(t.Context(), "schema \"s\"\ntype T {\n\tname String\n", "s.yammm")
	if counts := codeCounts(res); counts[diag.E_SYNTAX] != 1 || counts[diag.E_NO_PRIMARY_KEY] != 0 {
		t.Errorf("want one E_SYNTAX and no E_NO_PRIMARY_KEY; got %v", res)
	}
}

// A key a supertype's body lost may be the one its subtype inherits, so the
// subtype draws no E_NO_PRIMARY_KEY either.
func TestLoad_ATypeWhoseSupertypeLostAMemberIsNotBlamedForItsKey(t *testing.T) {
	t.Parallel()
	for _, member := range []string{`id Pattern["a", "b"] primary`, "id Integer[1, 2, 3] primary", "id String Primary"} {
		t.Run(member, func(t *testing.T) {
			t.Parallel()
			src := "schema \"s\"\nabstract type B {\n\t" + member + "\n}\n\ntype C extends B {\n}\n"
			_, res := schema.LoadString(t.Context(), src, "s.yammm")
			if counts := codeCounts(res); counts[diag.E_SYNTAX] == 0 || counts[diag.E_NO_PRIMARY_KEY] != 0 {
				t.Errorf("want the member's E_SYNTAX and no E_NO_PRIMARY_KEY; got %v", res)
			}
		})
	}
}

// Skipped text that holds no token able to begin a member loses none: an
// annotation's argument list that does not parse, however recovery splits it,
// an annotation's name, a comment, or a stray ")". The keyless type is still
// judged.
func TestLoad_ASkipThatHoldsNoMemberLeavesTheKeyJudged(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		"name String @index()", "name String\n\t@@index()", "name String @index // c\n\t()",
		"name String\n\t)", "name String @index(foo bar)",
		"name String @index(1 /* c */ 2)", "name String @index(1 @@ 2)", "name String @index(1 ! 2)",
		"name String @index(1 --> 2)",
		"name String @index /* c */ (1 2)", "name String @index(1 2) /* c */", "name String\n\t/* c */",
		"name String @index(1 2) @index(3 4)",
	} {
		t.Run(body, func(t *testing.T) {
			t.Parallel()
			src := "schema \"s\"\ntype T {\n\t" + body + "\n}\n"
			_, res := schema.LoadString(t.Context(), src, "s.yammm")
			if counts := codeCounts(res); counts[diag.E_SYNTAX] == 0 || counts[diag.E_NO_PRIMARY_KEY] != 1 {
				t.Errorf("want the E_SYNTAX and one E_NO_PRIMARY_KEY; got %v", res)
			}
		})
	}
}

// A parenthesis left open where parsing resumes may have swallowed a member its
// author wrote, so the type is not blamed for its key, though a later skip
// closes it.
func TestLoad_AMemberSwallowedByAnOpenParenthesisIsNotBlamedForTheKey(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		"name String @index(1 id String primary", "name String @index(1 id Strin\n\tage Integer\n\t)",
	} {
		t.Run(body, func(t *testing.T) {
			t.Parallel()
			src := "schema \"s\"\ntype T {\n\t" + body + "\n}\n"
			_, res := schema.LoadString(t.Context(), src, "s.yammm")
			if counts := codeCounts(res); counts[diag.E_SYNTAX] == 0 || counts[diag.E_NO_PRIMARY_KEY] != 0 {
				t.Errorf("want the E_SYNTAX and no E_NO_PRIMARY_KEY; got %v", res)
			}
		})
	}
}

// The grammar reads an annotation's name past spaces and line comments, so a
// name written apart from its "@" is no member either, after a relation where
// no annotation parses. A doc comment ends the annotation, as the grammar
// reads it, so the word after one may be a member.
func TestLoad_AnAnnotationNameApartFromItsAtSignLosesNoMember(t *testing.T) {
	t.Parallel()
	for stray, lost := range map[string]bool{
		"@ unique": false, "@\n\tunique": false, "@ // c\n\tunique": false, "@ unique(1 2)": false,
		"@\tunique(1)": false, "@ Primary": false, "@ unique ( name String )": false, "@unique": false,
		"@/* d */unique": true, "@-->": true,
	} {
		t.Run(stray, func(t *testing.T) {
			t.Parallel()
			src := "schema \"s\"\ntype B {\n\tid String primary\n}\n\ntype T {\n\tname String\n\t--> R (one) B\n\t" +
				stray + "\n}\n"
			_, res := schema.LoadString(t.Context(), src, "s.yammm")
			counts := codeCounts(res)
			if counts[diag.E_SYNTAX] == 0 {
				t.Fatalf("precondition: the stray text is a syntax error; got %v", res)
			}
			if want := map[bool]int{false: 1, true: 0}[lost]; counts[diag.E_NO_PRIMARY_KEY] != want {
				t.Errorf("want %d E_NO_PRIMARY_KEY; got %v", want, res)
			}
		})
	}
}

// A member start inside parentheses that close holds no member, though an
// outer parenthesis stays open where the body ends.
func TestLoad_AMemberInsideClosedParenthesesWithinAnOpenOneIsNoMember(t *testing.T) {
	t.Parallel()
	for body, lost := range map[string]bool{
		"name String @index( ( name2 )": false, "name String @index( ( name2 ) id String primary": true,
		"name String @index( id String primary ( name2 )": true,
	} {
		t.Run(body, func(t *testing.T) {
			t.Parallel()
			src := "schema \"s\"\ntype T {\n\t" + body + "\n}\n"
			_, res := schema.LoadString(t.Context(), src, "s.yammm")
			counts := codeCounts(res)
			if counts[diag.E_SYNTAX] == 0 {
				t.Fatalf("precondition: the text is a syntax error; got %v", res)
			}
			if want := map[bool]int{false: 1, true: 0}[lost]; counts[diag.E_NO_PRIMARY_KEY] != want {
				t.Errorf("want %d E_NO_PRIMARY_KEY; got %v", want, res)
			}
		})
	}
}
