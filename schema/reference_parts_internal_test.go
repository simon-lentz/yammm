package schema

import "testing"

// A reference no source writes comes back whole as its name with no qualifier,
// so a reader that skipped the judge would look up a name no datatype has.
func TestReferenceParts_SplitsOnlyWhatASourceWrites(t *testing.T) {
	for _, tc := range []struct {
		ref, qualifier, name string
		ok                   bool
	}{
		{"T1", "", "T1", true},
		{"lib.Zip", "lib", "Zip", true},
		{"Lib.Zip_2", "Lib", "Zip_2", true},
		{".T1", "", ".T1", false},
		{"T1.", "", "T1.", false},
		{"lib..Zip", "", "lib..Zip", false},
		{"a.b.C", "", "a.b.C", false},
		{"1lib.Zip", "", "1lib.Zip", false},
		{"li-b.Zip", "", "li-b.Zip", false},
		{"lib.Zi[p", "", "lib.Zi[p", false},
		{"lib.Zi{p", "", "lib.Zi{p", false},
		{"lib.Zi`p", "", "lib.Zi`p", false},
		{"lib.Zi@p", "", "lib.Zi@p", false},
		{"lib.Zi/p", "", "lib.Zi/p", false},
		{"lib.Zi:p", "", "lib.Zi:p", false},
		{"", "", "", false},
	} {
		qualifier, name, ok := referenceParts(tc.ref)
		if qualifier != tc.qualifier || name != tc.name || ok != tc.ok {
			t.Errorf("referenceParts(%q) = %q, %q, %t; want %q, %q, %t", tc.ref, qualifier, name, ok, tc.qualifier, tc.name, tc.ok)
		}
	}
}

// The judge reads the fault NewAliasConstraint recorded and parses no name
// again, so judging a reference reads two fields at each call.
func TestConstraintFault_ReadsTheRecordedReferenceFault(t *testing.T) {
	if _, found := constraintFault(AliasConstraint{dataTypeName: "T1", badName: true}); !found {
		t.Error("the judge accepts a reference whose recorded fault is set")
	}
	if fault, found := constraintFault(NewAliasConstraint("T1", nil)); found {
		t.Errorf("the judge refuses the reference T1: %s", fault)
	}
}
