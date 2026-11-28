//go:build windows

package hostpath

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
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

// TestResolve_ResolvesAJunction holds a path through a junction to the
// junction's target: the kernel opens the file there, and one file has one
// resolution.
func TestResolve_ResolvesAJunction(t *testing.T) {
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
	if got, err := Resolve(typed); err != nil || got != want {
		t.Errorf("Resolve(%q) = %q, %v; want %q", typed, got, err, want)
	}
	throughJunction, err := Resolve(typed)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", typed, err)
	}
	direct, err := Resolve(want)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", want, err)
	}
	if throughJunction != direct {
		t.Errorf("two spellings of one file resolve apart:\n  through the junction %q\n  direct               %q",
			throughJunction, direct)
	}
}

// TestENOTDIR_IsAMissingPathOnWindows holds the premise the classifier rests
// on: Go's ENOTDIR on Windows is ERROR_PATH_NOT_FOUND, which reads as a missing
// path, so only the climb to the first existing element can tell a path under
// a file from a missing one.
func TestENOTDIR_IsAMissingPathOnWindows(t *testing.T) {
	t.Parallel()
	if syscall.ENOTDIR != syscall.ERROR_PATH_NOT_FOUND || !errors.Is(syscall.ENOTDIR, fs.ErrNotExist) {
		t.Errorf("syscall.ENOTDIR = %d, ErrNotExist %v; want ERROR_PATH_NOT_FOUND, a missing path", syscall.ENOTDIR, errors.Is(syscall.ENOTDIR, fs.ErrNotExist))
	}
	base := t.TempDir()
	file := filepath.Join(base, "f")
	writeEmpty(t, file)
	if _, err := os.Stat(filepath.Join(file, "x")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("os.Stat under a file = %v; want the host's missing path", err)
	}
	if _, err := Stat(filepath.Join(file, "x")); !errors.Is(err, ErrNotDirectory) {
		t.Errorf("Stat under a file = %v; want ErrNotDirectory", err)
	}
}

// TestResolve_AnExtendedPathResolvesAsItsDrivePath holds the extended form to
// the identity of the path it spells: \\?\ hands the path to the filesystem
// as it is, and a clean absolute path names one file in either form.
func TestResolve_AnExtendedPathResolvesAsItsDrivePath(t *testing.T) {
	t.Parallel()
	base := diskSpelling(t, t.TempDir())
	file := filepath.Join(base, "f")
	writeEmpty(t, file)
	want, err := Resolve(file)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := Resolve(`\\?\` + file); err != nil || got != want {
		t.Errorf("Resolve(%q) = %q, %v; want %q", `\\?\`+file, got, err, want)
	}
}

// substDrive maps a free drive letter to dir with subst, which needs no
// administrator, and returns it, or skips t.
func substDrive(t *testing.T, args func(drive string) []string, undo func(drive string) []string) string {
	t.Helper()
	for c := 'Z'; c >= 'M'; c-- {
		drive := string(c) + ":"
		if _, err := os.Stat(drive + `\`); err == nil {
			continue
		}
		a := args(drive)
		if out, err := exec.CommandContext(t.Context(), a[0], a[1:]...).CombinedOutput(); err != nil { //nolint:gosec // the test's own subst and net use command lines
			t.Skipf("%v: %v\n%s", a, err, out)
		}
		t.Cleanup(func() {
			u := undo(drive)
			exec.Command(u[0], u[1:]...).Run() //nolint:gosec,noctx // best-effort removal of the test's own drive
		})
		return drive
	}
	t.Skip("no free drive letter from M: to Z:")
	return ""
}

// TestResolve_ASubstDriveResolvesToItsTarget holds a path through a SUBST drive
// to the identity of the path it maps: the kernel opens the same file, and one
// file has one resolution.
func TestResolve_ASubstDriveResolvesToItsTarget(t *testing.T) {
	base := diskSpelling(t, t.TempDir())
	file := filepath.Join(base, "x")
	writeEmpty(t, file)
	drive := substDrive(t,
		func(d string) []string { return []string{"subst", d, base} },
		func(d string) []string { return []string{"subst", d, "/d"} })
	checkOneFileOneResolution(t, drive+`\x`, file)
}

// TestResolve_AMappedDriveResolvesToItsShare holds a path through a mapped
// drive to the identity of the share path it stands for. The handle is opened
// through the share, so its final path is the share's: a share of a local
// directory keeps an identity beside the local path's, as a hard link does.
func TestResolve_AMappedDriveResolvesToItsShare(t *testing.T) {
	base := diskSpelling(t, t.TempDir())
	file := filepath.Join(base, "x")
	writeEmpty(t, file)
	vol := filepath.VolumeName(base)
	if len(vol) != 2 || vol[1] != ':' {
		t.Skipf("the temporary directory %q is on no drive letter", base)
	}
	share := `\\localhost\` + vol[:1] + "$"
	drive := substDrive(t,
		func(d string) []string { return []string{"net", "use", d, share} },
		func(d string) []string { return []string{"net", "use", d, "/delete", "/y"} })
	typed := drive + base[len(vol):] + `\x`
	checkOneFileOneResolution(t, typed, share+base[len(vol):]+`\x`)
	got, err := Resolve(typed)
	if err != nil {
		t.Fatal(err)
	}
	if local, err := Resolve(file); err != nil || strings.EqualFold(got, local) {
		t.Errorf("Resolve(%q) = %q, Resolve(%q) = %q, %v; want the share's identity beside the local one", typed, got, file, local, err)
	}
}

// checkOneFileOneResolution fails t unless typed and direct name one file, as
// os.SameFile judges it, and resolve to one host path.
func checkOneFileOneResolution(t *testing.T, typed, direct string) {
	t.Helper()
	a, err := os.Stat(typed)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.Stat(direct)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(a, b) {
		t.Fatalf("%q and %q are two files", typed, direct)
	}
	got, err := Resolve(typed)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", typed, err)
	}
	want, err := Resolve(direct)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", direct, err)
	}
	if got != want {
		t.Errorf("one file resolves apart:\n  %q -> %q\n  %q -> %q", typed, got, direct, want)
	}
}

// TestLookupName_KeepsEveryPrefixedForm holds a lookup to the path os opens: a
// path already in a device or extended form, in either separator or as \??\,
// is looked up as it is, and an absolute path gains the extended prefix once.
func TestLookupName_KeepsEveryPrefixedForm(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, want string }{
		{`C:\x\y`, `\\?\C:\x\y`},
		{`C:\x\..\y`, `\\?\C:\y`},
		{`\\host\share\x`, `\\?\UNC\host\share\x`},
		{`\\?\C:\x`, `\\?\C:\x`},
		{`//?/C:/x`, `//?/C:/x`},
		{`\??\C:\x`, `\??\C:\x`},
		{`\\.\pipe\p`, `\\.\pipe\p`},
		{`//./pipe/p`, `//./pipe/p`},
		{`x\y`, `x\y`},
	} {
		if got := lookupName(tc.in); got != tc.want {
			t.Errorf("lookupName(%q) = %q; want %q", tc.in, got, tc.want)
		}
	}
}
