// Package snapshottest provides test helpers for snapshot persistence tests.
//
// This is a separate package (not _test.go helpers in snapshot/) so that it
// is available to both snapshot/ tests and cmd/yammm/ CLI integration tests.
package snapshottest

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

var update = flag.Bool("update", false, "update golden files")

// ReadGolden reads a golden .ys file from snapshot/testdata/<name>.ys.
// Calls t.Fatal if the file does not exist or cannot be read.
func ReadGolden(tb testing.TB, name string) []byte {
	tb.Helper()
	path := goldenPath(name)
	data, err := os.ReadFile(path)
	if err != nil {
		tb.Fatalf("ReadGolden(%q): %v", name, err)
	}
	return data
}

// UpdateGolden writes data to snapshot/testdata/<name>.ys when the test is
// run with the -update flag (e.g., "go test -update ./snapshot/...").
// When -update is not set, UpdateGolden is a no-op.
func UpdateGolden(tb testing.TB, name string, data []byte) {
	tb.Helper()
	if !*update {
		return
	}
	path := goldenPath(name)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		tb.Fatalf("UpdateGolden(%q): %v", name, err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		tb.Fatalf("UpdateGolden(%q): %v", name, err)
	}
	tb.Logf("Updated golden file: %s", path)
}

// AssertRoundTrip marshals a snapshot, loads it back, and verifies structural
// equivalence via [CompareSnapshots]. Fails the test with a detailed diff on
// mismatch.
func AssertRoundTrip(tb testing.TB, snap *graph.Snapshot, s *schema.Schema, opts ...snapshot.Option) {
	tb.Helper()
	ctx := context.Background()

	data, result := snapshot.Marshal(ctx, snap, opts...)
	if err := result.Err(); err != nil {
		tb.Fatalf("AssertRoundTrip: Marshal failed: %v", err)
	}

	loaded, result := snapshot.Load(ctx, data, s)
	if err := result.Err(); err != nil {
		tb.Fatalf("AssertRoundTrip: Load failed: %v", err)
	}

	if err := CompareSnapshots(snap, loaded); err != nil {
		tb.Errorf("AssertRoundTrip: snapshots differ:\n%v", err)
	}
}

// AssertDeterministic marshals a snapshot twice with the same options and
// verifies byte-level equality.
func AssertDeterministic(tb testing.TB, snap *graph.Snapshot, _ *schema.Schema, opts ...snapshot.Option) {
	tb.Helper()
	ctx := context.Background()

	data1, result := snapshot.Marshal(ctx, snap, opts...)
	if err := result.Err(); err != nil {
		tb.Fatalf("AssertDeterministic: first Marshal failed: %v", err)
	}

	data2, result := snapshot.Marshal(ctx, snap, opts...)
	if err := result.Err(); err != nil {
		tb.Fatalf("AssertDeterministic: second Marshal failed: %v", err)
	}

	if string(data1) != string(data2) {
		tb.Error("AssertDeterministic: two marshals produced different output")
	}
}

// CompareSnapshots performs a deep structural comparison between two snapshots
// using typed accessors. Returns nil if the snapshots are structurally
// equivalent, or an error describing all differences found.
func CompareSnapshots(a, b *graph.Snapshot) error {
	var diffs []string

	// Types.
	aTypes := a.Types()
	bTypes := b.Types()
	if len(aTypes) != len(bTypes) {
		diffs = append(diffs, fmt.Sprintf("Types: len %d vs %d", len(aTypes), len(bTypes)))
	} else {
		for i := range aTypes {
			if aTypes[i] != bTypes[i] {
				diffs = append(diffs, fmt.Sprintf("Types[%d]: %q vs %q", i, aTypes[i], bTypes[i]))
			}
		}
	}

	// Instances per type.
	for _, typeName := range aTypes {
		aInsts := a.InstancesOf(typeName)
		bInsts := b.InstancesOf(typeName)
		if len(aInsts) != len(bInsts) {
			diffs = append(diffs, fmt.Sprintf("InstancesOf(%q): len %d vs %d", typeName, len(aInsts), len(bInsts)))
			continue
		}
		for i := range aInsts {
			ai, bi := aInsts[i], bInsts[i]
			prefix := fmt.Sprintf("%s[%s]", typeName, ai.PrimaryKey().String())

			if ai.PrimaryKey().String() != bi.PrimaryKey().String() {
				diffs = append(diffs, fmt.Sprintf("%s: PrimaryKey %q vs %q",
					prefix, ai.PrimaryKey().String(), bi.PrimaryKey().String()))
			}

			// Properties via Clone() deep equality.
			aProps := ai.Properties().Clone()
			bProps := bi.Properties().Clone()
			if !mapsEqualAny(aProps, bProps) {
				diffs = append(diffs, prefix+": Properties differ")
			}

			// Edges via EdgesFrom.
			aEdges := a.EdgesFrom(ai)
			bEdges := b.EdgesFrom(bi)
			if len(aEdges) != len(bEdges) {
				diffs = append(diffs, fmt.Sprintf("%s: EdgesFrom len %d vs %d",
					prefix, len(aEdges), len(bEdges)))
			} else {
				for j := range aEdges {
					ae, be := aEdges[j], bEdges[j]
					if ae.Relation() != be.Relation() ||
						ae.Target().TypeName() != be.Target().TypeName() ||
						ae.Target().PrimaryKey().String() != be.Target().PrimaryKey().String() {
						diffs = append(diffs, fmt.Sprintf("%s: Edge[%d] relation/target differ", prefix, j))
					}
					if !mapsEqualAny(ae.Properties().Clone(), be.Properties().Clone()) {
						diffs = append(diffs, fmt.Sprintf("%s: Edge[%d] properties differ", prefix, j))
					}
				}
			}

			// Compositions.
			aRels := ai.ComposedRelations()
			bRels := bi.ComposedRelations()
			if len(aRels) != len(bRels) {
				diffs = append(diffs, fmt.Sprintf("%s: ComposedRelations len %d vs %d",
					prefix, len(aRels), len(bRels)))
			} else {
				for _, rel := range aRels {
					aChildren := ai.Composed(rel)
					bChildren := bi.Composed(rel)
					if len(aChildren) != len(bChildren) {
						diffs = append(diffs, fmt.Sprintf("%s.Composed(%q): len %d vs %d",
							prefix, rel, len(aChildren), len(bChildren)))
						continue
					}
					for k := range aChildren {
						ac, bc := aChildren[k], bChildren[k]
						if ac.PrimaryKey().String() != bc.PrimaryKey().String() {
							diffs = append(diffs, fmt.Sprintf("%s.Composed(%q)[%d]: PK differ",
								prefix, rel, k))
						}
						if !mapsEqualAny(ac.Properties().Clone(), bc.Properties().Clone()) {
							diffs = append(diffs, fmt.Sprintf("%s.Composed(%q)[%d]: Properties differ",
								prefix, rel, k))
						}
					}
				}
			}

			// Provenance: compare source name and path (ignore span, which is zero for loaded).
			aProv := ai.Provenance()
			bProv := bi.Provenance()
			if (aProv == nil) != (bProv == nil) {
				diffs = append(diffs, prefix+": Provenance nil mismatch")
			} else if aProv != nil {
				if aProv.SourceName() != bProv.SourceName() {
					diffs = append(diffs, fmt.Sprintf("%s: Provenance.SourceName %q vs %q",
						prefix, aProv.SourceName(), bProv.SourceName()))
				}
			}
		}
	}

	// Duplicates.
	aDups := a.Duplicates()
	bDups := b.Duplicates()
	if len(aDups) != len(bDups) {
		diffs = append(diffs, fmt.Sprintf("Duplicates: len %d vs %d", len(aDups), len(bDups)))
	} else {
		for i := range aDups {
			ad, bd := aDups[i], bDups[i]
			if ad.Instance.TypeName() != bd.Instance.TypeName() ||
				ad.Instance.PrimaryKey().String() != bd.Instance.PrimaryKey().String() {
				diffs = append(diffs, fmt.Sprintf("Duplicates[%d]: type/key differ", i))
			}
		}
	}

	// Unresolved.
	aUnres := a.Unresolved()
	bUnres := b.Unresolved()
	if len(aUnres) != len(bUnres) {
		diffs = append(diffs, fmt.Sprintf("Unresolved: len %d vs %d", len(aUnres), len(bUnres)))
	} else {
		for i := range aUnres {
			au, bu := aUnres[i], bUnres[i]
			if au.Source.TypeName() != bu.Source.TypeName() ||
				au.Relation != bu.Relation ||
				au.TargetType != bu.TargetType ||
				au.TargetKey != bu.TargetKey ||
				au.Required != bu.Required ||
				au.Reason != bu.Reason {
				diffs = append(diffs, fmt.Sprintf("Unresolved[%d]: differ", i))
			}
		}
	}

	if len(diffs) == 0 {
		return nil
	}
	return fmt.Errorf("snapshot differences:\n  %s", strings.Join(diffs, "\n  "))
}

// mapsEqualAny performs a best-effort deep comparison of two map[string]any values.
func mapsEqualAny(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok {
			return false
		}
		if !valuesEqual(av, bv) {
			return false
		}
	}
	return true
}

func valuesEqual(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	// Handle numeric type coercion (int64 vs float64).
	switch av := a.(type) {
	case int64:
		switch bv := b.(type) {
		case int64:
			return av == bv
		case float64:
			return float64(av) == bv
		}
	case float64:
		switch bv := b.(type) {
		case float64:
			return av == bv
		case int64:
			return av == float64(bv)
		}
	case string:
		if bv, ok := b.(string); ok {
			return av == bv
		}
	case bool:
		if bv, ok := b.(bool); ok {
			return av == bv
		}
	case map[string]any:
		if bv, ok := b.(map[string]any); ok {
			return mapsEqualAny(av, bv)
		}
	case []any:
		if bv, ok := b.([]any); ok {
			if len(av) != len(bv) {
				return false
			}
			for i := range av {
				if !valuesEqual(av[i], bv[i]) {
					return false
				}
			}
			return true
		}
	}
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

func goldenPath(name string) string {
	// Get the directory of this source file to find testdata relative to it.
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "testdata", name+".ys")
}
