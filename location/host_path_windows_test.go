//go:build windows

package location

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestDropExtendedPrefix_WritesTheFormEveryAPIReads holds the final path's
// prefix handling to os.Readlink's: a drive path and a UNC path lose the
// extended prefix, and a volume GUID path keeps it.
func TestDropExtendedPrefix_WritesTheFormEveryAPIReads(t *testing.T) {
	t.Parallel()

	rows := []struct{ name, in, want string }{
		{"a drive path", `\\?\C:\Users\x`, `C:\Users\x`},
		{"a drive root", `\\?\C:\`, `C:\`},
		{"a UNC path", `\\?\UNC\host\share\x`, `\\host\share\x`},
		{"a volume GUID path", `\\?\Volume{0f7e0a5b-0000-0000-0000-500600000000}\x`, `\\?\Volume{0f7e0a5b-0000-0000-0000-500600000000}\x`},
		{"a path with no prefix", `C:\x`, `C:\x`},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			if got := dropExtendedPrefix(row.in); got != row.want {
				t.Errorf("dropExtendedPrefix(%q) = %q; want %q", row.in, got, row.want)
			}
		})
	}
}

// TestResolveHostPath_ResolvesAJunction holds a path through a junction to the
// identity of the junction's target: the kernel opens the file there, and one
// file has one identity.
func TestResolveHostPath_ResolvesAJunction(t *testing.T) {
	t.Parallel()

	base := diskSpelling(t, t.TempDir())
	target := filepath.Join(base, "real")
	mkdirAll(t, target)
	writeEmpty(t, filepath.Join(target, "x"))
	junction := filepath.Join(base, "j")
	if out, err := exec.CommandContext(t.Context(), "cmd", "/c", "mklink", "/j", junction, target).CombinedOutput(); err != nil {
		t.Skipf("mklink /j: %v\n%s", err, out)
	}
	t.Cleanup(func() { _ = os.Remove(junction) })

	typed := filepath.Join(junction, "x")
	want := filepath.Join(target, "x")
	if got, err := ResolveHostPath(typed); err != nil || got != want {
		t.Errorf("ResolveHostPath(%q) = %q, %v; want %q", typed, got, err, want)
	}
	throughJunction, err := NewCanonicalPath(typed)
	if err != nil {
		t.Fatalf("NewCanonicalPath(%q): %v", typed, err)
	}
	direct, err := NewCanonicalPath(want)
	if err != nil {
		t.Fatalf("NewCanonicalPath(%q): %v", want, err)
	}
	if throughJunction != direct {
		t.Errorf("two spellings of one file give two identities:\n  through the junction %q\n  direct               %q",
			throughJunction.String(), direct.String())
	}
}
