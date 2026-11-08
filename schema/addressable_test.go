package schema_test

import (
	"slices"
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

func loadAddrSchema(t *testing.T) schema.Schema {
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
func closureTypes(s schema.Schema) []schema.Type {
	var out []schema.Type
	for member := range s.Closure() {
		out = append(out, slices.Collect(member.Types())...)
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
	if !ok || base.Schema().IsZero() {
		t.Fatal("import alias base did not resolve")
	}
	deep, ok := base.Schema().ImportByAlias("deep")
	if !ok || deep.Schema().IsZero() {
		t.Fatal("import alias deep did not resolve")
	}
	probe, ok := deep.Schema().Type("Probe")
	if !ok {
		t.Fatal("deep.Probe not found")
	}

	if tag, ok := schema.AddressableTag(s, probe.ID()); ok {
		t.Errorf("a transitively imported type is addressable as %q", tag)
	}
	// TagForm still renders it, by its declaring schema's name.
	if got := schema.TagForm(s, probe.ID()); got != "deep#Probe" {
		t.Errorf("TagForm(deep.Probe) = %q, want %q", got, "deep#Probe")
	}
}

// TestAddressableTag_RefusesANameTheAliasedSchemaDoesNotDeclare drives the arm a
// caller reaches with a TypeID from another load of an imported path: the schema
// path may be one this schema imports while the name is one that schema never
// declared, and the alias alone must not be taken as proof.
//
// Mutation: dropping the name-exists check in the alias arm turns this red.
func TestAddressableTag_RefusesANameTheAliasedSchemaDoesNotDeclare(t *testing.T) {
	t.Parallel()
	s := loadAddrSchema(t)

	base, ok := s.ImportByAlias("base")
	if !ok || base.Schema().IsZero() {
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

// TestTagForm_DenotesOneTypeWithinALoad pins TagForm's three forms over a
// closure where three schemas declare a Beacon: each type's rendering is its
// own, and a rendering the entry schema resolves resolves back to that type.
func TestTagForm_DenotesOneTypeWithinALoad(t *testing.T) {
	t.Parallel()
	s := loadAddrSchema(t)

	want := map[string]bool{
		"Beacon": true, "base.Basin": true, "base.Beacon": true,
		"deep#Probe": true, "deep#Beacon": true,
	}
	seen := map[string]schema.TypeID{}
	for member := range s.Closure() {
		for typ := range member.Types() {
			name := schema.TagForm(s, typ.ID())
			if other, ok := seen[name]; ok {
				t.Errorf("%s and %s both render as %q", other, typ.ID(), name)
			}
			seen[name] = typ.ID()
			if !want[name] {
				t.Errorf("%s renders as %q, not one of the expected forms", typ.ID(), name)
			}
			if resolved, ok := s.ResolveTypeName(name); ok && resolved.ID() != typ.ID() {
				t.Errorf("%q resolves to %s, want %s", name, resolved.ID(), typ.ID())
			}
		}
	}
	if len(seen) != len(want) {
		t.Errorf("rendered %d types, want %d: %v", len(seen), len(want), seen)
	}
}

// TestTagForm_RendersAnIdentityByItsSchemaPath pins that TagForm reads only an
// identity's schema path, so an identity of a name its schema does not declare,
// as another load's can be, renders in its schema's form.
func TestTagForm_RendersAnIdentityByItsSchemaPath(t *testing.T) {
	t.Parallel()
	s := loadAddrSchema(t)
	base, _ := s.ImportByAlias("base")
	deep, _ := base.Schema().ImportByAlias("deep")

	for want, id := range map[string]schema.TypeID{
		"NoSuchType":      schema.NewTypeID(s.SourceID(), "NoSuchType"),
		"base.NoSuchType": schema.NewTypeID(base.Schema().SourceID(), "NoSuchType"),
		"deep#NoSuchType": schema.NewTypeID(deep.Schema().SourceID(), "NoSuchType"),
	} {
		if got := schema.TagForm(s, id); got != want {
			t.Errorf("TagForm(%s) = %q, want %q", id, got, want)
		}
	}
}
