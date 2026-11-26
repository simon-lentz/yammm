package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// On Linux a removed working directory with PWD unset has no name, so a path
// that climbs out of it does not resolve: the check leaves it to the read,
// which reports it, and refuses no stream.
func TestCheckRewrite_LeavesAPathThatDoesNotResolveToTheRead(t *testing.T) {
	wd := filepath.Join(t.TempDir(), "gone")
	if err := os.Mkdir(wd, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(wd)
	t.Setenv("PWD", "")
	if err := os.Remove(wd); err != nil {
		t.Fatal(err)
	}
	if _, err := HostPath("../s.yammm"); err == nil {
		t.Fatal("../s.yammm resolves from a removed working directory; the case needs one that does not")
	}
	if err := CheckRewrite("../s.yammm", "../s.yammm"); err != nil {
		t.Errorf("CheckRewrite refused a path that does not resolve: %v", err)
	}
}
