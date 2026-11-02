package schema_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

func TestDeclaration_SimpleType(t *testing.T) {
	s := loadOK(t, `schema "test"

type Person {
	name String primary
	age Integer required
}
`)
	if got := s.Name(); got != "test" {
		t.Errorf("s.Name() = %v, want %v", got, "test")
	}

	typ, ok := s.Type("Person")
	if !ok {
		t.Fatalf("ok is false")
	}
	if got := typ.Name(); got != "Person" {
		t.Errorf("typ.Name() = %v, want %v", got, "Person")
	}
	if got := len(slices.Collect(typ.Properties())); got != 2 {
		t.Errorf("len(slices.Collect(typ.Properties())) = %d, want 2", got)
	}
}

func TestDeclaration_MultipleTypes(t *testing.T) {
	s := loadOK(t, `schema "multi"

type Person {
	name String primary
}

type Company {
	title String primary
}
`)
	_, ok := s.Type("Person")
	if !ok {
		t.Fatalf("ok is false")
	}

	_, ok = s.Type("Company")
	if !ok {
		t.Fatalf("ok is false")
	}
}

func TestDeclaration_OptionalProperty(t *testing.T) {
	s := loadOK(t, `schema "test"

type Person {
	name String primary
	nickname String
}
`)
	typ, ok := s.Type("Person")
	if !ok {
		t.Fatalf("ok is false")
	}

	for prop := range typ.Properties() {
		if prop.Name() == "nickname" {
			if !prop.IsOptional() {
				t.Errorf("prop.IsOptional() is false")
			}
		} else {
			if prop.IsOptional() {
				t.Errorf("prop.IsOptional() is true")
			}
		}
	}
}

func TestDeclaration_PrimaryKey(t *testing.T) {
	s := loadOK(t, `schema "test"

type Person {
	id UUID primary
	name String required
}
`)
	typ, ok := s.Type("Person")
	if !ok {
		t.Fatalf("ok is false")
	}

	for prop := range typ.Properties() {
		if prop.Name() == "id" {
			if !prop.IsPrimaryKey() {
				t.Errorf("prop.IsPrimaryKey() is false")
			}
		} else {
			if prop.IsPrimaryKey() {
				t.Errorf("prop.IsPrimaryKey() is true")
			}
		}
	}
}

func TestDeclaration_Relation(t *testing.T) {
	s := loadOK(t, `schema "test"

type Person {
	name String primary
	--> EMPLOYER (one) Company
}

type Company {
	title String primary
}
`)
	typ, ok := s.Type("Person")
	if !ok {
		t.Fatalf("ok is false")
	}

	rels := slices.Collect(typ.Associations())
	if got := len(rels); got != 1 {
		t.Fatalf("len(rels) = %d, want 1", got)
	}
	if got := rels[0].Name(); got != "EMPLOYER" {
		t.Errorf("rels[0].Name() = %v, want %v", got, "EMPLOYER")
	}
}

func TestDeclaration_AbstractType(t *testing.T) {
	s := loadOK(t, `schema "test"

abstract type Base {
	id UUID primary
}

type Person extends Base {
	name String required
}
`)
	base, ok := s.Type("Base")
	if !ok {
		t.Fatalf("ok is false")
	}
	if !base.IsAbstract() {
		t.Errorf("base.IsAbstract() is false")
	}

	person, ok := s.Type("Person")
	if !ok {
		t.Fatalf("ok is false")
	}
	if person.IsAbstract() {
		t.Errorf("person.IsAbstract() is true")
	}
}

func TestDeclaration_PartType(t *testing.T) {
	s := loadOK(t, `schema "test"

part type Wheel {
	size Integer required
}
`)
	wheel, ok := s.Type("Wheel")
	if !ok {
		t.Fatalf("ok is false")
	}
	if !wheel.IsPart() {
		t.Errorf("wheel.IsPart() is false")
	}
}

func TestDeclaration_DataType(t *testing.T) {
	s := loadOK(t, `schema "test"

type Email = Pattern["^[a-z]+@[a-z]+\\.[a-z]+$"]

type Person {
	id String primary
	email Email required
}
`)
	dt, ok := s.DataType("Email")
	if !ok {
		t.Fatalf("ok is false")
	}
	if got := dt.Name(); got != "Email" {
		t.Errorf("dt.Name() = %v, want %v", got, "Email")
	}
}

func TestDeclaration_Documentation(t *testing.T) {
	s := loadOK(t, `/* This is the test schema. */
schema "test"

/* A person entity. */
type Person {
	name String primary
}
`)
	if got := s.Documentation(); got != "This is the test schema." {
		t.Errorf("s.Documentation() = %v, want %v", got, "This is the test schema.")
	}

	typ, ok := s.Type("Person")
	if !ok {
		t.Fatalf("ok is false")
	}
	if got := typ.Documentation(); got != "A person entity." {
		t.Errorf("typ.Documentation() = %v, want %v", got, "A person entity.")
	}
}

func TestDeclaration_Composition(t *testing.T) {
	s := loadOK(t, `schema "test"

type Car {
	model String primary
	*-> WHEELS (one:many) Wheel
}

part type Wheel {
	size Integer required
}
`)
	car, ok := s.Type("Car")
	if !ok {
		t.Fatalf("ok is false")
	}

	rels := slices.Collect(car.Compositions())
	if got := len(rels); got != 1 {
		t.Fatalf("len(rels) = %d, want 1", got)
	}
	if got := rels[0].Name(); got != "WHEELS" {
		t.Errorf("rels[0].Name() = %v, want %v", got, "WHEELS")
	}
	if got := rels[0].Kind(); got != schema.RelationComposition {
		t.Errorf("rels[0].Kind() = %v, want %v", got, schema.RelationComposition)
	}
}

func TestDeclaration_EveryConstraintType(t *testing.T) {
	s := loadOK(t, `schema "test"

type AllConstraints {
	str String[1, 100] required
	num Integer[0, 1000] required
	flt Float[0.0, 1.0] required
	flag Boolean required
	id UUID primary
	created Timestamp required
	born Date required
	status Enum["active", "inactive"] required
	email Pattern["^[a-z]+@[a-z]+\\.[a-z]+$"] required
	embedding Vector[128] required
}
`)
	typ, ok := s.Type("AllConstraints")
	if !ok {
		t.Fatalf("ok is false")
	}
	if got := len(slices.Collect(typ.Properties())); got != 10 {
		t.Errorf("len(slices.Collect(typ.Properties())) = %d, want 10", got)
	}
}

// Underscores, digits after the first character, and digits in a relation name
// are all valid per the grammar.
func TestDeclaration_GrammarValidNames(t *testing.T) {
	loadOK(t, `schema "t"

type Email2 = String

type Foo_Bar {
	id String primary
	first_name String required
	*-> WHEELS (one:many) Wheel
	*-> WHEELS_2 (many) Wheel
}

part type Wheel {
	id String primary
}
`)
}

func TestDeclaration_CrossSchemaInheritance(t *testing.T) {
	dir := t.TempDir()
	writeSchema(t, dir, "base.yammm", "schema \"base\"\n\nabstract type Entity {\n\tid UUID primary\n\tcreated Timestamp required\n}\n")
	writeSchema(t, dir, "derived.yammm", "schema \"derived\"\n\nimport \"./base\" as base\n\ntype Person extends base.Entity {\n\tname String required\n}\n")
	derived, res := schema.Load(t.Context(), filepath.Join(dir, "derived.yammm"), schema.WithModuleRoot(dir))
	if res.HasErrors() {
		t.Fatalf("result: %v", res)
	}

	personType, ok := derived.Type("Person")
	if !ok {
		t.Fatalf("ok is false")
	}

	var propNames []string
	for p := range personType.AllProperties() {
		propNames = append(propNames, p.Name())
	}
	if !slices.Contains(propNames, "id") {
		t.Errorf("should inherit 'id' from Entity")
	}
	if !slices.Contains(propNames, "created") {
		t.Errorf("should inherit 'created' from Entity")
	}
	if !slices.Contains(propNames, "name") {
		t.Errorf("should have own 'name' property")
	}

	imp, ok := derived.ImportByAlias("base")
	if !ok {
		t.Fatalf("should have import with alias 'base'")
	}
	base := imp.Schema()
	if base.IsZero() {
		t.Fatalf("base.IsZero() is true")
	}
	if got := base.Name(); got != "base" {
		t.Errorf("base.Name() = %v, want %v", got, "base")
	}
	if got := imp.ResolvedSourceID(); got != base.SourceID() {
		t.Errorf("imp.ResolvedSourceID() = %v, want %v", got, base.SourceID())
	}
}

func TestDeclaration_CrossSchemaRelation(t *testing.T) {
	dir := t.TempDir()
	writeSchema(t, dir, "base.yammm", "schema \"base\"\n\ntype Organization {\n\tname String primary\n}\n")
	writeSchema(t, dir, "derived.yammm", "schema \"derived\"\n\nimport \"./base\" as base\n\ntype Person {\n\tname String primary\n\t--> EMPLOYER (_:one) base.Organization\n}\n")
	derived, res := schema.Load(t.Context(), filepath.Join(dir, "derived.yammm"), schema.WithModuleRoot(dir))
	if res.HasErrors() {
		t.Fatalf("result: %v", res)
	}

	personType, ok := derived.Type("Person")
	if !ok {
		t.Fatalf("ok is false")
	}

	var employerRel schema.Relation
	for rel := range personType.Associations() {
		if rel.Name() == "EMPLOYER" {
			employerRel = rel
			break
		}
	}
	if employerRel.IsZero() {
		t.Fatalf("should have 'employer' relation")
	}
	if got := employerRel.Target().Name(); got != "Organization" {
		t.Errorf("employerRel.Target().Name() = %v, want %v", got, "Organization")
	}
	if got := employerRel.Target().Qualifier(); got != "base" {
		t.Errorf("employerRel.Target().Qualifier() = %v, want %v", got, "base")
	}
}

func TestDeclaration_Invariant(t *testing.T) {
	s := loadOK(t, `schema "test"

type Person {
	id String primary
	age Integer required

	/* Validates that age is greater than zero */
	! "age must be positive" age > 0
}
`)
	typ, ok := s.Type("Person")
	if !ok {
		t.Fatalf("ok is false")
	}

	invariants := slices.Collect(typ.Invariants())
	if got := len(invariants); got != 1 {
		t.Fatalf("len(invariants) = %d, want 1", got)
	}
	if got := invariants[0].Name(); got != "age must be positive" {
		t.Errorf("invariants[0].Name() = %v, want %v", got, "age must be positive")
	}
	if got := invariants[0].Documentation(); got != "Validates that age is greater than zero" {
		t.Errorf("invariants[0].Documentation() = %v, want %v", got, "Validates that age is greater than zero")
	}
	if invariants[0].Expression() == nil {
		t.Errorf("invariants[0].Expression() is nil")
	}
}

func TestDeclaration_TwoInvariantsOnOneType(t *testing.T) {
	s := loadOK(t, `schema "test"

type Person {
	id String primary
	age Integer required
	! "age must be positive" age > 0
	! "age must be reasonable" age < 150
}
`)
	typ, ok := s.Type("Person")
	if !ok {
		t.Fatalf("ok is false")
	}
	if got := len(slices.Collect(typ.Invariants())); got != 2 {
		t.Errorf("len(slices.Collect(typ.Invariants())) = %d, want 2", got)
	}
}

func TestImportPath_RelativeImportBindsItsFile(t *testing.T) {
	dir := t.TempDir()
	writeSchema(t, dir, "helper.yammm", "schema \"helper\"\n\ntype Base {\n\tid UUID primary\n}\n")
	writeSchema(t, dir, "main.yammm", "schema \"main\"\n\nimport \"./helper\" as helper\n\ntype Person extends helper.Base {\n\tname String required\n}\n")
	s, res := schema.Load(t.Context(), filepath.Join(dir, "main.yammm"), schema.WithModuleRoot(dir))
	if res.HasErrors() {
		t.Fatalf("result: %v", res)
	}

	imports := slices.Collect(s.Imports())
	if got := len(imports); got != 1 {
		t.Fatalf("len(imports) = %d, want 1", got)
	}
	if got := imports[0].Path(); got != "./helper" {
		t.Errorf("imports[0].Path() = %v, want %v", got, "./helper")
	}
}

// A relative import names its file with the extension added before the path is
// resolved, so a link beside the file that carries the import's bare name does
// not decide which schema the import binds.
func TestImportPath_RelativeImportBesideALinkOfItsBareName(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, name := range []string{"helper", "decoy"} {
		writeSchema(t, dir, name+".yammm", "schema \""+name+"\"\n\ntype Base {\n\tid UUID primary\n}\n")
	}
	if err := os.Mkdir(filepath.Join(dir, "decoy"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("decoy", filepath.Join(dir, "helper")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	writeSchema(t, dir, "main.yammm", "schema \"main\"\n\nimport \"./helper\" as helper\n\ntype Person extends helper.Base {\n}\n")

	mainSchema, res := schema.Load(t.Context(), filepath.Join(dir, "main.yammm"), schema.WithModuleRoot(dir))
	if res.HasErrors() {
		t.Fatal(res.Err())
	}
	got := slices.Collect(mainSchema.Imports())[0].ResolvedSourceID()
	want, err := location.SourceIDFromPath(canonicalPath(t, filepath.Join(dir, "helper.yammm")))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("./helper binds %s, want helper.yammm's %s", got, want)
	}
}

func TestImportPath_RelativeImportNotFound(t *testing.T) {
	dir := t.TempDir()
	writeSchema(t, dir, "main.yammm", "schema \"main\"\n\nimport \"./nonexistent\" as alias\n\ntype Person {\n\tname String primary\n}\n")
	s, res := schema.Load(t.Context(), filepath.Join(dir, "main.yammm"), schema.WithModuleRoot(dir))
	if !res.HasErrors() {
		t.Fatalf("res.HasErrors() is false")
	}
	if !s.IsZero() {
		t.Errorf("s.IsZero() is false")
	}
	if !res.HasCode(diag.E_IMPORT_RESOLVE) {
		t.Errorf("should emit E_IMPORT_RESOLVE when import not found")
	}
}

func TestImportPath_ParentDirectoryImport(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o750); err != nil {
		t.Fatalf("%v", err)
	}
	writeSchema(t, dir, "helper.yammm", "schema \"helper\"\n\ntype Base {\n\tid UUID primary\n}\n")
	writeSchema(t, sub, "main.yammm", "schema \"main\"\n\nimport \"../helper\" as helper\n\ntype Person extends helper.Base {\n\tname String required\n}\n")
	s, res := schema.Load(t.Context(), filepath.Join(sub, "main.yammm"), schema.WithModuleRoot(dir))
	if res.HasErrors() {
		t.Fatalf("result: %v", res)
	}
	if s.IsZero() {
		t.Fatalf("s.IsZero() is true")
	}
}

// Two spellings of one file are one schema imported twice.
func TestImportPath_TwoSpellingsOfOneFileAreADuplicateImport(t *testing.T) {
	dir := t.TempDir()
	writeSchema(t, dir, "common.yammm", "schema \"common\"\n\ntype Base {\n\tid UUID primary\n}\n")
	writeSchema(t, dir, "main.yammm", "schema \"main\"\n\nimport \"./common\" as a\nimport \"./common.yammm\" as b\n\ntype Person {\n\tname String primary\n}\n")
	s, res := schema.Load(t.Context(), filepath.Join(dir, "main.yammm"), schema.WithModuleRoot(dir))
	if !s.IsZero() {
		t.Fatalf("schema should be zero when duplicate imports are detected")
	}

	found := false
	for issue := range res.Issues() {
		if issue.Code() == diag.E_DUPLICATE_IMPORT {
			found = true
			if !strings.Contains(issue.Message(), "imported multiple times") {
				t.Errorf("%q does not hold %q", issue.Message(), "imported multiple times")
			}
			if !strings.Contains(issue.Message(), "common.yammm") {
				t.Errorf("%q does not hold %q", issue.Message(), "common.yammm")
			}
		}
	}
	if !found {
		t.Errorf("should emit E_DUPLICATE_IMPORT when two imports resolve to the same schema")
	}
}
