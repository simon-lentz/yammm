package snapshot_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/diagtest"
	"github.com/simon-lentz/yammm/internal/raceskip"
	"github.com/simon-lentz/yammm/internal/snapread"
	"github.com/simon-lentz/yammm/snapshot"
)

// ReadHeader reads at most MaxHeaderSize bytes: a document whose types table
// ends at that byte is read, and one a byte longer is refused.
func TestReadHeader_ReadsUpToMaxHeaderSizeAndNoFurther(t *testing.T) {
	t.Parallel()
	// The 16 MiB header is read on one goroutine, and the detector multiplies each byte.
	raceskip.Skip(t)
	ctx := t.Context()
	sized := func(pad int) []byte {
		return makeSnapshotBytes(ctx, t, snapshot.WithMetadata(map[string]string{"big": strings.Repeat("x", pad)}))
	}
	typesEnd := func(data []byte) int {
		at := bytes.Index(data, []byte(`"types":`))
		return at + bytes.IndexByte(data[at:], ']') + 1
	}
	pad := snapshot.MaxHeaderSize - typesEnd(sized(0))
	if data := sized(pad); typesEnd(data) != snapshot.MaxHeaderSize {
		t.Fatalf("the types table ends at %d, want %d", typesEnd(data), snapshot.MaxHeaderSize)
	}
	if _, err := snapshot.ReadHeader(ctx, bytes.NewReader(sized(pad))); err != nil {
		t.Errorf("a types table ending at MaxHeaderSize is refused: %s", err)
	}
	if _, res := diagtest.Issues(t, func() (*snapshot.Header, error) {
		return snapshot.ReadHeader(ctx, bytes.NewReader(sized(pad+1)))
	}); !strings.Contains(res.String(), "header exceeded MaxHeaderSize") {
		t.Errorf("a types table ending a byte past MaxHeaderSize: %s, want the cap's refusal", res)
	}
}

// snapread.HeaderOf reads with no cap, so a document that ends early is
// refused for ending, never for a cap.
func TestHeaderOf_RefusesATruncatedDocumentForEndingEarly(t *testing.T) {
	t.Parallel()
	data := makeSnapshotBytes(t.Context(), t)
	_, err := snapread.HeaderOf(t.Context(), data[:40])
	if msg := fmt.Sprint(err); err == nil || !strings.Contains(msg, "unexpected EOF") || strings.Contains(msg, "MaxHeaderSize") {
		t.Errorf("HeaderOf of a truncated document: %s, want an early end and no cap", msg)
	}
}

// snapread's two reads return a nil header, and UnmarshalWithHeader the zero
// Snapshot, beside an error, so a caller that reads the value on error reads
// nothing.
func TestSnapread_ReturnsNoValueBesideAnError(t *testing.T) {
	t.Parallel()
	s := testSchema(t)
	data := makeSnapshotBytes(t.Context(), t)[:40]
	if h, err := snapread.HeaderOf(t.Context(), data); err == nil || h != nil {
		t.Errorf("HeaderOf of a truncated document = (%#v, %v), want (nil, an error)", h, err)
	}
	snap, h, err := snapread.UnmarshalWithHeader(t.Context(), data, s)
	if err == nil || h != nil || !snap.IsZero() {
		t.Errorf("UnmarshalWithHeader of a truncated document = (zero %t, %#v, %v), want (the zero Snapshot, nil, an error)", snap.IsZero(), h, err)
	}
}
