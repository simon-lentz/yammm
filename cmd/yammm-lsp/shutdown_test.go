package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
	"testing"
)

// wrappedEPIPE is an error carrying a broken pipe in its message alone, the
// shape the fallback exists for: some libraries wrap EPIPE in a type that
// breaks the errors.Is chain.
type wrappedEPIPE struct{}

func (wrappedEPIPE) Error() string { return "write tcp 127.0.0.1:1: broken pipe" }

// TestIsCleanShutdown pins B59. The function classified every fatal-versus-
// normal disconnect the server reports and no test referenced it, so a mutation
// making it answer false for everything survived — and every client closing
// stdio would have been logged as a crash.
func TestIsCleanShutdown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"EOF", io.EOF, true},
		{"wrapped EOF", fmt.Errorf("read stdin: %w", io.EOF), true},
		{"closed file", os.ErrClosed, true},
		{"wrapped closed file", fmt.Errorf("read: %w", os.ErrClosed), true},
		{"EPIPE", syscall.EPIPE, true},
		{"wrapped EPIPE", fmt.Errorf("write: %w", syscall.EPIPE), true},
		{"EPIPE by message only", wrappedEPIPE{}, true},
		{"unexpected EOF is not a clean close", io.ErrUnexpectedEOF, false},
		{"a real failure", errors.New("decode message: invalid content length"), false},
		{"permission denied", os.ErrPermission, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isCleanShutdown(tt.err); got != tt.want {
				t.Errorf("isCleanShutdown(%v) = %t, want %t", tt.err, got, tt.want)
			}
		})
	}
}
