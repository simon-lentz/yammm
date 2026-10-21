//go:build unix

package main

import (
	"os"
	"os/signal"
	"syscall"
)

// ignoreStackRequest makes this process ignore the signals that ask it to
// stop, so only a kill ends it.
func ignoreStackRequest() {
	signal.Ignore(syscall.SIGQUIT, syscall.SIGTERM)
}

// exitAtStackRequest makes this process exit 0 when asked for its stacks.
func exitAtStackRequest() {
	asked := make(chan os.Signal, 1)
	signal.Notify(asked, syscall.SIGQUIT)
	go func() {
		<-asked
		os.Exit(0)
	}()
}
