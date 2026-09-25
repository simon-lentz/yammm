package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/simon-lentz/yammm/adapter/csv"
	adapterjson "github.com/simon-lentz/yammm/adapter/json"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// dataExtension is the one spelling of a data file's extension that
// [DetectFormat] and [CSVDelimiter] both read, so a name that selects the CSV
// format and a name that selects a tab are decided by one fold.
func dataExtension(path string) string {
	return strings.ToLower(filepath.Ext(path))
}

// DetectFormat returns "json" or "csv" based on the file extension.
func DetectFormat(path string) (string, error) {
	switch dataExtension(path) {
	case ".json", ".jsonc":
		return "json", nil
	case ".csv", ".tsv":
		return "csv", nil
	default:
		return "", fmt.Errorf("cannot detect data format for %q: use --from to specify json or csv", path)
	}
}

// CSVDelimiter returns the field delimiter a CSV file's extension names: '\t'
// for ".tsv" and ',' for every other name, the empty one included. The read
// side and the write side both take it, so a file `export` writes under a
// ".tsv" name is one `check` reads back.
func CSVDelimiter(path string) rune {
	if dataExtension(path) == ".tsv" {
		return '\t'
	}
	return ','
}

// LoadAndParseJSON reads a JSON file and parses it into raw instances.
//
// The identity and the host path come out of one resolution, as the schema
// loader takes them, so a data diagnostic and a schema diagnostic name one file
// the same way.
//
// Returns (T, diag.Result, error) because the error return captures the
// failures that stop a read — a path that names no file, one that cannot be
// read — which are distinct from semantic parse issues reported through the
// diag.Result. A path the resolver refuses is a usage failure and reaches a
// different exit code from a file that cannot be read; [ExitForError] decides.
// This is an internal CLI helper, not a public API.
func LoadAndParseJSON(ctx context.Context, path string) (map[string][]instance.RawInstance, diag.Result, error) {
	sourceID, hostPath, err := location.ResolveSourcePath(path)
	if err != nil {
		return nil, diag.Result{}, fmt.Errorf("resolve data file %q: %w", path, err)
	}

	data, err := os.ReadFile(hostPath)
	if err != nil {
		return nil, diag.Result{}, fmt.Errorf("read data file: %w", err)
	}

	parsed, result := adapterjson.New().ParseObject(ctx, sourceID, data)
	return parsed, result, nil
}

// LoadAndParseCSV reads a CSV file and parses it into raw instances.
// The typeName parameter specifies the schema type for the rows.
// If typeColumn is non-empty, it is used instead of typeName for multi-type CSVs.
//
// The identity and the host path come out of one resolution, as
// [LoadAndParseJSON] takes them; the handle is opened on the path it returns.
// The delimiter is [CSVDelimiter]'s for path as the caller spelled it, the
// spelling [DetectFormat] reads.
//
// Returns (T, diag.Result, error) because the error return captures I/O
// failures (file open errors) which are distinct from semantic parse
// issues reported through the diag.Result.
func LoadAndParseCSV(ctx context.Context, path, typeName, typeColumn string, s *schema.Schema) (map[string][]instance.RawInstance, diag.Result, error) {
	sourceID, hostPath, err := location.ResolveSourcePath(path)
	if err != nil {
		return nil, diag.Result{}, fmt.Errorf("resolve data file %q: %w", path, err)
	}

	f, err := os.Open(hostPath)
	if err != nil {
		return nil, diag.Result{}, fmt.Errorf("open data file: %w", err)
	}
	defer f.Close()

	if typeColumn != "" {
		adapter := csv.New(csv.WithTypeColumn(typeColumn), csv.WithSchema(s), csv.WithDelimiter(CSVDelimiter(path)))
		parsed, result := adapter.ParseWithTypeColumn(ctx, sourceID, f, func(name string) *schema.Type {
			t, _ := s.ResolveTypeName(name)
			return t
		})
		return parsed, result, nil
	}

	// An unknown type is the caller's mistake, refused here rather than parsed
	// uncoerced and refused row by row downstream.
	schemaType, ok := s.ResolveTypeName(typeName)
	if !ok {
		return nil, diag.Result{}, fmt.Errorf("type %q not found in schema", typeName)
	}
	raws, result := csv.New(csv.WithSchema(s), csv.WithDelimiter(CSVDelimiter(path))).ParseTyped(ctx, sourceID, typeName, f, schemaType)
	return map[string][]instance.RawInstance{typeName: raws}, result, nil
}

// Validated is a document's instances after validation, in the order they
// were validated: type names sorted, and each type's instances in the order the
// document lists them.
type Validated struct {
	validator *instance.Validator
	batches   []validatedBatch
}

// validatedBatch is one type name's instances and the validator's answer for
// each: valids holds one entry per raw, nil where the validator refused it, and
// is nil when the batch was refused whole or did not complete.
type validatedBatch struct {
	typeName string
	raws     []instance.RawInstance
	valids   []*instance.ValidInstance
}

// ValidateInstances validates parsed instances against the schema and returns
// them with the merged diagnostics.
func ValidateInstances(ctx context.Context, s *schema.Schema, parsed map[string][]instance.RawInstance) (Validated, diag.Result) {
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
func (d Validated) Valids() []*instance.ValidInstance {
	var out []*instance.ValidInstance
	for _, b := range d.batches {
		for _, valid := range b.valids {
			if valid != nil {
				out = append(out, valid)
			}
		}
	}
	return out
}

// BuildGraph adds the document's valid instances to base, or to a new graph
// when base is nil, runs the graph-level checks, and returns the graph with a
// verdict about the document (see [documentVerdict]).
func BuildGraph(ctx context.Context, s *schema.Schema, base *graph.Graph, doc Validated) (*graph.Graph, diag.Result) {
	g := base
	if g == nil {
		g = graph.New(s)
	}
	collector := diag.NewCollectorUnlimited()
	valids := doc.Valids()
	added := make(map[*instance.ValidInstance]diag.Result, len(valids))
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

// IsSnapshotFile reports whether the file at path is a yammm snapshot (.ys)
// file using content-based detection. It reads the first bytes and checks
// for the {"yammm_snapshot": prefix.
//
// Returns (false, nil) for non-snapshot files (including non-JSON files).
// Returns (false, err) only for genuine I/O errors.
func IsSnapshotFile(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	dec := json.NewDecoder(io.LimitReader(f, 512))

	// First token must be '{'.
	tok, err := dec.Token()
	if err != nil {
		return false, nil // not valid JSON — not a snapshot
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != '{' {
		return false, nil
	}

	// Second token must be the string "yammm_snapshot".
	tok, err = dec.Token()
	if err != nil {
		return false, nil
	}
	key, ok := tok.(string)
	if !ok {
		return false, nil
	}
	return key == "yammm_snapshot", nil
}

// LoadSnapshotFile reads a .ys file and loads it into a [*graph.Snapshot],
// beside the header that document carries.
//
// The header comes back because [graph.Snapshot] exposes no metadata or
// created_at accessor, so a caller re-marshaling what it read has no other
// route to it; it is nil when the header cannot be parsed. Returns
// (T, diag.Result, error) following the CLI helper convention: error captures
// I/O failures, diag.Result captures semantic issues.
func LoadSnapshotFile(ctx context.Context, path string, s *schema.Schema, opts ...snapshot.LoadOption) (*graph.Snapshot, *snapshot.HeaderInfo, diag.Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, diag.Result{}, fmt.Errorf("read snapshot file: %w", err)
	}
	snap, result := snapshot.Load(ctx, data, s, opts...)
	// Load has checked the whole document and reported its issues, so the header
	// is read at header cost and its diagnostics are dropped, not reported twice.
	header, _ := snapshot.HeaderOnlyRead(ctx, bytes.NewReader(data))
	return snap, header, result, nil
}
