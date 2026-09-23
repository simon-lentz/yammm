package location

import (
	"errors"
	"strings"
	"testing"
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
		{"sub/main.yammm/", "sub/main.yammm"},
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
		{"/abs/main.yammm", "must be relative to the synthetic root", ErrAbsolutePathSourceID},
		{"C:/main.yammm", "must be relative to the synthetic root", ErrAbsolutePathSourceID},
		{`C:\main.yammm`, "must be relative to the synthetic root", ErrAbsolutePathSourceID},
		{`\\srv\share\x.yammm`, "must be relative to the synthetic root", ErrAbsolutePathSourceID},
		{"a\xffb.yammm", "must be relative to the synthetic root", ErrInvalidUTF8Path},
		{"", "resolves to the synthetic root itself", nil},
		{".", "resolves to the synthetic root itself", nil},
		{"a/..", "resolves to the synthetic root itself", nil},
		{"..", "names a directory above the synthetic root", nil},
		{"../..", "names a directory above the synthetic root", nil},
		{"a/../..", "names a directory above the synthetic root", nil},
		{"../x/..", "names a directory above the synthetic root", nil},
		{"./C:/x.yammm", "once normalized", ErrAbsolutePathSourceID},
		{"a/../C:/x.yammm", "once normalized", ErrAbsolutePathSourceID},
		{"./c:/x.yammm", "once normalized", ErrAbsolutePathSourceID},
		{"\u212a:/x.yammm", "once normalized", ErrAbsolutePathSourceID},
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
