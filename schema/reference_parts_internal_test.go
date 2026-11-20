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
