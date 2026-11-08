package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"maps"
	"slices"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/jsondata"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// LoadAndParseJSON reads a JSON file and parses it into raw instances.
//
// The bytes come through the path the kernel reaches for path ([ReadFile]), and
// the identity, the one the schema loader derives, only names them, so a data
// diagnostic and a schema diagnostic name one file the same way, and a
// descriptor path, or a link to one, reads the file a shell reads.
//
// The error return captures the failures that stop a read: a path that names no
// file, one that cannot be read, or one the identity cannot be derived for. The
// diag.Result carries the parse's diagnostics. The caller refuses an empty path
// or one that is not valid UTF-8 first ([CheckSourceOperand], exit 2), so every
// error this returns is a path the filesystem answered, and exits 3.
func LoadAndParseJSON(ctx context.Context, path string) (map[string][]instance.RawInstance, diag.Result, error) {
	data, err := ReadFile(path)
	if err != nil {
		return nil, diag.Result{}, fmt.Errorf("read data file: %w", err)
	}
	sourceID, err := location.SourceIDFromPath(path)
	if err != nil {
		return nil, diag.Result{}, fmt.Errorf("resolve data file %q: %w", path, err)
	}
	parsed, result := jsondata.Parse(ctx, sourceID, data)
	return parsed, result, nil
}

// Validated is a document's instances after validation, in the order they
// were validated: type names sorted, and each type's instances in the order the
// document lists them.
type Validated struct {
	validator *instance.Validator
	batches   []validatedBatch
}

// validatedBatch is one type name's instances and the validator's answer for
// each: valids holds one entry per raw, zero where the validator refused it, and
// is nil when the batch was refused whole or did not complete.
type validatedBatch struct {
	typeName string
	raws     []instance.RawInstance
	valids   []instance.ValidInstance
}

// ValidateInstances validates parsed instances against the schema and returns
// them with the merged diagnostics.
func ValidateInstances(ctx context.Context, s schema.Schema, parsed map[string][]instance.RawInstance) (Validated, diag.Result) {
	doc := Validated{validator: instance.NewValidator(s)}
	collector := diag.NewCollectorUnlimited()
	for _, typeName := range slices.Sorted(maps.Keys(parsed)) {
		raws := parsed[typeName]
		valids, result := doc.validator.Validate(ctx, typeName, raws)
		collector.Merge(result)
		doc.batches = append(doc.batches, validatedBatch{typeName: typeName, raws: raws, valids: valids})
	}
	return doc, collector.Result()
}

// Valids returns the instances the validator accepted, in validation order.
func (d Validated) Valids() []instance.ValidInstance {
	var out []instance.ValidInstance
	for _, b := range d.batches {
		for _, valid := range b.valids {
			if !valid.IsZero() {
				out = append(out, valid)
			}
		}
	}
	return out
}

// BuildGraph adds the document's valid instances to base, or to a new graph
// when base is nil, runs the graph-level checks, and returns the graph with a
// verdict about the document (see [documentVerdict]).
func BuildGraph(ctx context.Context, s schema.Schema, base *graph.Graph, doc Validated) (*graph.Graph, diag.Result) {
	g := base
	if g == nil {
		g = graph.New(s)
	}
	collector := diag.NewCollectorUnlimited()
	valids := doc.Valids()
	added := make(map[instance.ValidInstance]diag.Result, len(valids))
	refusals := false
	for _, valid := range valids {
		result := g.Add(ctx, valid)
		collector.Merge(result)
		added[valid] = result
		refusals = refusals || result.HasErrors()
	}
	check := g.Check(ctx)
	if ctx.Err() == nil && (refusals || len(valids) < doc.rootCount()) {
		verdict := documentVerdict(s, g, base != nil, doc, added)
		check = verdict.explain(check)
		collector.Merge(verdict.duplicates)
	}
	collector.Merge(check)
	return g, collector.Result()
}

// rootCount is the number of root instances the document lists.
func (d Validated) rootCount() int {
	n := 0
	for _, b := range d.batches {
		n += len(b.raws)
	}
	return n
}

// MergeResults merges multiple diagnostic results into one.
func MergeResults(results ...diag.Result) diag.Result {
	collector := diag.NewCollectorUnlimited()
	for _, r := range results {
		collector.Merge(r)
	}
	return collector.Result()
}

// WriteTo writes data to the specified path, or to w if path is empty.
func WriteTo(data []byte, path string, w io.Writer) error {
	if path != "" {
		return WriteFile(path, data)
	}
	_, err := w.Write(data)
	return err
}

// LoadSnapshotFile reads a .ys file and loads it into a [graph.Snapshot],
// beside the header that document carries.
//
// The header comes back because [graph.Snapshot] exposes no metadata or
// created_at accessor, so a caller re-marshaling what it read has no other
// route to it; it is nil when the header cannot be parsed. Returns
// (T, diag.Result, error) following the CLI helper convention: error captures
// I/O failures, diag.Result captures semantic issues.
func LoadSnapshotFile(ctx context.Context, path string, s schema.Schema, opts ...snapshot.LoadOption) (graph.Snapshot, *snapshot.HeaderInfo, diag.Result, error) {
	data, err := ReadFile(path)
	if err != nil {
		return graph.Snapshot{}, nil, diag.Result{}, fmt.Errorf("read snapshot file: %w", err)
	}
	snap, result := snapshot.Load(ctx, data, s, opts...)
	// Load has checked the whole document and reported its issues, so the header
	// is read at header cost and its diagnostics are dropped, not reported twice.
	header, _ := snapshot.HeaderOnlyRead(ctx, bytes.NewReader(data))
	return snap, header, result, nil
}
