package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
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

// DetectFormat returns "json" or "csv" based on the file extension.
func DetectFormat(path string) (string, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json", ".jsonc":
		return "json", nil
	case ".csv", ".tsv":
		return "csv", nil
	default:
		return "", fmt.Errorf("cannot detect data format for %q: use --from to specify json or csv", path)
	}
}

// LoadAndParseJSON reads a JSON file and parses it into raw instances.
//
// Returns (T, diag.Result, error) because the error return captures I/O
// failures (file not found, permission denied) which are distinct from
// semantic parse issues reported through the diag.Result. This is an
// internal CLI helper, not a public API.
func LoadAndParseJSON(ctx context.Context, path string) (map[string][]instance.RawInstance, diag.Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, diag.Result{}, fmt.Errorf("read data file: %w", err)
	}

	sourceID := location.NewSourceID("file://" + path)

	adapter, err := adapterjson.New(nil)
	if err != nil {
		return nil, diag.Result{}, fmt.Errorf("create json adapter: %w", err)
	}

	parsed, result := adapter.ParseObject(ctx, sourceID, data)
	return parsed, result, nil
}

// LoadAndParseCSV reads a CSV file and parses it into raw instances.
// The typeName parameter specifies the schema type for the rows.
// If typeColumn is non-empty, it is used instead of typeName for multi-type CSVs.
//
// Returns (T, diag.Result, error) because the error return captures I/O
// failures (file open errors) which are distinct from semantic parse
// issues reported through the diag.Result.
func LoadAndParseCSV(ctx context.Context, path string, typeName string, typeColumn string, s *schema.Schema) (map[string][]instance.RawInstance, diag.Result, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, diag.Result{}, fmt.Errorf("open data file: %w", err)
	}
	defer f.Close()

	sourceID := location.NewSourceID("file://" + path)
	adapter := csv.New()

	if typeColumn != "" {
		parsed, result := adapter.ParseWithTypeColumn(ctx, sourceID, f, func(name string) *schema.Type {
			t, _ := s.Type(name)
			return t
		})
		return parsed, result, nil
	}

	schemaType, _ := s.Type(typeName)
	raws, result := adapter.ParseTyped(ctx, sourceID, typeName, f, schemaType)
	return map[string][]instance.RawInstance{typeName: raws}, result, nil
}

// ValidateInstances validates parsed instances against the schema.
// Returns all valid instances and a merged diagnostic result.
func ValidateInstances(ctx context.Context, s *schema.Schema, parsed map[string][]instance.RawInstance) ([]*instance.ValidInstance, diag.Result) {
	v := instance.NewValidator(s)
	collector := diag.NewCollectorUnlimited()
	var allValid []*instance.ValidInstance

	for typeName, raws := range parsed {
		valids, result := v.Validate(ctx, typeName, raws)
		collector.Merge(result)
		for _, valid := range valids {
			if valid != nil {
				allValid = append(allValid, valid)
			}
		}
	}

	return allValid, collector.Result()
}

// BuildGraph creates a graph from validated instances, adds them all,
// and runs graph-level checks.
func BuildGraph(ctx context.Context, s *schema.Schema, valids []*instance.ValidInstance) (*graph.Graph, diag.Result) {
	g := graph.New(s)
	collector := diag.NewCollectorUnlimited()

	for _, valid := range valids {
		result := g.Add(ctx, valid)
		collector.Merge(result)
	}

	checkResult := g.Check(ctx)
	collector.Merge(checkResult)

	return g, collector.Result()
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
		return os.WriteFile(path, data, 0o600)
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

// LoadSnapshotFile reads a .ys file and loads it into a [*graph.Snapshot].
//
// Returns (T, diag.Result, error) following the CLI helper convention:
// error captures I/O failures, diag.Result captures semantic issues.
func LoadSnapshotFile(ctx context.Context, path string, s *schema.Schema, opts ...snapshot.LoadOption) (*graph.Snapshot, diag.Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, diag.Result{}, fmt.Errorf("read snapshot file: %w", err)
	}
	snap, result := snapshot.Load(ctx, data, s, opts...)
	return snap, result, nil
}
