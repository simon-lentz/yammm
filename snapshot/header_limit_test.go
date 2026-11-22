package snapshot_test

import (
	"bytes"
	"strings"
	"testing"

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
	if _, res := snapshot.ReadHeader(ctx, bytes.NewReader(sized(pad))); res.HasErrors() {
		t.Errorf("a types table ending at MaxHeaderSize is refused: %s", res)
	}
	if _, res := snapshot.ReadHeader(ctx, bytes.NewReader(sized(pad+1))); !strings.Contains(res.String(), "header exceeded MaxHeaderSize") {
		t.Errorf("a types table ending a byte past MaxHeaderSize: %s, want the cap's refusal", res)
	}
}

// snapread.HeaderOf reads with no cap, so a document that ends early is
// refused for ending, never for a cap.
func TestHeaderOf_RefusesATruncatedDocumentForEndingEarly(t *testing.T) {
	t.Parallel()
	data := makeSnapshotBytes(t.Context(), t)
	_, res := snapread.HeaderOf(t.Context(), data[:40])
	if msg := res.String(); !strings.Contains(msg, "unexpected EOF") || strings.Contains(msg, "MaxHeaderSize") {
		t.Errorf("HeaderOf of a truncated document: %s, want an early end and no cap", msg)
	}
}
