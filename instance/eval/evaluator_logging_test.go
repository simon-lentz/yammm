package eval_test

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/simon-lentz/yammm/instance/eval"
	"github.com/simon-lentz/yammm/schema/expr"
)

// recordHandler is a test handler that records log records for inspection.
type recordHandler struct {
	mu      sync.Mutex
	records []slog.Record
	level   slog.Level
}

func newRecordHandler(level slog.Level) *recordHandler {
	return &recordHandler{level: level}
}

func (h *recordHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *recordHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}

func (h *recordHandler) WithAttrs(_ []slog.Attr) slog.Handler {
	return h
}

func (h *recordHandler) WithGroup(_ string) slog.Handler {
	return h
}

func (h *recordHandler) Records() []slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	result := make([]slog.Record, len(h.records))
	copy(result, h.records)
	return result
}

// hasOpName checks if any record has the given operation name.
func hasOpName(records []slog.Record, opName string) bool {
	for _, r := range records {
		var found bool
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == "op" && a.Value.String() == opName {
				found = true
				return false
			}
			return true
		})
		if found {
			return true
		}
	}
	return false
}

// hasMessage checks if any record has the given message.
func hasMessage(records []slog.Record, msg string) bool {
	for _, r := range records {
		if r.Message == msg {
			return true
		}
	}
	return false
}

func TestEvaluator_Logging(t *testing.T) {
	h := newRecordHandler(slog.LevelDebug)
	logger := slog.New(h)

	ev := eval.NewEvaluator(eval.WithLogger(logger))
	scope := eval.EmptyScope()

	// Evaluate a simple expression
	e := expr.SExpr{
		expr.Op("+"),
		expr.NewLiteral(int64(2)),
		expr.NewLiteral(int64(3)),
	}

	_, err := ev.Evaluate(e, scope)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	records := h.Records()

	// Should have eval.expr operation
	if !hasOpName(records, "yammm.eval.expr") {
		t.Error("expected yammm.eval.expr operation to be logged")
	}

	// Should log s-expression evaluation
	if !hasMessage(records, "evaluating s-expression") {
		t.Error("expected 'evaluating s-expression' message")
	}
}

func TestEvaluator_Logging_SExprOp(t *testing.T) {
	h := newRecordHandler(slog.LevelDebug)
	logger := slog.New(h)

	ev := eval.NewEvaluator(eval.WithLogger(logger))
	scope := eval.EmptyScope()

	// Evaluate an arithmetic expression
	e := expr.SExpr{
		expr.Op("*"),
		expr.NewLiteral(int64(4)),
		expr.NewLiteral(int64(5)),
	}

	_, err := ev.Evaluate(e, scope)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	records := h.Records()

	// Should have op attribute
	var foundOp bool
	for _, r := range records {
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == "op" && a.Value.String() == "*" {
				foundOp = true
				return false
			}
			return true
		})
	}

	if !foundOp {
		t.Error("expected op=* attribute in s-expression log")
	}
}

func TestEvaluator_NoLogging_WhenNilLogger(t *testing.T) {
	// No logger - should not panic
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	e := expr.SExpr{
		expr.Op("+"),
		expr.NewLiteral(int64(2)),
		expr.NewLiteral(int64(3)),
	}

	_, err := ev.Evaluate(e, scope)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	// Test passes if no panic occurred
}

func TestEvaluator_Logging_NilExpression(t *testing.T) {
	h := newRecordHandler(slog.LevelDebug)
	logger := slog.New(h)

	ev := eval.NewEvaluator(eval.WithLogger(logger))
	scope := eval.EmptyScope()

	// Nil expression should not log (returns early)
	_, err := ev.Evaluate(nil, scope)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	records := h.Records()

	// Should NOT have operation logged for nil expression
	if hasOpName(records, "yammm.eval.expr") {
		t.Error("expected no logging for nil expression")
	}
}
