package snapshot

import (
	"context"

	"github.com/simon-lentz/yammm/schema"
)

// FiledRecords walks data's instances as a read with opts does and returns the
// number of roots the walk filed target_missing records under, or -1 for a
// document the walk cannot decode. Only a revalidating read reads that index,
// so a test can hold every other read to building none.
func FiledRecords(data []byte, s *schema.Schema, opts ...LoadOption) int {
	sd := newStreamDecoder(data, s, applyLoadOptions(opts))
	if err := sd.decodeHeader(); err != nil {
		return -1
	}
	groups, diags, err := sd.decodeSections()
	if err != nil {
		return -1
	}
	idx, err := sd.walkInstances(context.Background(), groups, diags.Unresolved)
	if err != nil {
		return -1
	}
	return len(idx.lost)
}
