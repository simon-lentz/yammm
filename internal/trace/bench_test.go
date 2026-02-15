package trace

import (
	"context"
	"log/slog"
	"testing"
)

// These benchmarks verify the near-zero cost when logging is disabled.
// Target: ~1-2ns (nil check only), 0 allocations.
//
// All benchmarks use b.ReportAllocs() to make allocation counts always visible,
// and b.ResetTimer() after any setup to exclude setup cost from measurements.

func BenchmarkEnabled_NilLogger(b *testing.B) {
	ctx := context.Background()
	var logger *slog.Logger
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = Enabled(ctx, logger, slog.LevelDebug)
	}
}

func BenchmarkDebug_NilLogger(b *testing.B) {
	ctx := context.Background()
	var logger *slog.Logger
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		Debug(ctx, logger, "msg", slog.String("key", "value"))
	}
}

func BenchmarkDebugLazy_NilLogger(b *testing.B) {
	ctx := context.Background()
	var logger *slog.Logger
	fn := func() []slog.Attr {
		return []slog.Attr{slog.String("key", "value")}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		DebugLazy(ctx, logger, "msg", fn)
	}
}

func BenchmarkInfo_NilLogger(b *testing.B) {
	ctx := context.Background()
	var logger *slog.Logger
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		Info(ctx, logger, "msg", slog.String("key", "value"))
	}
}

func BenchmarkInfoLazy_NilLogger(b *testing.B) {
	ctx := context.Background()
	var logger *slog.Logger
	fn := func() []slog.Attr {
		return []slog.Attr{slog.String("key", "value")}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		InfoLazy(ctx, logger, "msg", fn)
	}
}

func BenchmarkWarn_NilLogger(b *testing.B) {
	ctx := context.Background()
	var logger *slog.Logger
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		Warn(ctx, logger, "msg", slog.String("key", "value"))
	}
}

func BenchmarkError_NilLogger(b *testing.B) {
	ctx := context.Background()
	var logger *slog.Logger
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		Error(ctx, logger, "msg", slog.String("key", "value"))
	}
}

func BenchmarkOpBeginEnd_NilLogger(b *testing.B) {
	ctx := context.Background()
	var logger *slog.Logger
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		op := Begin(ctx, logger, "test.op")
		op.End(nil)
	}
}

func BenchmarkOpBeginEnd_NilLoggerWithRequestID(b *testing.B) {
	ctx := WithRequestID(context.Background(), "req-123")
	var logger *slog.Logger
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		op := Begin(ctx, logger, "test.op")
		op.End(nil)
	}
}

// Benchmarks with disabled level (logger non-nil but level too low)

func BenchmarkDebug_DisabledLevel(b *testing.B) {
	ctx := context.Background()
	h := newRecordHandler(slog.LevelInfo) // Debug not enabled
	logger := slog.New(h)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		Debug(ctx, logger, "msg", slog.String("key", "value"))
	}
}

func BenchmarkDebugLazy_DisabledLevel(b *testing.B) {
	ctx := context.Background()
	h := newRecordHandler(slog.LevelInfo) // Debug not enabled
	logger := slog.New(h)
	fn := func() []slog.Attr {
		return []slog.Attr{slog.String("key", "value")}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		DebugLazy(ctx, logger, "msg", fn)
	}
}

func BenchmarkOpBeginEnd_DisabledLevel(b *testing.B) {
	ctx := context.Background()
	h := newRecordHandler(slog.LevelInfo) // Debug not enabled
	logger := slog.New(h)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		op := Begin(ctx, logger, "test.op")
		op.End(nil)
	}
}

// Benchmarks with enabled logging (for comparison)

func BenchmarkDebug_EnabledLevel(b *testing.B) {
	ctx := context.Background()
	h := newRecordHandler(slog.LevelDebug)
	logger := slog.New(h)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		Debug(ctx, logger, "msg", slog.String("key", "value"))
	}
}

func BenchmarkOpBeginEnd_EnabledLevel(b *testing.B) {
	ctx := context.Background()
	h := newRecordHandler(slog.LevelDebug)
	logger := slog.New(h)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		op := Begin(ctx, logger, "test.op")
		op.End(nil)
	}
}
