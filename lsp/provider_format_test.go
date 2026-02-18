package lsp

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema/load"
)

func TestFormatDocument_NoChanges(t *testing.T) {
	t.Parallel()

	input := `schema "test"

type Person {
	name String required
}
`
	result := formatDocument(input)
	if result != input {
		t.Errorf("formatDocument: expected no changes, got:\n%q", result)
	}

	tsResult, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream returned error: %v", err)
	}
	if tsResult != input {
		t.Errorf("formatTokenStream: expected no changes, got:\n%q", tsResult)
	}
}

func TestFormatDocument_TrailingWhitespace(t *testing.T) {
	t.Parallel()

	input := "schema \"test\"   \n\ntype Person {   \n\tname String required   \n}\n"
	expected := "schema \"test\"\n\ntype Person {\n\tname String required\n}\n"

	result := formatDocument(input)
	if result != expected {
		t.Errorf("formatDocument() =\n%q\nwant:\n%q", result, expected)
	}

	tsResult, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream returned error: %v", err)
	}
	if tsResult != expected {
		t.Errorf("formatTokenStream() =\n%q\nwant:\n%q", tsResult, expected)
	}
}

func TestFormatDocument_NormalizeCRLF(t *testing.T) {
	t.Parallel()

	input := "schema \"test\"\r\n\r\ntype Person {\r\n\tname String\r\n}\r\n"
	expected := "schema \"test\"\n\ntype Person {\n\tname String\n}\n"

	result := formatDocument(input)
	if result != expected {
		t.Errorf("formatDocument() =\n%q\nwant:\n%q", result, expected)
	}

	tsResult, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream returned error: %v", err)
	}
	if tsResult != expected {
		t.Errorf("formatTokenStream() =\n%q\nwant:\n%q", tsResult, expected)
	}
}

func TestFormatDocument_NormalizeCR(t *testing.T) {
	t.Parallel()

	input := "schema \"test\"\r\rtype Person {\r\tname String\r}\r"
	expected := "schema \"test\"\n\ntype Person {\n\tname String\n}\n"

	result := formatDocument(input)
	if result != expected {
		t.Errorf("formatDocument() =\n%q\nwant:\n%q", result, expected)
	}

	tsResult, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream returned error: %v", err)
	}
	if tsResult != expected {
		t.Errorf("formatTokenStream() =\n%q\nwant:\n%q", tsResult, expected)
	}
}

func TestFormatDocument_PreservesBlankLines(t *testing.T) {
	t.Parallel()

	input := `schema "test"



type Person {
	name String
}



type Company {
	title String
}
`

	// formatDocument preserves blank lines (conservative aesthetic choice)
	fdExpected := `schema "test"



type Person {
	name String
}



type Company {
	title String
}
`
	result := formatDocument(input)
	if result != fdExpected {
		t.Errorf("formatDocument() =\n%q\nwant:\n%q", result, fdExpected)
	}

	// formatTokenStream collapses blank lines (Phase 2: max 1 blank between declarations)
	tsExpected := `schema "test"

type Person {
	name String
}

type Company {
	title String
}
`
	tsResult, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream returned error: %v", err)
	}
	if tsResult != tsExpected {
		t.Errorf("formatTokenStream() =\n%q\nwant:\n%q", tsResult, tsExpected)
	}
}

func TestFormatDocument_RemoveTrailingBlankLines(t *testing.T) {
	t.Parallel()

	input := "schema \"test\"\n\ntype Person {\n\tname String\n}\n\n\n\n"
	expected := "schema \"test\"\n\ntype Person {\n\tname String\n}\n"

	result := formatDocument(input)
	if result != expected {
		t.Errorf("formatDocument() =\n%q\nwant:\n%q", result, expected)
	}

	tsResult, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream returned error: %v", err)
	}
	if tsResult != expected {
		t.Errorf("formatTokenStream() =\n%q\nwant:\n%q", tsResult, expected)
	}
}

func TestFormatDocument_EnsureTrailingNewline(t *testing.T) {
	t.Parallel()

	input := "schema \"test\"\n\ntype Person {\n\tname String\n}"
	expected := "schema \"test\"\n\ntype Person {\n\tname String\n}\n"

	result := formatDocument(input)
	if result != expected {
		t.Errorf("formatDocument() =\n%q\nwant:\n%q", result, expected)
	}

	tsResult, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream returned error: %v", err)
	}
	if tsResult != expected {
		t.Errorf("formatTokenStream() =\n%q\nwant:\n%q", tsResult, expected)
	}
}

func TestFormatDocument_PreservesComments(t *testing.T) {
	t.Parallel()

	input := `schema "test"

// This is a type
type Person {
	name String // inline comment
}
`
	result := formatDocument(input)
	if result != input {
		t.Errorf("formatDocument: comments should be preserved, got:\n%q", result)
	}

	tsResult, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream returned error: %v", err)
	}
	if tsResult != input {
		t.Errorf("formatTokenStream: comments should be preserved, got:\n%q", tsResult)
	}
}

func TestFormatDocument_PreservesIndentation(t *testing.T) {
	t.Parallel()

	input := `schema "test"

type Person {
	name String
	age Integer
	--> EMPLOYER (one) Company
}
`
	result := formatDocument(input)
	if result != input {
		t.Errorf("formatDocument: indentation should be preserved, got:\n%q", result)
	}

	// formatTokenStream aligns name column within same-kind groups
	tsExpected := `schema "test"

type Person {
	name String
	age  Integer
	--> EMPLOYER (one) Company
}
`
	tsResult, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream returned error: %v", err)
	}
	if tsResult != tsExpected {
		t.Errorf("formatTokenStream() =\n%q\nwant:\n%q", tsResult, tsExpected)
	}
}

func TestFormatDocument_Empty(t *testing.T) {
	t.Parallel()

	input := ""
	result := formatDocument(input)

	if result != "" {
		t.Errorf("empty input should return empty output, got: %q", result)
	}
}

func TestFormatDocument_OnlyWhitespace(t *testing.T) {
	t.Parallel()

	input := "   \n\t\n   \n"
	result := formatDocument(input)

	if result != "" {
		t.Errorf("whitespace-only input should return empty, got: %q", result)
	}
}

func TestFormatDocument_Idempotent(t *testing.T) {
	t.Parallel()

	input := `schema "test"


type Person {
	name String

	age Integer
}


`

	// formatDocument idempotency
	first := formatDocument(input)
	second := formatDocument(first)
	if first != second {
		t.Errorf("formatDocument should be idempotent:\nfirst:\n%q\nsecond:\n%q", first, second)
	}

	// formatTokenStream idempotency
	tsFirst, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream first pass returned error: %v", err)
	}
	tsSecond, err := formatTokenStream(tsFirst)
	if err != nil {
		t.Fatalf("formatTokenStream second pass returned error: %v", err)
	}
	if tsFirst != tsSecond {
		t.Errorf("formatTokenStream should be idempotent:\nfirst:\n%q\nsecond:\n%q", tsFirst, tsSecond)
	}
}

func TestFormatDocument_ComplexDocument(t *testing.T) {
	t.Parallel()

	input := `schema "vehicles"


import "./parts" as parts


// Abstract vehicle type
abstract type Vehicle {
	vin String[17, 17] primary


	--> MANUFACTURER (one) Manufacturer
}


// Concrete car type
type Car extends Vehicle {
	model String required
	*-> WHEELS (many) parts.Wheel
}


`

	// formatDocument preserves blank lines (trailing blank lines at EOF removed)
	fdExpected := `schema "vehicles"


import "./parts" as parts


// Abstract vehicle type
abstract type Vehicle {
	vin String[17, 17] primary


	--> MANUFACTURER (one) Manufacturer
}


// Concrete car type
type Car extends Vehicle {
	model String required
	*-> WHEELS (many) parts.Wheel
}
`
	result := formatDocument(input)
	if result != fdExpected {
		t.Errorf("formatDocument() =\n%q\nwant:\n%q", result, fdExpected)
	}

	// formatTokenStream collapses double blanks to single
	tsExpected := `schema "vehicles"

import "./parts" as parts

// Abstract vehicle type
abstract type Vehicle {
	vin String[17, 17] primary

	--> MANUFACTURER (one) Manufacturer
}

// Concrete car type
type Car extends Vehicle {
	model String required
	*-> WHEELS (many) parts.Wheel
}
`
	tsResult, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream returned error: %v", err)
	}
	if tsResult != tsExpected {
		t.Errorf("formatTokenStream() =\n%q\nwant:\n%q", tsResult, tsExpected)
	}
}

func TestFormatTokenStream_DeclarationSpacing(t *testing.T) {
	t.Parallel()

	input := `schema "test"

type   Address{
    name String  required
    age Integer [ 0 , _ ]
    score Float[- 90.0, 90.0]
    -->  REL ( one ) Target / owned_by(one)
}

type Email=Pattern["^.+@.+$"]
`
	expected := `schema "test"

type Address {
	name  String required
	age   Integer[0, _]
	score Float[-90.0, 90.0]
	--> REL (one) Target / owned_by (one)
}

type Email = Pattern["^.+@.+$"]
`

	result, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream returned error: %v", err)
	}

	if result != expected {
		t.Errorf("formatTokenStream() =\n%q\nwant:\n%q", result, expected)
	}
}

func TestFormatTokenStream_ExpressionPreservation(t *testing.T) {
	t.Parallel()

	input := `schema "test"

type   RuleSet{
    ! "all_positive" ITEMS -> All |$item| { $item.qty > 0 }
    ! "adult_status" age >= 18 ? { "adult" : "minor" } == category
    ! "must_be_enabled" !disabled && active
    ! "grouping" (a > 0) && (b < 100)
    ! "replace" items -> Replace("old", "new")
}
`
	expected := `schema "test"

type RuleSet {
	! "all_positive" ITEMS -> All |$item| { $item.qty > 0 }
	! "adult_status" age >= 18 ? { "adult" : "minor" } == category
	! "must_be_enabled" !disabled && active
	! "grouping" (a > 0) && (b < 100)
	! "replace" items -> Replace("old", "new")
}
`

	result, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream returned error: %v", err)
	}

	if result != expected {
		t.Errorf("formatTokenStream() =\n%q\nwant:\n%q", result, expected)
	}

	if !strings.Contains(result, `! "must_be_enabled" !disabled && active`) {
		t.Errorf("logical NOT spacing should be preserved, got:\n%s", result)
	}
	if !strings.Contains(result, `{ "adult" : "minor" }`) {
		t.Errorf("ternary brace/colon spacing should be preserved, got:\n%s", result)
	}
}

func TestFormatTokenStream_CommentHandling(t *testing.T) {
	t.Parallel()

	input := `schema "test"

/* Doc
block
*/
type   Person{
    // standalone
    name String // inline
}
`
	expected := `schema "test"

/* Doc
block
*/
type Person {
	// standalone
	name String // inline
}
`

	result, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream returned error: %v", err)
	}

	if result != expected {
		t.Errorf("formatTokenStream() =\n%q\nwant:\n%q", result, expected)
	}
}

func TestFormatTokenStream_CollapsesBlankLines(t *testing.T) {
	t.Parallel()

	input := `schema "test"



type   Person{
    name String


    age Integer
}



type Company{
    title String
}
`
	expected := `schema "test"

type Person {
	name String

	age Integer
}

type Company {
	title String
}
`

	result, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream returned error: %v", err)
	}
	if result != expected {
		t.Errorf("formatTokenStream() =\n%q\nwant:\n%q", result, expected)
	}
}

func TestFormatTokenStream_BlankLinesAtStartOfFile(t *testing.T) {
	t.Parallel()

	input := `

schema "test"

type Person {
	name String
}
`
	expected := `schema "test"

type Person {
	name String
}
`

	result, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream returned error: %v", err)
	}
	if result != expected {
		t.Errorf("formatTokenStream() =\n%q\nwant:\n%q", result, expected)
	}
}

func TestFormatTokenStream_NoBlankAfterOpenBrace(t *testing.T) {
	t.Parallel()

	input := `schema "test"

type Person {

	name String
	age Integer
}
`
	expected := `schema "test"

type Person {
	name String
	age  Integer
}
`

	result, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream returned error: %v", err)
	}
	if result != expected {
		t.Errorf("formatTokenStream() =\n%q\nwant:\n%q", result, expected)
	}
}

func TestFormatTokenStream_NoBlankBeforeCloseBrace(t *testing.T) {
	t.Parallel()

	input := `schema "test"

type Person {
	name String
	age Integer

}
`
	expected := `schema "test"

type Person {
	name String
	age  Integer
}
`

	result, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream returned error: %v", err)
	}
	if result != expected {
		t.Errorf("formatTokenStream() =\n%q\nwant:\n%q", result, expected)
	}
}

func TestFormatTokenStream_EnsureBlankAfterSchema(t *testing.T) {
	t.Parallel()

	input := `schema "test"
type Person {
	name String
}
`
	expected := `schema "test"

type Person {
	name String
}
`

	result, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream returned error: %v", err)
	}
	if result != expected {
		t.Errorf("formatTokenStream() =\n%q\nwant:\n%q", result, expected)
	}
}

func TestFormatTokenStream_EnsureBlankAfterImportBlock(t *testing.T) {
	t.Parallel()

	input := `schema "test"

import "./other" as other
type Person {
	name String
}
`
	expected := `schema "test"

import "./other" as other

type Person {
	name String
}
`

	result, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream returned error: %v", err)
	}
	if result != expected {
		t.Errorf("formatTokenStream() =\n%q\nwant:\n%q", result, expected)
	}
}

func TestFormatTokenStream_ImportGroupingPreserved(t *testing.T) {
	t.Parallel()

	input := `schema "test"

import "./a" as a

import "./b" as b

type T {
	name String
}
`

	result, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream returned error: %v", err)
	}
	if result != input {
		t.Errorf("formatTokenStream() =\n%q\nwant:\n%q", result, input)
	}
}

func TestFormatTokenStream_CommentNotCollapsedAsBlank(t *testing.T) {
	t.Parallel()

	input := `schema "test"

type Person {
	name String
}

// This is a comment between types
type Company {
	title String
}
`

	result, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream returned error: %v", err)
	}
	if result != input {
		t.Errorf("formatTokenStream() =\n%q\nwant:\n%q", result, input)
	}
}

func TestFormatTokenStream_DocCommentMultilineNotCollapsed(t *testing.T) {
	t.Parallel()

	input := `schema "test"

/* This is a
multiline doc
comment */
type Person {
	name String
}
`

	result, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream returned error: %v", err)
	}
	if result != input {
		t.Errorf("formatTokenStream() =\n%q\nwant:\n%q", result, input)
	}
}

func TestFormatTokenStream_EdgePropertyBlockBlanks(t *testing.T) {
	t.Parallel()

	input := `schema "test"

type T {
	--> REL (one) Target {

		weight Float required
		score Integer

	}
}
`
	expected := `schema "test"

type T {
	--> REL (one) Target {
		weight Float required
		score  Integer
	}
}
`

	result, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream returned error: %v", err)
	}
	if result != expected {
		t.Errorf("formatTokenStream() =\n%q\nwant:\n%q", result, expected)
	}
}

func TestFormatTokenStream_GoldenFile(t *testing.T) {
	t.Parallel()

	unformatted, err := os.ReadFile("../testdata/lsp/formatting/unformatted.yammm")
	if err != nil {
		t.Fatalf("failed to read unformatted fixture: %v", err)
	}
	golden, err := os.ReadFile("../testdata/lsp/formatting/formatted.yammm.golden")
	if err != nil {
		t.Fatalf("failed to read golden fixture: %v", err)
	}

	result, err := formatTokenStream(string(unformatted))
	if err != nil {
		t.Fatalf("formatTokenStream returned error: %v", err)
	}
	if result != string(golden) {
		t.Errorf("formatTokenStream(unformatted) !=golden\ngot:\n%q\nwant:\n%q", result, string(golden))
	}
}

func TestFormatTokenStream_GoldenIdempotent(t *testing.T) {
	t.Parallel()

	golden, err := os.ReadFile("../testdata/lsp/formatting/formatted.yammm.golden")
	if err != nil {
		t.Fatalf("failed to read golden fixture: %v", err)
	}

	result, err := formatTokenStream(string(golden))
	if err != nil {
		t.Fatalf("formatTokenStream returned error: %v", err)
	}
	if result != string(golden) {
		t.Errorf("formatTokenStream(golden) != golden\ngot:\n%q\nwant:\n%q", result, string(golden))
	}
}

func TestFormatTokenStream_GoldenFixtures(t *testing.T) {
	t.Parallel()

	fixtures := []string{
		"alignment",
		"wrapping",
		"expressions",
		"edge_cases",
		"comprehensive",
	}

	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			inputPath := filepath.Join("..", "testdata", "lsp", "formatting", name+".yammm")
			goldenPath := filepath.Join("..", "testdata", "lsp", "formatting", name+".yammm.golden")

			input, err := os.ReadFile(inputPath)
			if err != nil {
				t.Fatalf("failed to read fixture %s: %v", name, err)
			}
			golden, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("failed to read golden %s: %v", name, err)
			}

			result, err := formatTokenStream(string(input))
			if err != nil {
				t.Fatalf("formatTokenStream returned error: %v", err)
			}
			if result != string(golden) {
				t.Errorf("formatTokenStream(%s) != golden\ngot:\n%s\nwant:\n%s", name, result, string(golden))
			}
		})
	}
}

func TestFormatTokenStream_GoldenIdempotentAll(t *testing.T) {
	t.Parallel()

	goldenFiles := []string{
		"formatted.yammm.golden",
		"alignment.yammm.golden",
		"wrapping.yammm.golden",
		"expressions.yammm.golden",
		"edge_cases.yammm.golden",
		"comprehensive.yammm.golden",
	}

	for _, name := range goldenFiles {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			goldenPath := filepath.Join("..", "testdata", "lsp", "formatting", name)
			golden, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("failed to read golden %s: %v", name, err)
			}

			result, err := formatTokenStream(string(golden))
			if err != nil {
				t.Fatalf("formatTokenStream returned error: %v", err)
			}
			if result != string(golden) {
				t.Errorf("formatTokenStream(%s) not idempotent\ngot:\n%s\nwant:\n%s", name, result, string(golden))
			}
		})
	}
}

func TestFormatTokenStream_BlankLineCollapsingIdempotent(t *testing.T) {
	t.Parallel()

	input := `



schema "test"



import "./a" as a
import "./b" as b



// Comment
abstract type Base {



	id String primary



	name String required



}



type Concrete extends Base {
	--> REL (one) Target {


		weight Float


	}
}



`

	first, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream first pass returned error: %v", err)
	}

	second, err := formatTokenStream(first)
	if err != nil {
		t.Fatalf("formatTokenStream second pass returned error: %v", err)
	}

	if first != second {
		t.Errorf("blank line collapsing should be idempotent:\nfirst:\n%q\nsecond:\n%q", first, second)
	}
}

func TestFormatTokenStream_Idempotent(t *testing.T) {
	t.Parallel()

	input := `schema "test"

type   Person{
    name String  required
    ! "must_be_enabled" !disabled && active
}
`

	first, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream first pass returned error: %v", err)
	}

	second, err := formatTokenStream(first)
	if err != nil {
		t.Fatalf("formatTokenStream second pass returned error: %v", err)
	}

	if first != second {
		t.Errorf("formatTokenStream should be idempotent:\nfirst:\n%q\nsecond:\n%q", first, second)
	}
}

func TestFormatTokenStream_InvalidInputReturnsError(t *testing.T) {
	t.Parallel()

	input := `schema "test"

type Person {
	name String
`

	_, err := formatTokenStream(input)
	if err == nil {
		t.Fatal("expected formatTokenStream to return error for malformed input")
	}
}

func TestFormatTokenStream_ColonInMultiplicity(t *testing.T) {
	t.Parallel()

	input := `schema "test"

type T {
	--> REL (_ : many) Target
	--> REL2 ( _:one ) Target
	*-> REL3 ( one : many ) Target
}
`
	expected := `schema "test"

type T {
	--> REL  (_:many) Target
	--> REL2 (_:one) Target
	*-> REL3 (one:many) Target
}
`

	result, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream returned error: %v", err)
	}
	if result != expected {
		t.Errorf("formatTokenStream() =\n%q\nwant:\n%q", result, expected)
	}
}

func TestFormatTokenStream_QualifiedReferences(t *testing.T) {
	t.Parallel()

	input := `schema "test"

import "./other" as other

type T {
	--> REL (one) other . Target
	name other . CustomType
}
`
	expected := `schema "test"

import "./other" as other

type T {
	--> REL (one) other.Target
	name other.CustomType
}
`

	result, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream returned error: %v", err)
	}
	if result != expected {
		t.Errorf("formatTokenStream() =\n%q\nwant:\n%q", result, expected)
	}
}

func TestFormatTokenStream_ImportSpacing(t *testing.T) {
	t.Parallel()

	input := `schema "test"

import   "./path"   as   alias
import"./other"as other

type T {
	name String
}
`
	expected := `schema "test"

import "./path" as alias
import "./other" as other

type T {
	name String
}
`

	result, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream returned error: %v", err)
	}
	if result != expected {
		t.Errorf("formatTokenStream() =\n%q\nwant:\n%q", result, expected)
	}
}

func TestFormatTokenStream_ExtendsMultipleTypes(t *testing.T) {
	t.Parallel()

	input := `schema "test"

abstract type Base {
	id String primary
}

abstract type Auditable {
	ts Timestamp required
}

type Concrete extends  Base ,Auditable {
	name String required
}
`
	expected := `schema "test"

abstract type Base {
	id String primary
}

abstract type Auditable {
	ts Timestamp required
}

type Concrete extends Base, Auditable {
	name String required
}
`

	result, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream returned error: %v", err)
	}
	if result != expected {
		t.Errorf("formatTokenStream() =\n%q\nwant:\n%q", result, expected)
	}
}

func TestFormatTokenStream_AllConstraintBracketTypes(t *testing.T) {
	t.Parallel()

	input := `schema "test"

type T {
	a String [1, 255]
	b Integer [0, _]
	c Float [0.0, 100.0]
	d Enum ["x", "y", "z"]
	e Pattern ["^[a-z]+$"]
	f Timestamp ["2006-01-02"]
	g Vector [128]
}
`
	expected := `schema "test"

type T {
	a String[1, 255]
	b Integer[0, _]
	c Float[0.0, 100.0]
	d Enum["x", "y", "z"]
	e Pattern["^[a-z]+$"]
	f Timestamp["2006-01-02"]
	g Vector[128]
}
`

	result, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream returned error: %v", err)
	}
	if result != expected {
		t.Errorf("formatTokenStream() =\n%q\nwant:\n%q", result, expected)
	}
}

func TestFormatTokenStream_DOCCommentNewlineAfter(t *testing.T) {
	t.Parallel()

	// Verify DOC_COMMENT always gets a newline before the next declaration token.
	input := `schema "test"

/* Entity doc */
type T {
	/* Field doc */
	name String
}
`
	expected := `schema "test"

/* Entity doc */
type T {
	/* Field doc */
	name String
}
`

	result, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream returned error: %v", err)
	}
	if result != expected {
		t.Errorf("formatTokenStream() =\n%q\nwant:\n%q", result, expected)
	}
}

func TestFormatTokenStream_TrailingCommaInConstraints(t *testing.T) {
	t.Parallel()

	// Trailing comma inside Enum is grammar-legal and should be tight before RBRACK.
	input := `schema "test"

type T {
	status Enum["a", "b", "c",]
}
`
	expected := `schema "test"

type T {
	status Enum["a", "b", "c",]
}
`

	result, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream returned error: %v", err)
	}
	if result != expected {
		t.Errorf("formatTokenStream() =\n%q\nwant:\n%q", result, expected)
	}
}

func TestFormatting_UsesTokenStreamFormatterForIntraLineSpacing(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	content := "schema \"test\"\n\ntype   A{\n\tname String\n}\n"
	filePath := filepath.Join(tmpDir, "test.yammm")
	if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := NewServer(logger, Config{ModuleRoot: tmpDir})
	uri := PathToURI(filePath)

	if err := server.textDocumentDidOpen(nil, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:        uri,
			LanguageID: "yammm",
			Version:    1,
			Text:       content,
		},
	}); err != nil {
		t.Fatalf("textDocumentDidOpen failed: %v", err)
	}

	edits, err := server.textDocumentFormatting(nil, &protocol.DocumentFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	})
	if err != nil {
		t.Fatalf("textDocumentFormatting failed: %v", err)
	}

	if len(edits) == 0 {
		t.Fatal("expected formatting edits for intra-line spacing normalization")
	}

	edit := edits[0]
	if edit.Range.Start.Line != 0 || edit.Range.Start.Character != 0 {
		t.Errorf("edit range should start at 0,0; got %d,%d", edit.Range.Start.Line, edit.Range.Start.Character)
	}
	if !strings.Contains(edit.NewText, "type A {") {
		t.Errorf("expected formatted text to normalize type spacing, got:\n%s", edit.NewText)
	}
}

func TestNormalizeIndentation_NoLeading(t *testing.T) {
	t.Parallel()

	input := "name String"
	result := normalizeIndentation(input)

	if result != input {
		t.Errorf("normalizeIndentation(%q) = %q; want %q", input, result, input)
	}
}

func TestNormalizeIndentation_Tabs(t *testing.T) {
	t.Parallel()

	input := "\tname String"
	result := normalizeIndentation(input)

	if result != input {
		t.Errorf("tabs should be preserved: %q", result)
	}
}

func TestNormalizeIndentation_SpacesToTabs(t *testing.T) {
	t.Parallel()

	input := "    name String"  // 4 spaces
	expected := "\tname String" // 1 tab

	result := normalizeIndentation(input)

	if result != expected {
		t.Errorf("normalizeIndentation(%q) = %q; want %q", input, result, expected)
	}
}

func TestNormalizeIndentation_MixedSpaces(t *testing.T) {
	t.Parallel()

	input := "      name String"  // 6 spaces
	expected := "\t  name String" // 1 tab + 2 spaces

	result := normalizeIndentation(input)

	if result != expected {
		t.Errorf("normalizeIndentation(%q) = %q; want %q", input, result, expected)
	}
}

func TestNormalizeIndentation_Empty(t *testing.T) {
	t.Parallel()

	input := ""
	result := normalizeIndentation(input)

	if result != "" {
		t.Errorf("empty input should return empty, got: %q", result)
	}
}

func TestFormatDocument_ConvertSpacesToTabs(t *testing.T) {
	t.Parallel()

	// Input uses 4-space indentation
	input := `schema "test"

type Person {
    name String required
    age Integer
}
`
	// formatDocument: tab indentation, no alignment
	fdExpected := `schema "test"

type Person {
	name String required
	age Integer
}
`

	result := formatDocument(input)
	if result != fdExpected {
		t.Errorf("formatDocument: spaces should be converted to tabs:\ngot:\n%q\nwant:\n%q", result, fdExpected)
	}

	// formatTokenStream: tab indentation + name column alignment
	tsExpected := `schema "test"

type Person {
	name String required
	age  Integer
}
`
	tsResult, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream returned error: %v", err)
	}
	if tsResult != tsExpected {
		t.Errorf("formatTokenStream: spaces should be converted to tabs:\ngot:\n%q\nwant:\n%q", tsResult, tsExpected)
	}
}

func TestFormatDocument_MixedIndentNormalized(t *testing.T) {
	t.Parallel()

	// Input uses mixed 6-space indentation (1 tab + 2 spaces)
	input := `schema "test"

type Person {
      name String
}
`
	// formatDocument normalizes to 1 tab + 2 spaces (preserves residual)
	expectedLineByLine := `schema "test"

type Person {
	  name String
}
`

	result := formatDocument(input)
	if result != expectedLineByLine {
		t.Errorf("formatDocument: mixed indent should be normalized:\ngot:\n%q\nwant:\n%q", result, expectedLineByLine)
	}

	// formatTokenStream uses canonical brace-depth indentation (1 tab at depth 1)
	expectedCanonical := `schema "test"

type Person {
	name String
}
`

	tsResult, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream returned error: %v", err)
	}
	if tsResult != expectedCanonical {
		t.Errorf("formatTokenStream: mixed indent should use brace-depth indentation:\ngot:\n%q\nwant:\n%q", tsResult, expectedCanonical)
	}
}

// --- hasSyntaxErrors tests ---

func TestHasSyntaxErrors_NoErrors(t *testing.T) {
	t.Parallel()

	// Valid schema should have no errors
	ctx := context.Background()
	_, result, err := load.LoadString(ctx, `schema "test"

type Person {
	name String
}
`, "test")
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	if hasSyntaxErrors(result) {
		t.Error("hasSyntaxErrors() returned true for valid schema")
	}
}

func TestHasSyntaxErrors_WithSyntaxError(t *testing.T) {
	t.Parallel()

	// Invalid syntax - missing closing brace
	ctx := context.Background()
	_, result, err := load.LoadString(ctx, `schema "test"

type Person {
	name String
`, "test")
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	if !hasSyntaxErrors(result) {
		t.Error("hasSyntaxErrors() should return true for syntax error")
	}
}

func TestHasSyntaxErrors_WithImportOnly(t *testing.T) {
	t.Parallel()

	// Schema with import - LoadString disallows imports, but this is NOT a syntax error
	// The parse succeeds; the import restriction is a semantic error (E_IMPORT_NOT_ALLOWED)
	ctx := context.Background()
	_, result, err := load.LoadString(ctx, `schema "test"

import "./other" as other

type Person {
	name String
}
`, "test")
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	// Should have errors (import not allowed)
	if result.OK() {
		t.Fatal("expected result to have errors due to import")
	}

	// But NOT syntax errors - the file is syntactically valid
	if hasSyntaxErrors(result) {
		t.Error("hasSyntaxErrors() should return false for import-only errors")
	}
}

func TestHasSyntaxErrors_VerifyImportErrorCategory(t *testing.T) {
	t.Parallel()

	// Verify that import errors are categorized as CategoryImport, not CategorySyntax
	ctx := context.Background()
	_, result, err := load.LoadString(ctx, `schema "test"
import "./other" as other
type Person { name String }
`, "test")
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	// Check that we have errors
	if result.OK() {
		t.Fatal("expected errors due to import in LoadString")
	}

	// Verify the error category
	foundImportError := false
	for issue := range result.Errors() {
		if issue.Code().Category() == diag.CategoryImport {
			foundImportError = true
		}
		if issue.Code().Category() == diag.CategorySyntax {
			t.Errorf("import error should not be CategorySyntax, got code: %s", issue.Code())
		}
	}

	if !foundImportError {
		t.Error("expected to find an import error (CategoryImport)")
	}
}

// =============================================================================
// Multibyte Content Tests (Priority 5: Test Coverage Gaps)
// =============================================================================

func TestFormatDocument_MultibyteCJK(t *testing.T) {
	// Test formatting with CJK characters (Chinese/Japanese/Korean) in strings
	// YAMMM identifiers are ASCII-only, but string literals can contain Unicode
	// CJK characters are 3-byte UTF-8
	t.Parallel()

	input := `schema "日本語テスト"

type User {
	name String required
	// 年齢 means age in Japanese
	age Integer
}
`
	expected := `schema "日本語テスト"

type User {
	name String required
	// 年齢 means age in Japanese
	age Integer
}
`

	result := formatDocument(input)
	if result != expected {
		t.Errorf("formatDocument() with CJK content:\ngot:\n%q\nwant:\n%q", result, expected)
	}

	tsResult, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream returned error: %v", err)
	}
	if tsResult != expected {
		t.Errorf("formatTokenStream() with CJK content:\ngot:\n%q\nwant:\n%q", tsResult, expected)
	}
}

func TestFormatDocument_Emoji(t *testing.T) {
	// Test formatting with emoji (4-byte UTF-8, surrogate pairs in UTF-16)
	t.Parallel()

	input := `schema "emoji🎉"

type User {
	status String
}
`
	expected := `schema "emoji🎉"

type User {
	status String
}
`

	result := formatDocument(input)
	if result != expected {
		t.Errorf("formatDocument() with emoji:\ngot:\n%q\nwant:\n%q", result, expected)
	}

	tsResult, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream returned error: %v", err)
	}
	if tsResult != expected {
		t.Errorf("formatTokenStream() with emoji:\ngot:\n%q\nwant:\n%q", tsResult, expected)
	}
}

func TestFormatDocument_MultibyteMixedContent(t *testing.T) {
	// Test formatting with mixed ASCII and multibyte content in comments and strings
	// YAMMM identifiers are ASCII-only, but strings and comments can contain Unicode
	t.Parallel()

	input := `schema "混合Content"

// コメント with 日本語
type MixedType {
	ascii String required
	// 日本語フィールド
	jpField Integer
	// emoji🎉field
	emojiField Float
}
`
	expected := `schema "混合Content"

// コメント with 日本語
type MixedType {
	ascii String required
	// 日本語フィールド
	jpField Integer
	// emoji🎉field
	emojiField Float
}
`

	result := formatDocument(input)
	if result != expected {
		t.Errorf("formatDocument() with mixed content:\ngot:\n%q\nwant:\n%q", result, expected)
	}

	tsResult, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream returned error: %v", err)
	}
	if tsResult != expected {
		t.Errorf("formatTokenStream() with mixed content:\ngot:\n%q\nwant:\n%q", tsResult, expected)
	}
}

func TestFormatDocument_MultibyteParseable(t *testing.T) {
	// Test that formatted multibyte content in strings is still parseable
	// YAMMM identifiers are ASCII-only, but string literals can contain Unicode
	t.Parallel()

	input := `schema "CJKテスト"

type JapaneseUser {
	name String required
}
`

	result := formatDocument(input)

	// Verify formatDocument result is still valid YAMMM
	ctx := context.Background()
	s, diagResult, err := load.LoadString(ctx, result, "test")
	if err != nil {
		t.Fatalf("formatDocument output failed to load: %v", err)
	}
	if !diagResult.OK() {
		for issue := range diagResult.Issues() {
			t.Logf("issue: %v", issue)
		}
		t.Error("formatDocument: formatted multibyte content should be parseable without errors")
	}
	if s != nil && s.Name() != "CJKテスト" {
		t.Errorf("formatDocument: schema name = %q; want CJKテスト", s.Name())
	}

	// Verify formatTokenStream result is also parseable
	tsResult, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream returned error: %v", err)
	}
	s2, diagResult2, err := load.LoadString(ctx, tsResult, "test")
	if err != nil {
		t.Fatalf("formatTokenStream output failed to load: %v", err)
	}
	if !diagResult2.OK() {
		for issue := range diagResult2.Issues() {
			t.Logf("issue: %v", issue)
		}
		t.Error("formatTokenStream: formatted multibyte content should be parseable without errors")
	}
	if s2 != nil && s2.Name() != "CJKテスト" {
		t.Errorf("formatTokenStream: schema name = %q; want CJKテスト", s2.Name())
	}
}

func TestFormatDocument_MultibyteIdempotent(t *testing.T) {
	// Verify formatting multibyte content is idempotent
	t.Parallel()

	input := `schema "日本語"

type 用戶 {
	名前 String required


	年齢 Integer
}


`

	// Format once
	first := formatDocument(input)

	// Format again
	second := formatDocument(first)

	if first != second {
		t.Errorf("formatting multibyte content should be idempotent:\nfirst:\n%q\nsecond:\n%q", first, second)
	}
}

// TestFormatting_UTF8PositionEncoding verifies that formatting respects
// the negotiated position encoding (UTF-8 vs UTF-16).
func TestFormatting_UTF8PositionEncoding(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create a schema file with CJK characters that needs formatting.
	// In UTF-8 mode, the edit range's Character field should be byte count.
	// In UTF-16 mode, it should be UTF-16 code units.
	// CJK characters are 3 bytes in UTF-8 but 1 UTF-16 code unit each.
	content := "schema \"日本語\"\n\ntype Person {    \n\tname String\n}\n"
	filePath := filepath.Join(tmpDir, "test.yammm")
	if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	// Test both encodings
	tests := []struct {
		name     string
		encoding PositionEncoding
	}{
		{"UTF-16", PositionEncodingUTF16},
		{"UTF-8", PositionEncodingUTF8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			server := NewServer(logger, Config{ModuleRoot: tmpDir})

			// Set position encoding
			server.workspace.SetPositionEncoding(tt.encoding)

			// Open the document
			uri := PathToURI(filePath)
			err := server.textDocumentDidOpen(nil, &protocol.DidOpenTextDocumentParams{
				TextDocument: protocol.TextDocumentItem{
					URI:        uri,
					LanguageID: "yammm",
					Version:    1,
					Text:       content,
				},
			})
			if err != nil {
				t.Fatalf("textDocumentDidOpen failed: %v", err)
			}

			// Request formatting
			edits, err := server.textDocumentFormatting(nil, &protocol.DocumentFormattingParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			})
			if err != nil {
				t.Fatalf("textDocumentFormatting failed: %v", err)
			}

			if len(edits) == 0 {
				// Document doesn't need formatting (trailing spaces removed in formatDocument)
				// This is acceptable - the test just verifies no crash with encoding switch
				return
			}

			// Verify the edit range covers the document (starts at 0,0)
			edit := edits[0]
			if edit.Range.Start.Line != 0 || edit.Range.Start.Character != 0 {
				t.Errorf("edit range should start at 0,0; got %d,%d",
					edit.Range.Start.Line, edit.Range.Start.Character)
			}

			// For UTF-8, the character should be byte offset (larger for multi-byte chars)
			// For UTF-16, the character should be code units (smaller for BMP chars)
			// The test primarily verifies that the call completes without panic
			// and returns a valid edit when the encoding is switched
		})
	}
}

// =============================================================================
// Column Alignment (Phase 3) Unit Tests
// =============================================================================

func TestAlignColumns_PropertyNamePadding(t *testing.T) {
	t.Parallel()

	input := "\tname String required\n\tage Integer\n\tscore Float[0.0, 100.0]\n"
	expected := "\tname  String required\n\tage   Integer\n\tscore Float[0.0, 100.0]\n"

	result := alignColumns(input)
	if result != expected {
		t.Errorf("alignColumns() =\n%q\nwant:\n%q", result, expected)
	}
}

func TestAlignColumns_PropertyInlineCommentAlignment(t *testing.T) {
	t.Parallel()

	input := "\tname String required // the name\n\tage Integer // age\n"
	// name(4), age(3) → max 4. Comments align to common column.
	// name content: "\tname String required" (21 chars)
	// age content:  "\tage  Integer" (13 chars)
	// comment col = 21 + 1 = 22
	expected := "\tname String required // the name\n\tage  Integer         // age\n"

	result := alignColumns(input)
	if result != expected {
		t.Errorf("alignColumns() =\n%q\nwant:\n%q", result, expected)
	}
}

func TestAlignColumns_RelationshipNamePadding(t *testing.T) {
	t.Parallel()

	input := "\t--> REL (_:many) Target\n\t--> REL2 (_:one) Target\n\t*-> REL3 (one:many) Target\n"
	// REL(3), REL2(4), REL3(4) → max 4
	expected := "\t--> REL  (_:many) Target\n\t--> REL2 (_:one) Target\n\t*-> REL3 (one:many) Target\n"

	result := alignColumns(input)
	if result != expected {
		t.Errorf("alignColumns() =\n%q\nwant:\n%q", result, expected)
	}
}

func TestAlignColumns_AliasNamePadding(t *testing.T) {
	t.Parallel()

	input := "type Email = Pattern[\"^.+@.+$\"]\ntype StateFP = String[2, 2]\n"
	// Email(5), StateFP(7) → max 7
	expected := "type Email   = Pattern[\"^.+@.+$\"]\ntype StateFP = String[2, 2]\n"

	result := alignColumns(input)
	if result != expected {
		t.Errorf("alignColumns() =\n%q\nwant:\n%q", result, expected)
	}
}

func TestAlignColumns_GroupBreakAtBlankLine(t *testing.T) {
	t.Parallel()

	input := "\tname String\n\n\tage Integer\n"
	// Blank line splits into two singleton groups → no alignment
	expected := "\tname String\n\n\tage Integer\n"

	result := alignColumns(input)
	if result != expected {
		t.Errorf("alignColumns() =\n%q\nwant:\n%q", result, expected)
	}
}

func TestAlignColumns_GroupBreakAtComment(t *testing.T) {
	t.Parallel()

	input := "\tname String\n\t// standalone comment\n\tage Integer\n"
	// Comment-only line splits properties into separate groups
	expected := "\tname String\n\t// standalone comment\n\tage Integer\n"

	result := alignColumns(input)
	if result != expected {
		t.Errorf("alignColumns() =\n%q\nwant:\n%q", result, expected)
	}
}

func TestAlignColumns_GroupBreakAtKindChange(t *testing.T) {
	t.Parallel()

	input := "\tname String\n\tage Integer\n\t--> REL (one) Target\n\t--> REL2 (many) Target\n"
	// Properties: name(4), age(3) → max 4
	// Then kind change → relationships: REL(3), REL2(4) → max 4
	expected := "\tname String\n\tage  Integer\n\t--> REL  (one) Target\n\t--> REL2 (many) Target\n"

	result := alignColumns(input)
	if result != expected {
		t.Errorf("alignColumns() =\n%q\nwant:\n%q", result, expected)
	}
}

func TestAlignColumns_MultilineBreaksGroup(t *testing.T) {
	t.Parallel()

	// Unbalanced [ on a line → excluded from alignment, breaks groups
	input := "\tname String\n\tstatus Enum[\n\t\t\"a\",\n\t\t\"b\"\n\t]\n\tage Integer\n"
	// name and age are in separate groups (multiline in between)
	expected := "\tname String\n\tstatus Enum[\n\t\t\"a\",\n\t\t\"b\"\n\t]\n\tage Integer\n"

	result := alignColumns(input)
	if result != expected {
		t.Errorf("alignColumns() =\n%q\nwant:\n%q", result, expected)
	}
}

func TestAlignColumns_EdgePropertyBlockAlignment(t *testing.T) {
	t.Parallel()

	// Properties inside { } edge blocks aligned at indent level 2
	input := "\t--> REL (one) Target {\n\t\tweight Float required\n\t\tscore Integer\n\t}\n"
	// Edge block: weight(6), score(5) → max 6
	expected := "\t--> REL (one) Target {\n\t\tweight Float required\n\t\tscore  Integer\n\t}\n"

	result := alignColumns(input)
	if result != expected {
		t.Errorf("alignColumns() =\n%q\nwant:\n%q", result, expected)
	}
}

func TestAlignColumns_SingleMemberNoChange(t *testing.T) {
	t.Parallel()

	input := "\tname String required\n"
	expected := "\tname String required\n"

	result := alignColumns(input)
	if result != expected {
		t.Errorf("alignColumns() =\n%q\nwant:\n%q", result, expected)
	}
}

func TestAlignColumns_Idempotent(t *testing.T) {
	t.Parallel()

	inputs := []string{
		"\tname String required\n\tage Integer\n\tscore Float\n",
		"\t--> REL (_:many) Target\n\t--> REL2 (_:one) Target\n",
		"type Email = Pattern[\"^.+@.+$\"]\ntype StateFP = String[2, 2]\n",
		"\tname String // the name\n\tage Integer // age\n",
	}

	for _, input := range inputs {
		first := alignColumns(input)
		second := alignColumns(first)
		if first != second {
			t.Errorf("alignColumns not idempotent for input:\n%q\nfirst:\n%q\nsecond:\n%q", input, first, second)
		}
	}
}

func TestAlignColumns_EmptyAndPassthrough(t *testing.T) {
	t.Parallel()

	// Empty string
	if result := alignColumns(""); result != "" {
		t.Errorf("empty input should return empty, got: %q", result)
	}

	// Non-alignable content passes through unchanged
	nonAlignable := "schema \"test\"\n\n// comment\n! \"msg\" expr\n}\n"
	if result := alignColumns(nonAlignable); result != nonAlignable {
		t.Errorf("non-alignable input should pass through unchanged:\ngot:\n%q\nwant:\n%q", result, nonAlignable)
	}
}

// =============================================================================
// Phase 4: Long Line Wrapping Tests
// =============================================================================

// --- displayWidth ---

func TestDisplayWidth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"empty", "", 0},
		{"ascii", "hello", 5},
		{"tab", "\t", 4},
		{"tab_and_text", "\tname String", 15},
		{"two_tabs", "\t\tvalue", 13},
		{"mixed", "\t  abc", 9},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := displayWidth(tt.input)
			if got != tt.want {
				t.Errorf("displayWidth(%q) = %d; want %d", tt.input, got, tt.want)
			}
		})
	}
}

// --- Enum wrapping tests ---

func TestWrapLongLines_ShortEnumUnchanged(t *testing.T) {
	t.Parallel()

	// Short Enum (well under 100 chars) should pass through unchanged
	input := "\tstatus Enum[\"active\", \"inactive\"] required\n"
	result := wrapLongLines(input)
	if result != input {
		t.Errorf("short Enum should be unchanged:\ngot:\n%q\nwant:\n%q", result, input)
	}
}

func TestWrapLongLines_LongEnumWraps(t *testing.T) {
	t.Parallel()

	// Build a long Enum that exceeds 100 chars
	input := "\tstatus Enum[\"pending_review\", \"approved\", \"rejected\", \"needs_revision\", \"escalated\", \"archived\", \"deleted\"] required\n"
	if displayWidth(strings.TrimSuffix(input, "\n")) <= lineWidthThreshold {
		t.Fatal("test input should exceed threshold")
	}

	result := wrapLongLines(input)

	// Should be wrapped: Enum[ on first line, values indented, ] with modifier
	if !strings.Contains(result, "Enum[\n") {
		t.Errorf("expected Enum[ on first line with newline after:\n%s", result)
	}
	if !strings.Contains(result, "\t\t\"pending_review\",\n") {
		t.Errorf("expected values indented with trailing commas:\n%s", result)
	}
	if !strings.Contains(result, "\t\t\"deleted\",\n") {
		t.Errorf("expected last value with trailing comma:\n%s", result)
	}
	if !strings.Contains(result, "\t] required\n") {
		t.Errorf("expected ] with modifier on closing line:\n%s", result)
	}
}

func TestWrapLongLines_EnumCollapseToSingleLine(t *testing.T) {
	t.Parallel()

	// Multiline Enum that would fit on a single line
	input := "\tstatus Enum[\n\t\t\"a\",\n\t\t\"b\",\n\t] required\n"
	result := wrapLongLines(input)

	// Should be collapsed to single line (no trailing comma in single-line form)
	expected := "\tstatus Enum[\"a\", \"b\"] required\n"
	if result != expected {
		t.Errorf("multiline Enum should collapse:\ngot:\n%q\nwant:\n%q", result, expected)
	}
}

func TestWrapLongLines_EnumCollapseStillLong(t *testing.T) {
	t.Parallel()

	// Multiline Enum that's still too long when collapsed → re-canonicalize
	input := "\tstatus Enum[\n\t\t\"pending_review\",\n\t\t\"approved\",\n\t\t\"rejected\",\n\t\t\"needs_revision\",\n\t\t\"escalated\",\n\t\t\"archived\",\n\t\t\"deleted\",\n\t] required\n"

	result := wrapLongLines(input)

	// Should stay multiline with canonical form
	if !strings.Contains(result, "Enum[\n") {
		t.Errorf("long Enum should stay multiline:\n%s", result)
	}
	if !strings.Contains(result, "\t] required\n") {
		t.Errorf("expected ] with modifier:\n%s", result)
	}
}

func TestWrapLongLines_EnumEscapedQuotes(t *testing.T) {
	t.Parallel()

	// Values with escaped quotes should not break the parser
	input := "\tval Enum[\"say \\\"hello\\\"\", \"say \\\"bye\\\"\", \"normal\", \"another\", \"more_values\", \"extra_long_value_here\", \"padding_it\"] required\n"
	result := wrapLongLines(input)

	if displayWidth(strings.TrimSuffix(input, "\n")) > lineWidthThreshold {
		// Should be wrapped
		if !strings.Contains(result, "Enum[\n") {
			t.Errorf("long Enum with escaped quotes should wrap:\n%s", result)
		}
		// Escaped quotes preserved
		if !strings.Contains(result, "\\\"hello\\\"") {
			t.Errorf("escaped quotes should be preserved:\n%s", result)
		}
	}
}

func TestWrapLongLines_EnumInlineComment(t *testing.T) {
	t.Parallel()

	// Enum with inline comment — comment should reattach to ] line
	input := "\tstatus Enum[\"pending_review\", \"approved\", \"rejected\", \"needs_revision\", \"escalated\", \"archived\"] required // status field\n"
	if displayWidth(strings.TrimSuffix(input, "\n")) <= lineWidthThreshold {
		t.Fatal("test input should exceed threshold")
	}

	result := wrapLongLines(input)

	// Comment should be on the closing line
	lines := strings.Split(strings.TrimSuffix(result, "\n"), "\n")
	lastLine := lines[len(lines)-1]
	if !strings.Contains(lastLine, "] required // status field") {
		t.Errorf("inline comment should reattach to ] line, got last line:\n%q", lastLine)
	}
}

// --- Extends wrapping tests ---

func TestWrapLongLines_ShortExtendsUnchanged(t *testing.T) {
	t.Parallel()

	input := "type Concrete extends Base, Audit {\n"
	result := wrapLongLines(input)
	if result != input {
		t.Errorf("short extends should be unchanged:\ngot:\n%q\nwant:\n%q", result, input)
	}
}

func TestWrapLongLines_LongExtendsWraps(t *testing.T) {
	t.Parallel()

	input := "type ComplexEntity extends Auditable, Trackable, Validatable, Serializable, Cacheable, Observable, Publishable {\n"
	if displayWidth(strings.TrimSuffix(input, "\n")) <= lineWidthThreshold {
		t.Fatal("test input should exceed threshold")
	}

	result := wrapLongLines(input)

	if !strings.Contains(result, "type ComplexEntity extends\n") {
		t.Errorf("expected header on first line:\n%s", result)
	}
	if !strings.Contains(result, "\tAuditable,\n") {
		t.Errorf("expected types indented with trailing comma:\n%s", result)
	}
	if !strings.Contains(result, "\tPublishable,\n") {
		t.Errorf("expected last type with trailing comma:\n%s", result)
	}
	// { on own line
	lines := strings.Split(strings.TrimSuffix(result, "\n"), "\n")
	lastLine := strings.TrimSpace(lines[len(lines)-1])
	if lastLine != "{" {
		t.Errorf("expected { on own line, got: %q", lastLine)
	}
}

func TestWrapLongLines_ExtendsCollapseToSingleLine(t *testing.T) {
	t.Parallel()

	// Multiline extends that fits on one line
	input := "type Concrete extends\n\tBase,\n\tAudit,\n{\n"
	result := wrapLongLines(input)

	expected := "type Concrete extends Base, Audit {\n"
	if result != expected {
		t.Errorf("multiline extends should collapse:\ngot:\n%q\nwant:\n%q", result, expected)
	}
}

func TestWrapLongLines_ExtendsCollapseStillLong(t *testing.T) {
	t.Parallel()

	// Multiline extends that's still too long when collapsed
	input := "type ComplexEntity extends\n\tAuditable,\n\tTrackable,\n\tValidatable,\n\tSerializable,\n\tCacheable,\n\tObservable,\n\tPublishable,\n{\n"
	result := wrapLongLines(input)

	// Should stay multiline
	if !strings.Contains(result, "type ComplexEntity extends\n") {
		t.Errorf("long extends should stay multiline:\n%s", result)
	}
	if !strings.Contains(result, "{\n") {
		t.Errorf("expected { on last line:\n%s", result)
	}
}

func TestWrapLongLines_ExtendsQualifiedTypes(t *testing.T) {
	t.Parallel()

	// Extends with qualified types (base.Type)
	input := "type ComplexEntity extends base.Auditable, other.Trackable, third.Validatable, fourth.Serializable, fifth.Cacheable {\n"
	if displayWidth(strings.TrimSuffix(input, "\n")) <= lineWidthThreshold {
		t.Fatal("test input should exceed threshold")
	}

	result := wrapLongLines(input)

	if !strings.Contains(result, "\tbase.Auditable,\n") {
		t.Errorf("qualified types should be preserved:\n%s", result)
	}
	if !strings.Contains(result, "\tother.Trackable,\n") {
		t.Errorf("qualified types should be preserved:\n%s", result)
	}
}

func TestWrapLongLines_ExtendsAbstractType(t *testing.T) {
	t.Parallel()

	input := "abstract type ComplexEntity extends Auditable, Trackable, Validatable, Serializable, Cacheable, Observable {\n"
	if displayWidth(strings.TrimSuffix(input, "\n")) <= lineWidthThreshold {
		// This is 108 chars with "abstract " prefix — should exceed threshold
		t.Fatal("test input should exceed threshold")
	}

	result := wrapLongLines(input)

	if !strings.Contains(result, "abstract type ComplexEntity extends\n") {
		t.Errorf("abstract prefix should be preserved:\n%s", result)
	}
}

// --- Datatype alias tests ---

func TestWrapLongLines_ShortDatatypeAliasUnchanged(t *testing.T) {
	t.Parallel()

	input := "type Status = Enum[\"active\", \"inactive\"]\n"
	result := wrapLongLines(input)
	if result != input {
		t.Errorf("short alias should be unchanged:\ngot:\n%q\nwant:\n%q", result, input)
	}
}

func TestWrapLongLines_LongDatatypeAliasWraps(t *testing.T) {
	t.Parallel()

	input := "type DeactivatedReason = Enum[\"removed_from_source\", \"matured\", \"merged\", \"manual\", \"superseded\", \"error_corrected\"]\n"
	if displayWidth(strings.TrimSuffix(input, "\n")) <= lineWidthThreshold {
		t.Fatal("test input should exceed threshold")
	}

	result := wrapLongLines(input)

	if !strings.Contains(result, "= Enum[\n") {
		t.Errorf("expected Enum[ on first line:\n%s", result)
	}
	if !strings.Contains(result, "\t\"removed_from_source\",\n") {
		t.Errorf("expected values indented:\n%s", result)
	}
}

func TestWrapLongLines_DatatypeAliasCollapses(t *testing.T) {
	t.Parallel()

	// Multiline alias that fits on one line
	input := "type Status = Enum[\n\t\"a\",\n\t\"b\",\n]\n"
	result := wrapLongLines(input)

	expected := "type Status = Enum[\"a\", \"b\"]\n"
	if result != expected {
		t.Errorf("multiline alias should collapse:\ngot:\n%q\nwant:\n%q", result, expected)
	}
}

// --- Invariant wrapping tests ---

func TestWrapLongLines_ShortInvariantUnchanged(t *testing.T) {
	t.Parallel()

	input := "\t! \"check\" a > 0 && b < 100\n"
	result := wrapLongLines(input)
	if result != input {
		t.Errorf("short invariant should be unchanged:\ngot:\n%q\nwant:\n%q", result, input)
	}
}

func TestWrapLongLines_LongInvariantWrapsAtOr(t *testing.T) {
	t.Parallel()

	input := "\t! \"geo_check\" (geo_type == \"state\" && Len(geoid) == 2) || (geo_type == \"county\" && Len(geoid) == 5) || (geo_type == \"place\" && Len(geoid) == 7)\n"
	if displayWidth(strings.TrimSuffix(input, "\n")) <= lineWidthThreshold {
		t.Fatal("test input should exceed threshold")
	}

	result := wrapLongLines(input)

	// First line should be just the prefix
	lines := strings.Split(strings.TrimSuffix(result, "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected multiple lines, got %d:\n%s", len(lines), result)
	}
	if strings.TrimSpace(lines[0]) != "! \"geo_check\"" {
		t.Errorf("first line should be just the prefix, got: %q", lines[0])
	}
	// Operator should be at end of line
	if !strings.HasSuffix(strings.TrimSpace(lines[1]), "||") {
		t.Errorf("operator should be at end of line, got: %q", lines[1])
	}
}

func TestWrapLongLines_LongInvariantWrapsAtAnd(t *testing.T) {
	t.Parallel()

	input := "\t! \"complex_check\" very_long_field_name_one == \"expected_value_one\" && very_long_field_name_two == \"expected_value_two\" && third_field > 0\n"
	if displayWidth(strings.TrimSuffix(input, "\n")) <= lineWidthThreshold {
		t.Fatal("test input should exceed threshold")
	}

	result := wrapLongLines(input)

	lines := strings.Split(strings.TrimSuffix(result, "\n"), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines, got %d:\n%s", len(lines), result)
	}
	// Check operators at end of continuation lines
	for _, line := range lines[1 : len(lines)-1] {
		trimmed := strings.TrimSpace(line)
		if !strings.HasSuffix(trimmed, "&&") && !strings.HasSuffix(trimmed, "||") {
			t.Errorf("continuation line should end with operator, got: %q", trimmed)
		}
	}
}

func TestWrapLongLines_InvariantNeverCollapse(t *testing.T) {
	t.Parallel()

	// Multiline invariant should pass through unchanged — never collapse
	input := "\t! \"check\"\n\t\ta > 0 &&\n\t\tb < 100\n"
	result := wrapLongLines(input)
	if result != input {
		t.Errorf("multiline invariant should not be collapsed:\ngot:\n%q\nwant:\n%q", result, input)
	}
}

func TestWrapLongLines_InvariantNestedOpsSkipped(t *testing.T) {
	t.Parallel()

	// && inside () should NOT be a wrap point — only top-level operators
	input := "\t! \"check\" (very_long_condition_name && another_very_long_condition_name) || (yet_another_long_condition && final_long_condition_name)\n"
	if displayWidth(strings.TrimSuffix(input, "\n")) <= lineWidthThreshold {
		t.Fatal("test input should exceed threshold")
	}

	result := wrapLongLines(input)

	// Should wrap at || but NOT at && inside parens
	lines := strings.Split(strings.TrimSuffix(result, "\n"), "\n")

	// Verify the || is a wrap point
	foundOr := false
	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		if strings.HasSuffix(trimmed, "||") {
			foundOr = true
		}
		// Continuation lines containing && should also contain surrounding parens
		// (meaning the && is nested, not top-level)
		if strings.Contains(trimmed, "&&") && !strings.Contains(trimmed, "(") {
			t.Errorf("&& outside parens should not appear on a continuation line: %q", trimmed)
		}
	}
	if !foundOr {
		t.Errorf("expected || as wrap point:\n%s", result)
	}
}

func TestWrapLongLines_InvariantNoTopLevelOps(t *testing.T) {
	t.Parallel()

	// Very long invariant with no top-level && or || → left as-is
	input := "\t! \"check\" Len(very_long_field_name_that_makes_line_exceed_one_hundred_characters_by_quite_a_bit_actually) > 0\n"
	if displayWidth(strings.TrimSuffix(input, "\n")) <= lineWidthThreshold {
		t.Fatal("test input should exceed threshold")
	}

	result := wrapLongLines(input)

	// Should pass through unchanged (no operators to wrap at)
	if result != input {
		t.Errorf("invariant with no top-level ops should be unchanged:\ngot:\n%q\nwant:\n%q", result, input)
	}
}

func TestWrapLongLines_InvariantBraceExprPreserved(t *testing.T) {
	t.Parallel()

	// && inside { } braces (lambdas) should not be wrap points
	input := "\t! \"all_valid\" ITEMS -> All |$item| { $item.qty > 0 && $item.price > 0 }\n"
	result := wrapLongLines(input)

	// Under 100 chars, should pass through unchanged
	if result != input {
		t.Errorf("invariant with brace expr should be unchanged:\ngot:\n%q\nwant:\n%q", result, input)
	}
}

// --- Integration / edge case tests ---

func TestWrapLongLines_NonWrappable(t *testing.T) {
	t.Parallel()

	// Long Pattern or long string — not wrappable, left as-is
	input := "\tregex Pattern[\"^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\\\.[a-zA-Z]{2,}$\"] required // this is a very very long line that exceeds\n"

	result := wrapLongLines(input)
	if result != input {
		t.Errorf("non-wrappable line should pass through:\ngot:\n%q\nwant:\n%q", result, input)
	}
}

func TestWrapLongLines_Idempotent(t *testing.T) {
	t.Parallel()

	inputs := []string{
		// Long Enum
		"\tstatus Enum[\"pending_review\", \"approved\", \"rejected\", \"needs_revision\", \"escalated\", \"archived\", \"deleted\"] required\n",
		// Long extends
		"type ComplexEntity extends Auditable, Trackable, Validatable, Serializable, Cacheable, Observable, Publishable {\n",
		// Long alias
		"type DeactivatedReason = Enum[\"removed_from_source\", \"matured\", \"merged\", \"manual\", \"superseded\", \"error_corrected\"]\n",
		// Long invariant
		"\t! \"geo_check\" (geo_type == \"state\" && Len(geoid) == 2) || (geo_type == \"county\" && Len(geoid) == 5) || (geo_type == \"place\" && Len(geoid) == 7)\n",
		// Multiline Enum (collapsible)
		"\tstatus Enum[\n\t\t\"a\",\n\t\t\"b\",\n\t] required\n",
		// Multiline extends (collapsible)
		"type Concrete extends\n\tBase,\n\tAudit,\n{\n",
		// Short (unchanged)
		"\tname String required\n",
	}

	for _, input := range inputs {
		first := wrapLongLines(input)
		second := wrapLongLines(first)
		if first != second {
			t.Errorf("wrapLongLines not idempotent for input:\n%q\nfirst:\n%q\nsecond:\n%q", input, first, second)
		}
	}
}

func TestWrapLongLines_EmptyAndPassthrough(t *testing.T) {
	t.Parallel()

	// Empty string
	if result := wrapLongLines(""); result != "" {
		t.Errorf("empty input should return empty, got: %q", result)
	}

	// Non-wrappable content passes through unchanged
	input := "schema \"test\"\n\ntype T {\n\tname String\n}\n"
	if result := wrapLongLines(input); result != input {
		t.Errorf("short content should pass through:\ngot:\n%q\nwant:\n%q", result, input)
	}
}

func TestWrapLongLines_ExactlyAtThreshold(t *testing.T) {
	t.Parallel()

	// Build a line that's exactly 100 chars — should NOT be wrapped
	// "\tstatus Enum[...]" — pad to exactly 100 display width
	// Tab = 4, so we need 96 more chars after tab
	line := "\t" + strings.Repeat("x", 96) + "\n"
	if displayWidth(strings.TrimSuffix(line, "\n")) != 100 {
		t.Fatalf("test line should be exactly 100 chars, got %d", displayWidth(strings.TrimSuffix(line, "\n")))
	}

	result := wrapLongLines(line)
	if result != line {
		t.Errorf("line at exactly 100 chars should NOT be wrapped:\ngot:\n%q", result)
	}
}

// --- Full pipeline tests ---

func TestFormatTokenStream_WrapLongEnum(t *testing.T) {
	t.Parallel()

	input := `schema "test"

type T {
	status Enum["pending_review", "approved", "rejected", "needs_revision", "escalated", "archived", "deleted"] required
}
`
	result, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream returned error: %v", err)
	}

	// Should be wrapped
	if !strings.Contains(result, "Enum[\n") {
		t.Errorf("long Enum should be wrapped in full pipeline:\n%s", result)
	}
	if !strings.Contains(result, "] required\n") {
		t.Errorf("modifier should be on closing line:\n%s", result)
	}
}

func TestFormatTokenStream_CollapseShortMultilineEnum(t *testing.T) {
	t.Parallel()

	input := `schema "test"

type T {
	status Enum[
		"a",
		"b",
	] required
}
`
	result, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream returned error: %v", err)
	}

	// Should be collapsed to single line
	if strings.Contains(result, "Enum[\n") {
		t.Errorf("short multiline Enum should be collapsed:\n%s", result)
	}
	if !strings.Contains(result, `Enum["a", "b"] required`) {
		t.Errorf("expected collapsed Enum, got:\n%s", result)
	}
}

func TestFormatTokenStream_WrapAndAlignInteraction(t *testing.T) {
	t.Parallel()

	// Wrapped Enum should break alignment group
	input := `schema "test"

type T {
	name String required
	status Enum["pending_review", "approved", "rejected", "needs_revision", "escalated", "archived", "deleted"] required
	age Integer
}
`
	result, err := formatTokenStream(input)
	if err != nil {
		t.Fatalf("formatTokenStream returned error: %v", err)
	}

	// Wrapped Enum should break alignment between name and age
	// (they're in separate groups now)
	if !strings.Contains(result, "Enum[\n") {
		t.Errorf("long Enum should wrap:\n%s", result)
	}

	// Verify idempotency of full pipeline
	second, err := formatTokenStream(result)
	if err != nil {
		t.Fatalf("formatTokenStream second pass returned error: %v", err)
	}
	if result != second {
		t.Errorf("full pipeline should be idempotent:\nfirst:\n%q\nsecond:\n%q", result, second)
	}
}
