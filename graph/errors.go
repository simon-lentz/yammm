// Package graph builds an in-memory data structure from validated instances.
package graph

import (
	"errors"
	"fmt"
)

// Error sentinels for internal graph failures.
// These errors indicate programmer errors or internal faults, not data issues.
// Data issues are reported via diag.Result, not error returns.
var (
	// ErrInternal is the base error for internal graph failures.
	ErrInternal = errors.New("internal graph failure")

	// ErrNilGraph indicates a method was called on a nil *Graph receiver.
	ErrNilGraph = fmt.Errorf("%w: nil *Graph receiver", ErrInternal)

	// ErrNilInstance indicates nil ValidInstance was passed to Add.
	ErrNilInstance = fmt.Errorf("%w: nil ValidInstance passed to Add", ErrInternal)

	// ErrNilChild indicates nil ValidInstance was passed to AddComposed.
	ErrNilChild = fmt.Errorf("%w: nil ValidInstance passed to AddComposed", ErrInternal)

	// ErrSchemaMismatch indicates the instance was validated against a different
	// schema than the graph is bound to. This is a programmer error — callers
	// should ensure instances are validated with the same schema (or an imported
	// schema) used to construct the graph.
	ErrSchemaMismatch = fmt.Errorf("%w: instance schema does not match graph schema", ErrInternal)
)
