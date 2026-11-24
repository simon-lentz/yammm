package diag

import (
	"strconv"
	"testing"
	"unicode/utf8"
)

// detailInputs holds text a detail can carry: valid text held as given, and
// each way a byte sequence fails to be UTF-8.
var detailInputs = []string{
	"amount",
	"",
	"naïve ✓",
	"a\uFFFDb",
	`a"b\c`,
	"te\xffxt",
	"abcde\xff",
	"\xc3",
	"é\xffé",
	"\xed\xa0\x80",
	"\xc0\xaf",
	"\xf5\x80\x80\x80",
	"\xe2\x82",
	"ok\x00nul",
}

// wantDetail is the detail rule restated: valid text as given, any other as
// strconv.Quote spells it.
func wantDetail(v string) string {
	for i := 0; i < len(v); {
		r, size := utf8.DecodeRuneInString(v[i:])
		if r == utf8.RuneError && size == 1 {
			return strconv.Quote(v)
		}
		i += size
	}
	return v
}

// Every way a detail reaches an issue holds its value to valid UTF-8: a value
// that is not is held quoted, and any other as given.
func TestIssueBuilder_HoldsEveryDetailValueToValidUTF8(t *testing.T) {
	t.Parallel()
	base := NewIssue(Error, E_UNKNOWN_FIELD, "m").Build()
	for _, v := range detailInputs {
		builds := map[string]Issue{
			"WithDetail":      NewIssue(Error, E_UNKNOWN_FIELD, "m").WithDetail(DetailKeyField, v).Build(),
			"WithDetails":     NewIssue(Error, E_UNKNOWN_FIELD, "m").WithDetails(Detail{Key: DetailKeyField, Value: v}).Build(),
			"TypeField":       NewIssue(Error, E_UNKNOWN_FIELD, "m").WithDetails(TypeField(v, v)...).Build(),
			"RelationField":   NewIssue(Error, E_UNKNOWN_FIELD, "m").WithDetails(RelationField(v, v)...).Build(),
			"WithExpectedGot": NewIssue(Error, E_UNKNOWN_FIELD, "m").WithExpectedGot(v, v).Build(),
			"FromIssue":       FromIssue(base).WithDetail(DetailKeyField, v).Build(),
		}
		for how, issue := range builds {
			if len(issue.Details()) == 0 {
				t.Fatalf("%s(%q): no detail", how, v)
			}
			for _, d := range issue.Details() {
				if !utf8.ValidString(d.Value) {
					t.Errorf("%s(%q): detail %s is not valid UTF-8: %q", how, v, d.Key, d.Value)
				}
				if d.Value != wantDetail(v) {
					t.Errorf("%s(%q): detail %s = %q, want %q", how, v, d.Key, d.Value, wantDetail(v))
				}
			}
		}
	}
}
