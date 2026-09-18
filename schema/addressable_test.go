package schema_test

import (
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// The addressability rule and its inverse.
//
// AddressableTag renders the name an entry schema denotes a type by and
// ResolveTypeName reads one back. The two are halves of one contract, and a
// caller that decides eligibility with one and resolves with the other is
// wrong the moment they disagree. These tests hold them together.

const addrEntrySource = `schema "entry"

import "base.yammm" as base

type Beacon {
	id String primary
}
`

const addrBaseSource = `schema "base"

import "deep.yammm" as deep

type Basin {
	id String primary
}

type Beacon {
	id String primary
}
`

const addrDeepSource = `schema "deep"

type Probe {
	id String primary
}

type Beacon {
	id String primary
}
`

func loadAddrSchema(t *testing.T) *schema.Schema {
	t.Helper()
	s, result := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{
		"entry.yammm": []byte(addrEntrySource),
		"base.yammm":  []byte(addrBaseSource),
		"deep.yammm":  []byte(addrDeepSource),
	}, "entry.yammm", ".", schema.WithSourcesOnly(true))
	if result.HasErrors() {
		t.Fatalf("load addressability fixture: %s", result)
	}
	return s
}

// closureTypes returns every type of every closure member, so the sweep below
// reaches the transitively imported ones no entry-relative name can reach.
func closureTypes(s *schema.Schema) []*schema.Type {
	var out []*schema.Type
	for _, member := range s.Closure() {
		out = append(out, member.TypesSlice()...)
	}
	return out
}

// TestAddressableTag_IsResolveTypeNameInverse is the contract
// [schema.AddressableTag]'s godoc states, swept over a closure that holds all
// three origins and one bare name three schemas declare.
//
// Mutation: dropping the FindImportAlias arm, or returning a bare name for a
// transitively imported type, turns this red on the refused side.
func TestAddressableTag_IsResolveTypeNameInverse(t *testing.T) {
	t.Parallel()
	s := loadAddrSchema(t)

	var accepted, refused int
	for _, typ := range closureTypes(s) {
		id := typ.ID()
		tag, ok := schema.AddressableTag(s, id)
		if ok != schema.Addressable(s, id) {
			t.Errorf("%s: AddressableTag reports %t, Addressable reports %t", id, ok, schema.Addressable(s, id))
		}
		if !ok {
			refused++
			if tag != "" {
				t.Errorf("%s: refused but carries tag %q", id, tag)
			}
			// No entry-relative name may denote it, under either spelling.
			for _, spelling := range []string{id.Name(), "base." + id.Name(), "deep." + id.Name()} {
				if got, found := s.ResolveTypeName(spelling); found && got.ID() == id {
					t.Errorf("%s: refused, but %q resolves to it", id, spelling)
				}
			}
			continue
		}
		accepted++
		got, found := s.ResolveTypeName(tag)
		if !found {
			t.Errorf("%s: tag %q does not resolve", id, tag)
			continue
		}
		if got.ID() != id {
			t.Errorf("%s: tag %q resolves to %s", id, tag, got.ID())
		}
	}

	if accepted == 0 || refused == 0 {
		t.Fatalf("fixture is vacuous: %d accepted, %d refused; the sweep must exercise both sides", accepted, refused)
	}
}

// TestAddressableTag_RefusesATransitivelyImportedType names the one origin the
// rule exists to refuse, so the sweep above cannot pass by accepting
// everything.
func TestAddressableTag_RefusesATransitivelyImportedType(t *testing.T) {
	t.Parallel()
	s := loadAddrSchema(t)

	base, ok := s.ImportByAlias("base")
	if !ok || base.Schema() == nil {
		t.Fatal("import alias base did not resolve")
	}
	deep, ok := base.Schema().ImportByAlias("deep")
	if !ok || deep.Schema() == nil {
		t.Fatal("import alias deep did not resolve")
	}
	probe, ok := deep.Schema().Type("Probe")
	if !ok {
		t.Fatal("deep.Probe not found")
	}

	if tag, ok := schema.AddressableTag(s, probe.ID()); ok {
		t.Errorf("a transitively imported type is addressable as %q", tag)
	}
	// TagForm still renders it, and that difference is the reason both exist.
	if got := schema.TagForm(s, probe.ID()); got != "Probe" {
		t.Errorf("TagForm(deep.Probe) = %q, want the bare name %q", got, "Probe")
	}
}

// TestAddressableTag_RefusesANameTheAliasedSchemaDoesNotDeclare drives the arm a
// caller reaches by building a TypeID itself. [schema.NewTypeID] is exported, so
// the schema path may be one this schema imports while the name is one that
// schema never declared, and the alias alone must not be taken as proof.
//
// Mutation: dropping the name-exists check in the alias arm turns this red.
func TestAddressableTag_RefusesANameTheAliasedSchemaDoesNotDeclare(t *testing.T) {
	t.Parallel()
	s := loadAddrSchema(t)

	base, ok := s.ImportByAlias("base")
	if !ok || base.Schema() == nil {
		t.Fatal("import alias base did not resolve")
	}
	// The path resolves through the alias; the name is declared nowhere.
	forged := schema.NewTypeID(base.Schema().SourceID(), "NoSuchType")
	if _, ok := base.Schema().Type("NoSuchType"); ok {
		t.Fatal("fixture is vacuous: base declares NoSuchType")
	}

	if tag, ok := schema.AddressableTag(s, forged); ok {
		t.Errorf("a name the aliased schema does not declare is addressable as %q", tag)
	}
	if schema.Addressable(s, forged) {
		t.Error("Addressable accepted a name the aliased schema does not declare")
	}
}

// TestAddressableTag_SeparatesOneNameThreeSchemasDeclare pins that the rule
// does not merge same-named types: the local Beacon and the directly imported
// one both address, under different tags, and the transitive one does not.
func TestAddressableTag_SeparatesOneNameThreeSchemasDeclare(t *testing.T) {
	t.Parallel()
	s := loadAddrSchema(t)

	local, ok := s.Type("Beacon")
	if !ok {
		t.Fatal("local Beacon not found")
	}
	base, _ := s.ImportByAlias("base")
	imported, ok := base.Schema().Type("Beacon")
	if !ok {
		t.Fatal("base.Beacon not found")
	}
	deepImp, _ := base.Schema().ImportByAlias("deep")
	transitive, ok := deepImp.Schema().Type("Beacon")
	if !ok {
		t.Fatal("deep.Beacon not found")
	}

	localTag, localOK := schema.AddressableTag(s, local.ID())
	importedTag, importedOK := schema.AddressableTag(s, imported.ID())
	if !localOK || !importedOK {
		t.Fatalf("an addressable Beacon was refused: local %t, imported %t", localOK, importedOK)
	}
	if localTag == importedTag {
		t.Errorf("two addressable types share tag %q", localTag)
	}
	if _, ok := schema.AddressableTag(s, transitive.ID()); ok {
		t.Error("the transitively imported Beacon is addressable")
	}
}
