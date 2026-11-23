package snapshot_test

import (
	"bytes"
	"runtime"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/raceskip"
	"github.com/simon-lentz/yammm/snapshot"
)

// ReadHeader decodes the header into its fields once. A decode that first
// copies the header's bytes allocates one more header's worth, which a
// metadata value of 1 MiB makes the bulk of the cost.
func TestReadHeader_AllocatesNoCopyOfTheHeader(t *testing.T) {
	// The race detector changes what the runtime allocates.
	raceskip.Skip(t)
	ctx := t.Context()
	const size = 1 << 20
	data := makeSnapshotBytes(ctx, t, snapshot.WithMetadata(map[string]string{"big": strings.Repeat("x", size)}))

	read := func() uint64 {
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		if _, err := snapshot.ReadHeader(ctx, bytes.NewReader(data)); err != nil {
			t.Fatalf("ReadHeader: %s", err)
		}
		runtime.ReadMemStats(&after)
		return after.TotalAlloc - before.TotalAlloc
	}
	read()
	if got := float64(read()) / size; got > 9.5 {
		t.Errorf("ReadHeader allocates %.2f bytes a metadata byte, want at most 9.5", got)
	}
}
