package jschema

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

// duplicateKeys walks the document at the TOKEN level and reports every object
// that carries the same key twice.
//
// This cannot be done with json.Unmarshal: decoding into map[string]any is
// last-one-wins, which is exactly how the duplicate survived — the package's
// own selfCheck decodes that way and saw one description where two were
// emitted.
func duplicateKeys(t *testing.T, doc []byte) []string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(doc))
	var dupes []string
	var path []string

	var walkValue func()

	walkObject := func() {
		seen := make(map[string]bool)
		for dec.More() {
			tok, err := dec.Token()
			if err != nil {
				t.Fatalf("token: %v", err)
			}
			key, ok := tok.(string)
			if !ok {
				t.Fatalf("object key is %T, want string", tok)
			}
			if seen[key] {
				dupes = append(dupes, strings.Join(path, "/")+"/"+key)
			}
			seen[key] = true
			path = append(path, key)
			walkValue()
			path = path[:len(path)-1]
		}
		if _, err := dec.Token(); err != nil { // closing brace
			t.Fatalf("closing token: %v", err)
		}
	}

	walkValue = func() {
		tok, err := dec.Token()
		if err != nil {
			t.Fatalf("token: %v", err)
		}
		delim, isDelim := tok.(json.Delim)
		if !isDelim {
			return
		}
		switch delim {
		case '{':
			walkObject()
		case '[':
			for i := 0; dec.More(); i++ {
				path = append(path, strconv.Itoa(i))
				walkValue()
				path = path[:len(path)-1]
			}
			if _, err := dec.Token(); err != nil {
				t.Fatalf("closing token: %v", err)
			}
		}
	}

	walkValue()
	return dupes
}

// A documented property whose type carries its own description renders ONE
// description, not two. withDescription appended unconditionally, so a
// documented `Timestamp["<layout>"]` emitted the layout and then the
// doc-comment as separate members — and every last-one-wins reader dropped the
// layout the section promises.
//
// Mutation: reverting withDescription to a bare append turns this red at three
// paths, one per call site that can already carry a description.
func TestDuplicateDescription_NoObjectCarriesTwo(t *testing.T) {
	for _, name := range []string{"documented_layouts", "docs", "timestamp_formats", "edge_props"} {
		t.Run(name, func(t *testing.T) {
			got, err := Marshal(loadSchema(t, name))
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if dupes := duplicateKeys(t, got); len(dupes) != 0 {
				t.Errorf("duplicate keys emitted at %v", dupes)
			}
		})
	}
}

// The merge keeps BOTH halves: the constraint's source form and the
// doc-comment, in that order, separated by one space — the shape
// compositionFrag and associationFrag already use.
func TestDuplicateDescription_MergeKeepsBothHalves(t *testing.T) {
	got, err := Marshal(loadSchema(t, "documented_layouts"))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	for _, want := range []string{
		`Timestamp[\"2006-01-02 15:04:05\"] logged_at is when the operator filed the reading.`,
		`Timestamp[\"2006-01-02 15:04:05\"] observed_at is when the sensor sampled.`,
		`Timestamp[\"2006-01-02 15:04:05\"] Stamp is a wall-clock reading with no zone.`,
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("merged description missing:\n  want %s", want)
		}
	}
}

// An undocumented property of the same type still renders the layout alone, so
// the merge did not swallow the constraint half when there is nothing to append.
func TestDuplicateDescription_UndocumentedKeepsTheLayout(t *testing.T) {
	got, err := Marshal(loadSchema(t, "documented_layouts"))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(got), `"undocumented"`) {
		t.Fatal("the control property is absent from the document")
	}
	if !strings.Contains(string(got), `Timestamp[\"2006-01-02 15:04:05\"]"`) {
		t.Error("an undocumented layout property lost its source form")
	}
}
