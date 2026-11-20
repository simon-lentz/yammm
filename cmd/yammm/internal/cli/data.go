package cli

import (
	"context"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/jsondata"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/instance/path"
	"github.com/simon-lentz/yammm/internal/snapread"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// LoadAndParseJSON reads a JSON file and parses it into its elements.
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
func LoadAndParseJSON(ctx context.Context, path string) (map[string]jsondata.Batch, diag.Result, error) {
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

// ValidateInstances validates parsed elements against the schema and returns
// them with the merged diagnostics. An element the parser refused is not
// validated, and its entry is zero, as a refused instance's is. A diagnostic's
// instance index names its element among the type's elements, refused or not,
// and a diagnostic that names no element stands at the root of the batch's source.
func ValidateInstances(ctx context.Context, s schema.Schema, parsed map[string]jsondata.Batch) (Validated, diag.Result) {
	doc := Validated{validator: instance.NewValidator(s)}
	collector := diag.NewCollector(diag.NoLimit)
	for _, typeName := range slices.Sorted(maps.Keys(parsed)) {
		elements := parsed[typeName].Elements
		raws := make([]instance.RawInstance, len(elements))
		judged := []instance.RawInstance{}
		var at []int // at[j] is the index in elements of judged[j]
		for i, e := range elements {
			raws[i] = e.RawInstance
			if !e.Refused {
				judged = append(judged, e.RawInstance)
				at = append(at, i)
			}
		}
		judgedValids, result := doc.validator.Validate(ctx, typeName, judged)
		var valids []instance.ValidInstance
		if judgedValids != nil {
			valids = make([]instance.ValidInstance, len(elements))
			for j, valid := range judgedValids {
				valids[at[j]] = valid
			}
		}
		collector.MergeFunc(result, anchorIssue(parsed[typeName].Source, at))
		doc.batches = append(doc.batches, validatedBatch{typeName: typeName, raws: raws, valids: valids})
	}
	return doc, collector.Result()
}

// anchorIssue returns the transform that places an issue about the type name,
// one that names no element, at the root of source, and rewrites an element's
// instance index j, an index into the batch the validator judged, as at[j]. The
// validator anchors a type issue at the first element it judged, which names no
// file when it judged none and a later file than source when the first stated none.
func anchorIssue(source location.SourceID, at []int) func(diag.Issue) diag.Issue {
	return func(issue diag.Issue) diag.Issue {
		details := issue.Details()
		indexed := false
		changed := false
		for k, d := range details {
			if d.Key != diag.DetailKeyInstanceIndex {
				continue
			}
			indexed = true
			if j, err := strconv.Atoi(d.Value); err == nil && j >= 0 && j < len(at) && at[j] != j {
				details[k].Value = strconv.Itoa(at[j])
				changed = true
			}
		}
		switch {
		case !indexed:
			return diag.FromIssue(issue).WithSpan(location.Span{}).WithPath(source.String(), path.Root.String()).Build()
		case !changed:
			return issue
		}
		return diag.NewIssue(issue.Severity(), issue.Code(), issue.Message()).
			WithSpan(issue.Span()).
			WithPath(issue.SourceName(), issue.Path()).
			WithHint(issue.Hint()).
			WithRelated(issue.Related()...).
			WithDetails(details...).
			Build()
	}
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
	collector := diag.NewCollector(diag.NoLimit)
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
	collector := diag.NewCollector(diag.NoLimit)
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
// route to it. It is the header the load accepted, whatever its size, and nil
// when the result has errors. The error reports an I/O failure and the result
// a semantic issue, as every CLI helper splits them.
func LoadSnapshotFile(ctx context.Context, path string, s schema.Schema) (graph.Snapshot, *snapshot.Header, diag.Result, error) {
	data, err := ReadFile(path)
	if err != nil {
		return graph.Snapshot{}, nil, diag.Result{}, fmt.Errorf("read snapshot file: %w", err)
	}
	snap, header, result := snapread.UnmarshalWithHeader(ctx, data, s)
	return snap, header.(*snapshot.Header), result, nil
}
