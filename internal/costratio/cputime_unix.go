//go:build unix

package costratio

import (
	"syscall"
	"testing"
	"time"
)

// CPUTime returns the CPU time the process has spent, user and system.
func CPUTime(tb testing.TB) time.Duration {
	tb.Helper()
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		tb.Fatal(err)
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}
