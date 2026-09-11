//go:build !unix

package cli

// platformTargetCases returns no case: the host cannot create a FIFO.
func platformTargetCases() []writeTargetCase { return nil }
