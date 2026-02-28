package trace

import (
	"context"
	"testing"
)

func TestWithRequestID_RoundTrip(t *testing.T) {
	ctx := t.Context()
	ctx = WithRequestID(ctx, "req-123")

	got, ok := RequestIDFrom(ctx)
	if !ok {
		t.Fatal("expected request ID to be present")
	}
	if got != "req-123" {
		t.Errorf("got %q, want %q", got, "req-123")
	}
}

func TestRequestIDFrom_NotSet(t *testing.T) {
	ctx := t.Context()

	got, ok := RequestIDFrom(ctx)
	if ok {
		t.Error("expected request ID to not be present")
	}
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestWithRequestID_EmptyString(t *testing.T) {
	// Empty string is a valid request ID; distinguishable from "not set"
	ctx := t.Context()
	ctx = WithRequestID(ctx, "")

	got, ok := RequestIDFrom(ctx)
	if !ok {
		t.Fatal("expected request ID to be present (even if empty)")
	}
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestWithRequestID_Override(t *testing.T) {
	ctx := t.Context()
	ctx = WithRequestID(ctx, "first")
	ctx = WithRequestID(ctx, "second")

	got, ok := RequestIDFrom(ctx)
	if !ok {
		t.Fatal("expected request ID to be present")
	}
	if got != "second" {
		t.Errorf("got %q, want %q", got, "second")
	}
}

func TestWithRequestID_ChildContext(t *testing.T) {
	ctx := t.Context()
	ctx = WithRequestID(ctx, "parent-req")

	// Child context inherits parent's value
	child, cancel := context.WithCancel(ctx)
	defer cancel()

	got, ok := RequestIDFrom(child)
	if !ok {
		t.Fatal("expected request ID to be inherited by child context")
	}
	if got != "parent-req" {
		t.Errorf("got %q, want %q", got, "parent-req")
	}
}

func TestWithRequestID_ParentUnaffected(t *testing.T) {
	parent := t.Context()
	child := WithRequestID(parent, "child-req")

	// Parent should not have the request ID
	_, ok := RequestIDFrom(parent)
	if ok {
		t.Error("parent context should not have request ID")
	}

	// Child should have the request ID
	got, ok := RequestIDFrom(child)
	if !ok {
		t.Fatal("child context should have request ID")
	}
	if got != "child-req" {
		t.Errorf("got %q, want %q", got, "child-req")
	}
}
