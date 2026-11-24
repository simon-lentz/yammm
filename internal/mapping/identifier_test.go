package mapping

import "testing"

func TestIdentifierProblem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want string
	}{
		{"book_catalog", ""},
		{"_private", ""},
		{"mixed123_OK", ""},
		{"Matches", ""},
		{"zone_size", ""},
		{"", reasonEmpty},
		{"9lives", reasonCharacters},
		{"geo-regions", reasonCharacters},
		{"Hello World", reasonCharacters},
		{"café", reasonCharacters},
		{"MATCH", reasonReserved},
		{"match", reasonReserved},
		{"Drop", reasonReserved},
	}
	for _, tt := range tests {
		if got := IdentifierProblem(tt.name); got != tt.want {
			t.Errorf("IdentifierProblem(%q) = %q, want %q", tt.name, got, tt.want)
		}
		if got, want := IsIdentifier(tt.name), tt.want == ""; got != want {
			t.Errorf("IsIdentifier(%q) = %v, want %v", tt.name, got, want)
		}
	}
}
