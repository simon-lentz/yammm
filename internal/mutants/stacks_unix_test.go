//go:build unix

package main

import (
	"os/signal"
	"syscall"
)

// ignoreStackRequest makes this process ignore the signals that ask it to
// stop, so only a kill ends it.
func ignoreStackRequest() {
	signal.Ignore(syscall.SIGQUIT, syscall.SIGTERM)
}
