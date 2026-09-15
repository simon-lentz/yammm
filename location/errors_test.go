package location

import (
	"errors"
	"testing"
)

// TestSentinels_ReturnedByTheirConstructors holds each constructor to the
// sentinel errors.go documents for it, where no other test pins the pair, and
// to no other sentinel.
func TestSentinels_ReturnedByTheirConstructors(t *testing.T) {
	t.Parallel()

	sentinels := []error{
		ErrEmptySourceID,
		ErrAbsolutePathSourceID,
		ErrEmptyPath,
		ErrInvalidUTF8Path,
		ErrAbsoluteJoinElement,
	}
	base := MustCanonicalPath(t.TempDir())
	const bad = "caf\xff.yammm"

	rows := []struct {
		name string
		call func() error
		want error
	}{
		{
			name: "CanonicalizePathForSourceID refuses invalid UTF-8",
			call: func() error { _, err := CanonicalizePathForSourceID(bad); return err },
			want: ErrInvalidUTF8Path,
		},
		{
			name: "ResolveSourcePath refuses invalid UTF-8",
			call: func() error { _, _, err := ResolveSourcePath(bad); return err },
			want: ErrInvalidUTF8Path,
		},
		{
			name: "ResolveSourcePath refuses an empty path",
			call: func() error { _, _, err := ResolveSourcePath(""); return err },
			want: ErrEmptyPath,
		},
		{
			name: "CanonicalPath.Join refuses an element that is not valid UTF-8",
			call: func() error { _, err := base.Join("sub", bad); return err },
			want: ErrInvalidUTF8Path,
		},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			err := row.call()
			if !errors.Is(err, row.want) {
				t.Fatalf("error = %v; want %v", err, row.want)
			}
			var matched []error
			for _, s := range sentinels {
				if errors.Is(err, s) {
					matched = append(matched, s)
				}
			}
			if len(matched) != 1 {
				t.Errorf("error = %v matches %d sentinels %v; want only %v", err, len(matched), matched, row.want)
			}
		})
	}
}
