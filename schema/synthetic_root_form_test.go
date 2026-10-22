package schema_test

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/schema"
)

// syntheticRootRows holds each root with the form the rule gives it: RFC
// 3986's scheme "://" reg-name path-abempty, with no query, no fragment, no
// empty, "." or ".." authority, no percent escape, no one-letter or "string"
// scheme and no path above the authority, its path cleaned and its text in
// NFC, and any non-ASCII character that is no space or control character
// admitted. An empty form is a refusal; why names the reason.
var syntheticRootRows = []struct {
	root, form, why string
}{
	// In use: rdata's constant, the generator's recipe, this module's tests and documents.
	{"embedded://rdata", "embedded://rdata", "scheme, reg-name"},
	{"embedded://your-app", "embedded://your-app", `"-" is unreserved`},
	{"embedded://app", "embedded://app", "scheme, reg-name"},
	{"embedded://root", "embedded://root", "scheme, reg-name"},
	{"embedded://gen", "embedded://gen", "scheme, reg-name"},
	{"test://refs", "test://refs", "scheme, reg-name"},
	{"test://lib", "test://lib", "scheme, reg-name"},
	{"test://unit/fixtures", "test://unit/fixtures", "path-abempty"},
	{"x-y.z+1://app", "x-y.z+1://app", `scheme = ALPHA *( ALPHA / DIGIT / "+" / "-" / "." )`},
	{"embedded://root/sub", "embedded://root/sub", "path-abempty"},

	// The one form: scheme case, slashes, dot segments, NFC.
	{"EMBEDDED://App", "embedded://App", "a scheme is case-insensitive; the authority keeps its case"},
	{"embedded://app/", "embedded://app", "an empty path"},
	{"embedded://app///", "embedded://app", "empty segments"},
	{"embedded://app//sub", "embedded://app/sub", "an empty segment"},
	{"embedded://app/./sub/x/..", "embedded://app/sub", "dot segments are removed"},
	{"embedded://cafe\u0301/re\u0301sume\u0301", "embedded://caf\u00e9/r\u00e9sum\u00e9", "ucschar, in NFC"},
	{"embedded://\u65e5\u672c/\u8a9e", "embedded://\u65e5\u672c/\u8a9e", "ucschar"},

	// RFC 3986's reg-name and segment characters: each mark, and the letters
	// and digits at both ends of their ranges; and the shortest scheme.
	{"embedded://A-Za-z0-9._~", "embedded://A-Za-z0-9._~", "unreserved"},
	{"embedded://a!$&'()*+,;=b", "embedded://a!$&'()*+,;=b", "sub-delims"},
	{"embedded://app/a!$&'()*+,;=b", "embedded://app/a!$&'()*+,;=b", "sub-delims in a segment"},
	{"embedded://app/a:b@c", "embedded://app/a:b@c", `pchar admits ":" and "@"`},
	{"embedded://app/C:/x", "embedded://app/C:/x", `pchar admits ":"; the identity is no host path`},
	{"embedded://app/...", "embedded://app/...", "a segment of dots that is no dot segment"},
	{"embedded://...", "embedded://...", "a reg-name of dots that is no dot segment"},
	{"ab://x", "ab://x", "the shortest scheme a root takes"},

	// No form: the scheme and the authority.
	{"", "", "no scheme"},
	{"embedded:", "", `no "//"`},
	{"embedded:app", "", `no "//"`},
	{"embedded://", "", "a root names an authority: narrowed from reg-name, which may be empty"},
	{"embedded:///", "", "an empty authority"},
	{"embedded:///app", "", "an empty authority"},
	{".", "", "no scheme"},
	{"app", "", "no scheme"},
	{"://app", "", "an empty scheme"},
	{"1x://app", "", "a scheme starts with a letter"},
	{"em bedded://app", "", "a space in the scheme"},
	{"em_bedded://app", "", `"_" is no scheme character`},
	{"em~b://app", "", `"~" is no scheme character`},
	{"\u212ax://app", "", "a scheme is ASCII"},
	{"C://app", "", "narrowed: a one-letter scheme reads as a drive on a host that has them"},
	{"c://app", "", "narrowed: a one-letter scheme"},
	{"string://a", "", "narrowed: string:// is the identity LoadString mints"},
	{"STRING://a", "", "narrowed: string://, in any case"},
	{"embedded://.", "", "narrowed: an authority that is a dot segment"},
	{"embedded://..", "", "narrowed: an authority that is a dot segment"},
	{"embedded://./sub", "", "narrowed: an authority that is a dot segment"},
	{"embedded://app:8080", "", `narrowed: no port; ":" is no reg-name character`},
	{"embedded://a:b", "", `":" is no reg-name character, and "b" is no port`},
	{"embedded://user@app", "", `narrowed: no userinfo; "@" is no reg-name character`},
	{"embedded://[::1]", "", "narrowed: no IP literal"},

	// No form: a query, a fragment, and a sample of the characters the rule
	// refuses, some of them narrowed from what RFC 3986 or 3987 admits.
	{"embedded://app?q", "", "a query"},
	{"embedded://app#f", "", "a fragment"},
	{"embedded://a?b#c", "", "a query and a fragment"},
	{"embedded://app/p?a/../b", "", "a query"},
	{"embedded://app/p#a/../b", "", "a fragment"},
	{"embedded://app?x/../../y", "", "a query"},
	{"embedded://a b", "", "a space"},
	{"embedded://app/a b", "", "a space"},
	{"embedded://app ", "", "a space"},
	{" embedded://app", "", "a space"},
	{"embedded://app\x00", "", "a control character"},
	{"embedded://app/sub\x00", "", "a control character"},
	{"embedded://a\tb", "", "a control character"},
	{"embedded://app\n", "", "a control character"},
	{"embedded://app\x7f", "", "a control character"},
	{"embedded://a\u0085b", "", "a C1 control character"},
	{"embedded://a\u00a0b", "", "narrowed from ucschar: a space"},
	{"embedded://a\u2028b", "", "narrowed from ucschar: a line separator is a space"},
	{"embedded://a\u3000b", "", "narrowed from ucschar: a space"},
	{`embedded://app\sub`, "", "a backslash"},
	{`embedded://a\pp`, "", "a backslash"},
	{`embedded://app/sub\x`, "", "a backslash"},
	{`embedded://a"b`, "", `'"'`},
	{"embedded://a<b>", "", `"<" and ">"`},
	{"embedded://a^b", "", `"^"`},
	{"embedded://a`b", "", "a grave accent"},
	{"embedded://a\u1fefb", "", "U+1FEF, which NFC makes a grave accent"},
	{"embedded://a{b}", "", `"{" and "}"`},
	{"embedded://a|b", "", `"|"`},
	{"embedded://app/a[b]", "", `"[" and "]"`},
	{"embedded://a%41b", "", "narrowed: no percent escape, which would give one root two spellings"},
	{"embedded://app/a%2Fb", "", "narrowed: no percent escape"},
	{"embedded://app/%", "", `"%" alone is no escape`},
	{"embedded://a\xffb", "", "not valid UTF-8"},
	{"embedded://app/\xc3", "", "not valid UTF-8"},

	// No form: a path that climbs above its authority.
	{"embedded://app/..", "", "a path above the authority"},
	{"embedded://app/../x", "", "a path above the authority"},
	{"embedded://app/sub/../../x", "", "a path above the authority"},
}

func loadUnderRoot(t *testing.T, root string) (*schema.Schema, string) {
	t.Helper()
	s, res := schema.LoadSourcesWithEntry(t.Context(), syntheticRootSources(), "main.yammm", "",
		schema.WithSourcesOnly(true), schema.WithSyntheticRoot(root))
	if res.HasErrors() {
		return nil, firstError(res)
	}
	return s, ""
}

// A synthetic root is judged whole: a root with a form loads under that form,
// which loads as itself, and a root with none is refused before any source is read.
func TestWithSyntheticRoot_IsJudgedWhole(t *testing.T) {
	t.Parallel()
	for _, row := range syntheticRootRows {
		t.Run("root="+row.root, func(t *testing.T) {
			t.Parallel()
			s, msg := loadUnderRoot(t, row.root)
			if row.form == "" {
				if s != nil {
					t.Fatalf("the root loaded as %q; want a refusal (%s)", s.ModuleRoot(), row.why)
				}
				if !strings.Contains(msg, "invalid synthetic root") {
					t.Errorf("refused with %q; want the invalid-root error", msg)
				}
				return
			}
			if s == nil {
				t.Fatalf("refused: %s; want the form %q (%s)", msg, row.form, row.why)
			}
			if got := s.ModuleRoot(); got != row.form {
				t.Errorf("the root's form is %q; want %q (%s)", got, row.form, row.why)
			}
			if got, want := s.SourceID().String(), row.form+"/main.yammm"; got != want {
				t.Errorf("the entry identity is %q; want %q", got, want)
			}
			again, msg := loadUnderRoot(t, row.form)
			if again == nil || again.ModuleRoot() != row.form {
				t.Errorf("the form %q does not load as itself: %s", row.form, msg)
			}
		})
	}
}

// rootCharacterAdmitted states RFC 3986's character sets for the text after a
// root's scheme: unreserved and sub-delims in the authority, pchar in a path
// segment. Past ASCII it admits a letter, mark, number, punctuation or symbol.
func rootCharacterAdmitted(r rune, inPath bool) bool {
	const unreserved = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"
	const subDelims = "!$&'()*+,;="
	switch {
	case r >= utf8.RuneSelf:
		return unicode.In(r, unicode.L, unicode.M, unicode.N, unicode.P, unicode.S)
	case strings.ContainsRune(unreserved+subDelims, r):
		return true
	}
	return inPath && (r == ':' || r == '@')
}

// Every ASCII character but "/", and a sample past ASCII, is admitted or
// refused at each position as rootCharacterAdmitted states the rule.
func TestWithSyntheticRoot_AdmitsRFC3986sCharactersAlone(t *testing.T) {
	t.Parallel()
	runes := []rune{0x80, 0x85, 0x9f, 0xa0, 0xa1, 0xad, 0xe9, 0x2003, 0x2028, 0x2029, 0x200d, 0x200e, 0x202e, 0x2066, 0x3000, 0x65e5, 0xe000, 0xfeff, 0xfffd, 0xfffe, 0x1f600, 0xe0001, 0x10ffff}
	for r := range rune(utf8.RuneSelf) {
		runes = append(runes, r)
	}
	for _, r := range runes {
		if r == '/' {
			continue
		}
		for _, inPath := range []bool{false, true} {
			root := "embedded://a" + string(r) + "b"
			if inPath {
				root = "embedded://app/a" + string(r) + "b"
			}
			s, msg := loadUnderRoot(t, root)
			if want := rootCharacterAdmitted(r, inPath); (s != nil) != want {
				t.Errorf("root %q (U+%04X, in the path: %t): loaded %t, want %t: %s", root, r, inPath, s != nil, want, msg)
			}
		}
	}
}

// Two roots that load mint one identity for a key only when they have one form.
func TestWithSyntheticRoot_OneIdentityMeansOneForm(t *testing.T) {
	t.Parallel()
	formOf := map[string]string{}
	for _, row := range syntheticRootRows {
		s, _ := loadUnderRoot(t, row.root)
		if s == nil {
			continue
		}
		id := s.SourceID().String()
		if form, seen := formOf[id]; seen && form != s.ModuleRoot() {
			t.Errorf("the forms %q and %q both mint %q", form, s.ModuleRoot(), id)
		}
		formOf[id] = s.ModuleRoot()
		if !strings.HasSuffix(id, "/main.yammm") || strings.TrimSuffix(id, "/main.yammm") != s.ModuleRoot() {
			t.Errorf("root %q: the identity %q is not its form %q joined to the key", row.root, id, s.ModuleRoot())
		}
	}
	if len(formOf) < 15 {
		t.Fatalf("only %d roots loaded", len(formOf))
	}
}

// A refused root is told why in the root rule's own words, and is never
// advised to take a form the rule refuses.
func TestWithSyntheticRoot_RefusalNamesItsReason(t *testing.T) {
	t.Parallel()
	for root, want := range map[string]string{
		"app":                 "has the form scheme://authority",
		"embedded://":         "names no authority",
		"C://app":             "one letter, which reads as a drive",
		"string://a":          "names a LoadString source",
		"embedded://.":        `the dot segment "."`,
		"embedded://..":       `the dot segment ".."`,
		"embedded://app:8080": `authority holds ':'; a root names no user and no port`,
		"embedded://user@app": `authority holds '@'`,
		"embedded://app?q":    `the root holds '?'`,
		"embedded://a?b#c":    `the root holds '?'`,
		"embedded://a b/c#d":  `the root holds ' '`,
		"embedded://app/p#f":  `the root holds '#'`,
		"embedded://a b":      `the root holds ' '`,
		"embedded://app\x00":  `the root holds '\x00'`,
		`embedded://app\sub`:  `the root holds '\\'`,
		"embedded://app/%41":  `the root holds '%'`,
		"embedded://a\u1fefb": "the root holds '`'",
		"embedded://a\xffb":   "not valid UTF-8",
		"embedded://app/../x": "climbs above its authority",
	} {
		_, msg := loadUnderRoot(t, root)
		if want != "" && !strings.Contains(msg, want) {
			t.Errorf("root %q is refused with %q; want %q", root, msg, want)
		}
		if strings.Contains(msg, "use a scheme prefix") {
			t.Errorf("root %q is advised to use a scheme prefix: %q", root, msg)
		}
	}
}

// A refused character outside the authority's ":" and "@" gets a message that
// states the ASCII part of the character rule.
func TestWithSyntheticRoot_TheCharacterRefusalStatesTheRule(t *testing.T) {
	t.Parallel()
	const rule = "after its scheme a root holds letters, digits, / and the marks - . _ ~ ! $ & ' ( ) * + , ; =, with : and @ in its path alone, and printable characters past ASCII, so no space, control or format character, backslash, percent escape, query or fragment"
	if _, msg := loadUnderRoot(t, "embedded://a b"); !strings.Contains(msg, "the root holds ' '; "+rule) {
		t.Errorf("refused with %q; want the rule %q", msg, rule)
	}
}
