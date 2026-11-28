package source

import (
	"errors"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/location"
)

// TestNormalizeSyntheticKey holds the key rule to one answer on every host: a
// backslash is refused where one host would read it as a separator and
// another as part of a name, and a key that only cleaning or NFC makes look
// absolute is refused, so every key the rule returns is a fixed point.
func TestNormalizeSyntheticKey(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		key, want string
	}{
		{"main.yammm", "main.yammm"},
		{"a/b/x.yammm", "a/b/x.yammm"},
		{"./main.yammm", "main.yammm"},
		{"a//b/./x.yammm", "a/b/x.yammm"},
		{"a/../main.yammm", "main.yammm"},
		{"../main.yammm", "../main.yammm"},
		{"a/../../x.yammm", "../x.yammm"},
		{"cafe\u0301/x.yammm", "caf\u00e9/x.yammm"},
		{"C:app/x.yammm", "C:app/x.yammm"},
		{"../C:/x.yammm", "../C:/x.yammm"},
		{"sub/C:/x.yammm", "sub/C:/x.yammm"},
	} {
		got, err := NormalizeSyntheticKey(tc.key)
		if err != nil || got != tc.want {
			t.Errorf("NormalizeSyntheticKey(%q) = (%q, %v), want %q", tc.key, got, err, tc.want)
		}
		if again, err := NormalizeSyntheticKey(got); err != nil || again != got {
			t.Errorf("NormalizeSyntheticKey(%q) = (%q, %v), want its own input: a normalized key is a fixed point", got, again, err)
		}
	}

	for _, tc := range []struct {
		key, msg string
		is       error
	}{
		{`a\b.yammm`, "holds a backslash", nil},
		{`sub\main.yammm`, "holds a backslash", nil},
		{`..\x.yammm`, "holds a backslash", nil},
		{"/abs/main.yammm", "must be relative to the synthetic root", location.ErrAbsolutePathSourceID},
		{"C:/main.yammm", "must be relative to the synthetic root", location.ErrAbsolutePathSourceID},
		{`C:\main.yammm`, "must be relative to the synthetic root", location.ErrAbsolutePathSourceID},
		{`\\srv\share\x.yammm`, "must be relative to the synthetic root", location.ErrAbsolutePathSourceID},
		{"a\xffb.yammm", `source key "a\xffb.yammm": location: path is not valid UTF-8`, location.ErrInvalidUTF8Path},
		{"", "resolves to the synthetic root itself", ErrDirectoryKey},
		{".", "resolves to the synthetic root itself", ErrDirectoryKey},
		{"a/..", "resolves to the synthetic root itself", ErrDirectoryKey},
		{"..", "names a directory above the synthetic root", ErrDirectoryKey},
		{"sub/main.yammm/", "so it names a directory, not a file", ErrDirectoryKey},
		{"sub/main.yammm/.", "so it names a directory, not a file", ErrDirectoryKey},
		{"main.yammm//", "so it names a directory, not a file", ErrDirectoryKey},
		{"sub/x/..", "so it names a directory, not a file", ErrDirectoryKey},
		{"../..", "names a directory above the synthetic root", ErrDirectoryKey},
		{"a/../..", "names a directory above the synthetic root", ErrDirectoryKey},
		{"../x/..", "names a directory above the synthetic root", ErrDirectoryKey},
		{"./C:/x.yammm", "once normalized", location.ErrAbsolutePathSourceID},
		{"a/../C:/x.yammm", "once normalized", location.ErrAbsolutePathSourceID},
		{"./c:/x.yammm", "once normalized", location.ErrAbsolutePathSourceID},
		{"\u212a:/x.yammm", "once normalized", location.ErrAbsolutePathSourceID},
	} {
		got, err := NormalizeSyntheticKey(tc.key)
		if err == nil {
			t.Errorf("NormalizeSyntheticKey(%q) = %q, want an error", tc.key, got)
			continue
		}
		if !strings.Contains(err.Error(), tc.msg) {
			t.Errorf("NormalizeSyntheticKey(%q) error = %v, want one saying %q", tc.key, err, tc.msg)
		}
		if tc.is != nil && !errors.Is(err, tc.is) {
			t.Errorf("NormalizeSyntheticKey(%q) error = %v, want it to wrap %v", tc.key, err, tc.is)
		}
	}
}

// absoluteStartText is how the refusal of an absolute-looking name ends.
const absoluteStartText = "; it starts as a file-backed source's path does: with a slash, two backslashes, or a drive letter, a colon and a separator"

// A source key is wrapped by the key rule, where a scheme is no remedy: a
// relative key holding one is joined under the root, and an absolute one is
// still refused.
func TestNormalizeSyntheticKey_TheAbsoluteRefusalGivesNoSchemeAdvice(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"/abs.yammm", "C:/x.yammm", "./C:/x.yammm"} {
		_, err := NormalizeSyntheticKey(key)
		if !errors.Is(err, location.ErrAbsolutePathSourceID) {
			t.Errorf("NormalizeSyntheticKey(%q) = %v, want location.ErrAbsolutePathSourceID", key, err)
			continue
		}
		if msg := err.Error(); !strings.HasSuffix(msg, absoluteStartText) || strings.Contains(msg, "scheme") {
			t.Errorf("NormalizeSyntheticKey(%q): %s", key, msg)
		}
	}
}

// A key is held to the same rule, with its ASCII space kept, and the refusal
// names the key and the character.
func TestNormalizeSyntheticKey_RefusesACharacterThatDoesNotPrint(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"a\x00b.yammm", "a\nb.yammm", "a\tb.yammm", "a\x7fb.yammm", "a\u202eb.yammm", "a\u2066b.yammm", "a\u200db.yammm", "a\ufeffb.yammm", "a\ue000b.yammm", "a\ufffeb.yammm"} {
		_, err := NormalizeSyntheticKey(key)
		if !errors.Is(err, location.ErrUnprintableSourceID) {
			t.Errorf("NormalizeSyntheticKey(%q) = %v, want location.ErrUnprintableSourceID", key, err)
			continue
		}
		if msg := err.Error(); strings.Contains(msg, "must be relative") || !strings.Contains(msg, "U+") {
			t.Errorf("NormalizeSyntheticKey(%q): %s", key, msg)
		}
	}
	for _, key := range []string{"my schema.yammm", "caf\u00e9/r\u00e9sum\u00e9.yammm", "\u65e5\u672c.yammm"} {
		if _, err := NormalizeSyntheticKey(key); err != nil {
			t.Errorf("NormalizeSyntheticKey(%q) = %v, want a key", key, err)
		}
	}
}
