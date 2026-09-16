//go:build !unix || aix || solaris

package cli

// platformTargetCases returns no case: the host cannot create a FIFO, or its
// syscall package has no Mkfifo to create one with.
func platformTargetCases() []writeTargetCase { return nil }
