package yammmtest

import (
	"context"
	"sync/atomic"
)

// CheckContext is a context that counts its Err calls and reports
// [context.Canceled] from a chosen call on. It puts a cancellation at an exact
// check of the code under test, and its Done channel never reports it.
type CheckContext struct {
	context.Context //nolint:containedctx // a decorator holds the context it wraps
	cancelAt        int64
	checks          atomic.Int64
}

// NewCheckContext returns a context that cancels from its cancelAt-th Err call
// on. A cancelAt of zero never cancels.
func NewCheckContext(parent context.Context, cancelAt int64) *CheckContext {
	return &CheckContext{Context: parent, cancelAt: cancelAt}
}

// Err reports [context.Canceled] once the count reaches the chosen call.
func (c *CheckContext) Err() error {
	if n := c.checks.Add(1); c.cancelAt > 0 && n >= c.cancelAt {
		return context.Canceled
	}
	return c.Context.Err() //nolint:wrapcheck // the wrapped context's own answer is the contract
}

// Checks returns the number of Err calls so far.
func (c *CheckContext) Checks() int64 { return c.checks.Load() }

// CountChecks returns the number of Err calls run makes on a context that never
// cancels. Each call from 1 to that number is a point a test can cancel at.
func CountChecks(parent context.Context, run func(ctx context.Context)) int64 {
	probe := NewCheckContext(parent, 0)
	run(probe) //nolint:contextcheck // probe wraps parent
	return probe.Checks()
}
