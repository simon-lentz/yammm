// Package snapread lets the module's CLI read a .ys document it holds in memory
// under the limits of the call it feeds. snapshot.ReadHeader refuses a header
// past snapshot.MaxHeaderSize, which snapshot.Unmarshal and
// snapshot.UpdateMetadata accept, so a capped header read would refuse a
// document those calls take. [UnmarshalWithHeader] reads a document and its
// header in one decode, so the header the CLI carries forward is the one the
// load accepted. [HeaderOf] reads the header of bytes already in memory with no
// cap, since the cap bounds a stream read and guards nothing there. The
// snapshot package sets both when it is initialized. No consumer needs either,
// so neither is public API.
//
// # Dependencies
//
//	snapread  ──imports──▶  graph, schema
package snapread

import (
	"context"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/schema"
)

// UnmarshalWithHeader is snapshot.Unmarshal with no options that also returns
// the header the decode accepted, a *snapshot.Header; beside an error it
// returns the zero Snapshot and a nil header. The header is typed any because snapshot imports this
// package.
var UnmarshalWithHeader func(ctx context.Context, data []byte, s schema.Schema) (graph.Snapshot, any, error)

// HeaderOf is snapshot.ReadHeader over data with no size cap. It returns a
// *snapshot.Header, or a nil any beside an error.
var HeaderOf func(ctx context.Context, data []byte) (any, error)
