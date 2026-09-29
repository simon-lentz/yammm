package typetag

import (
	"errors"
	"testing"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/internal/parse"
)

// TestReservedVocabularyMatchesTheGrammar holds this package's two reserved
// maps equal to [parse.ReservedKeywords], which the parser composes from the
// maps its own lookahead groups are checked against, and holds the two maps to
// the split the grammar's LC_WORD and UC_WORD branches make. Both lists are
// hand-written, in different packages, and only this test couples them.
func TestReservedVocabularyMatchesTheGrammar(t *testing.T) {
	grammar := parse.ReservedKeywords()

	for kw := range grammar {
		if !isReservedName(kw) {
			t.Errorf("the grammar refuses %q where either case is admitted; this package accepts it", kw)
		}
	}
	for kw := range datatypeKeywords {
		if !grammar[kw] {
			t.Errorf("this package refuses the datatype name %q; the grammar does not", kw)
		}
	}
	for kw := range reservedLC {
		if !grammar[kw] {
			t.Errorf("this package refuses the lowercase keyword %q; the grammar does not", kw)
		}
	}

	// The maps partition the vocabulary by case, and no behaviour can prove
	// it: the alias half looks a name up in the union, and the type half
	// refuses a lower-case name before it consults a vocabulary at all.
	for kw := range datatypeKeywords {
		if first, _ := utf8.DecodeRuneInString(kw); !isUpperASCII(first) {
			t.Errorf("datatypeKeywords holds %q, which is not a UC_WORD", kw)
		}
		if reservedLC[kw] {
			t.Errorf("%q is in both maps, which must be disjoint", kw)
		}
	}
	for kw := range reservedLC {
		if first, _ := utf8.DecodeRuneInString(kw); isUpperASCII(first) {
			t.Errorf("reservedLC holds %q, which is a UC_WORD", kw)
		}
	}
}

// TestValidate_RefusesEveryReservedSpelling drives both halves of a qualified
// tag against the grammar's whole vocabulary. A datatype name moved into the
// lowercase map fails here; the reverse cannot, because a lower-case name is
// refused before any vocabulary is read. The maps' own split is pinned by
// [TestReservedVocabularyMatchesTheGrammar].
func TestValidate_RefusesEveryReservedSpelling(t *testing.T) {
	for kw := range parse.ReservedKeywords() {
		t.Run(kw, func(t *testing.T) {
			assertDetail(t, kw+".Person", DetailAliasReserved)

			first, _ := utf8.DecodeRuneInString(kw)
			if isUpperASCII(first) {
				assertDetail(t, kw, DetailReservedDatatype)
				assertDetail(t, "mod."+kw, DetailReservedDatatype)
				return
			}
			// A lowercase keyword never reaches the type half's reserved
			// check: that half refuses a name not starting with an uppercase
			// ASCII letter before it looks the name up.
			assertDetail(t, kw, DetailMustStartUpper)
			assertDetail(t, "mod."+kw, DetailMustStartUpper)
		})
	}
}

func assertDetail(t *testing.T, tag, want string) {
	t.Helper()
	err := Validate(tag)
	if err == nil {
		t.Fatalf("Validate(%q) = nil, want detail %q", tag, want)
	}
	var tagErr *Error
	if !errors.As(err, &tagErr) {
		t.Fatalf("Validate(%q) returned %T, want *Error", tag, err)
	}
	if tagErr.Detail != want {
		t.Errorf("Validate(%q) detail = %q, want %q", tag, tagErr.Detail, want)
	}
}
