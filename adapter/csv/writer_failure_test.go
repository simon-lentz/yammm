package csv

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// errDevice stands for the I/O failures a real export meets: a full disk, a
// closed pipe, an unmounted volume.
var errDevice = errors.New("device gone")

// failingWriter refuses every byte. csv.Writer buffers through bufio, so which
// of the writer's three error checks fires depends on how much text the call
// pushes past the buffer: a short file reports at Flush alone, a long one
// reports from the Write that fills the buffer.
type failingWriter struct{ writes int }

func (w *failingWriter) Write(p []byte) (int, error) {
	w.writes++
	return 0, errDevice
}

// writeToFailingWriter runs WriteSnapshot against a writer that refuses every
// byte and returns that writer and the error.
func writeToFailingWriter(t *testing.T, s *schema.Schema, instances map[string][]map[string]any) (*failingWriter, error) {
	t.Helper()
	snap := buildSnapshot(t, s, instances)
	w := &failingWriter{}
	err := New().WriteSnapshot(context.Background(), func(string) (io.Writer, error) {
		return w, nil
	}, snap)
	if err == nil {
		t.Fatal("a writer that refuses every byte reported success")
	}
	if !errors.Is(err, errDevice) {
		t.Errorf("error does not wrap the device failure: %v", err)
	}
	return w, err
}

// A short export reports through Flush, which is the only signal that the file
// on disk is truncated.
func TestWriteSnapshot_FlushFailureIsReported(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "basic.yammm")

	w, err := writeToFailingWriter(t, s, map[string][]map[string]any{
		"Entity": {{"id": "e1", "name": "Alice"}},
	})

	if !strings.Contains(err.Error(), "csv flush") {
		t.Errorf("a short export must report at the flush, got %v", err)
	}
	if w.writes == 0 {
		t.Error("the writer was never reached")
	}
}

// An export long enough to fill the buffer reports from the row write, before
// the flush is reached.
func TestWriteSnapshot_RowFailureIsReported(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "basic.yammm")

	rows := make([]map[string]any, 0, 200)
	for i := range 200 {
		rows = append(rows, map[string]any{
			"id":   fmt.Sprintf("e%03d", i),
			"name": strings.Repeat("x", 40),
		})
	}

	_, err := writeToFailingWriter(t, s, map[string][]map[string]any{"Entity": rows})

	if !strings.Contains(err.Error(), "csv write row") {
		t.Errorf("a long export must report at the row write, got %v", err)
	}
}

// A header wider than the buffer reports at the header write. That the header
// precedes the rows is pinned by the round trip, which reads it back as the
// first record; this test pins only which check reports.
func TestWriteSnapshot_HeaderFailureIsReported(t *testing.T) {
	t.Parallel()

	var src strings.Builder
	src.WriteString("schema \"wide\"\n\ntype W {\n\tidentifier String primary\n")
	for i := range 320 {
		fmt.Fprintf(&src, "\tcolumn_number_%04d String\n", i)
	}
	src.WriteString("}\n")

	s, res := schema.LoadString(context.Background(), src.String(), "wide.yammm")
	if res.HasErrors() {
		t.Fatalf("load wide schema: %s", res)
	}

	_, err := writeToFailingWriter(t, s, map[string][]map[string]any{
		"W": {{"identifier": "w1"}},
	})

	if !strings.Contains(err.Error(), "csv write header") {
		t.Errorf("a wide header must report at the header write, got %v", err)
	}
}

// A nil snapshot is refused by both write entry points, not only by
// MarshalSnapshot.
func TestWriteSnapshot_NilSnapshot(t *testing.T) {
	t.Parallel()
	err := New().WriteSnapshot(context.Background(), func(string) (io.Writer, error) {
		t.Fatal("a writer was requested for a nil snapshot")
		return io.Discard, nil
	}, nil)
	if !errors.Is(err, ErrNilSnapshot) {
		t.Errorf("error is %v, want ErrNilSnapshot", err)
	}
}

// Cancellation between the header and the rows flushes what the writer already
// holds before it reports, so a truncated file still parses as far as it goes.
func TestWriteSnapshot_CancellationDuringRowsFlushesTheHeader(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "basic.yammm")
	snap := buildSnapshot(t, s, map[string][]map[string]any{
		"Entity": {{"id": "e1", "name": "Alice"}},
	})

	ctx, cancel := context.WithCancel(context.Background())
	var buf bytes.Buffer
	// Cancelling as the writer is handed over leaves the context live at the
	// per-type guard and cancelled at the per-instance one.
	err := New().WriteSnapshot(ctx, func(string) (io.Writer, error) {
		cancel()
		return &buf, nil
	}, snap)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error does not wrap context.Canceled: %v", err)
	}
	if !strings.Contains(err.Error(), "csv write:") {
		t.Errorf("a cancellation among the rows must report at the row guard, got %v", err)
	}
	if !strings.Contains(buf.String(), "id") {
		t.Errorf("the header was not flushed before the refusal, got %q", buf.String())
	}
}

// A context already cancelled is refused before any writer is requested.
func TestWriteSnapshot_CancelledBeforeAnyType(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "basic.yammm")
	snap := buildSnapshot(t, s, map[string][]map[string]any{
		"Entity": {{"id": "e1", "name": "Alice"}},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := New().WriteSnapshot(ctx, func(string) (io.Writer, error) {
		t.Fatal("a writer was requested under a cancelled context")
		return io.Discard, nil
	}, snap)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error does not wrap context.Canceled: %v", err)
	}
	if !strings.Contains(err.Error(), "csv write snapshot:") {
		t.Errorf("want the per-type guard, got %v", err)
	}
}

// MarshalSnapshot carries the same per-type guard as WriteSnapshot.
func TestMarshalSnapshot_CancelledBeforeAnyType(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "basic.yammm")
	snap := buildSnapshot(t, s, map[string][]map[string]any{
		"Entity": {{"id": "e1", "name": "Alice"}},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := New().MarshalSnapshot(ctx, snap)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error does not wrap context.Canceled: %v", err)
	}
	if !strings.Contains(err.Error(), "csv marshal snapshot:") {
		t.Errorf("want the per-type guard, got %v", err)
	}
}
