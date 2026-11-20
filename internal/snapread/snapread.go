// Package snapread lets the module's CLI read a .ys document and its header in
// one decode, so the header it carries forward is the one the load accepted. A
// second header read would judge the document again under other limits:
// snapshot.ReadHeader refuses a header past snapshot.MaxHeaderSize, which
// snapshot.Unmarshal accepts. The snapshot package sets [UnmarshalWithHeader]
// when it is initialized. No consumer reads a body and its header together, so
// the pair is not public API.
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
