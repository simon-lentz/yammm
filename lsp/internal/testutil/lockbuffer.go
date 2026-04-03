package testutil

import (
	"bytes"
	"sync"
)

// LockBuffer is a thread-safe bytes.Buffer for capturing log output in tests.
type LockBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (lb *LockBuffer) Write(p []byte) (int, error) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	return lb.buf.Write(p) //nolint:wrapcheck // thin wrapper; callers don't need wrapped errors
}

func (lb *LockBuffer) String() string {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	return lb.buf.String()
}

func (lb *LockBuffer) Reset() {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.buf.Reset()
}
