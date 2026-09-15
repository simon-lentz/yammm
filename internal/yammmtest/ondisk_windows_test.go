//go:build windows

package yammmtest

import (
	"syscall"
	"testing"
)

// shortPathName returns the 8.3 short form of the existing path long, or long
// itself where its volume generates no short names.
func shortPathName(t *testing.T, long string) string {
	t.Helper()
	p, err := syscall.UTF16PtrFromString(long)
	if err != nil {
		t.Fatal(err)
	}
	size, err := syscall.GetShortPathName(p, nil, 0)
	if err != nil {
		t.Fatalf("GetShortPathName(%q): %v", long, err)
	}
	buf := make([]uint16, size)
	n, err := syscall.GetShortPathName(p, &buf[0], size)
	if err != nil {
		t.Fatalf("GetShortPathName(%q): %v", long, err)
	}
	return syscall.UTF16ToString(buf[:n])
}
