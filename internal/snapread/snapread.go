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
//	snapread  ──imports──▶  diag, graph, schema
package snapread

import (
	"context"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/schema"
)

// UnmarshalWithHeader is snapshot.Unmarshal with no options that also returns
// the header the decode accepted, a *snapshot.Header, which is nil when the
// result has errors. The header is typed any because snapshot imports this
// package.
var UnmarshalWithHeader func(ctx context.Context, data []byte, s schema.Schema) (graph.Snapshot, any, diag.Result)

// HeaderOf is snapshot.ReadHeader over data with no size cap. It returns a
// *snapshot.Header, nil when the result has errors.
var HeaderOf func(ctx context.Context, data []byte) (any, diag.Result)
