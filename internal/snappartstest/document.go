// Package snappartstest writes .ys documents from part descriptions, for tests
// that drive the snapshot reader and what it builds.
package snappartstest

import (
	"bytes"
	"cmp"
	"encoding/json"
	"maps"
	"math"
	"slices"
	"strconv"
	"testing"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// Loaded loads the document [Document] writes for parts and fails tb when the
// reader refuses it.
func Loaded(tb testing.TB, s schema.Schema, parts snapparts.Parts) graph.Snapshot {
	tb.Helper()
	snap, res := snapshot.Load(tb.Context(), Document(tb, s, parts), s)
	if res.HasErrors() {
		tb.Fatalf("snappartstest.Loaded: %s", res)
	}
	return snap
}

// Document writes parts as the .ys document that states them, for a fixture the
// reader judges: a document may hold what a validator refuses, such as a number
// no finite float64 holds, or a text under an Integer. Its integrity hash is empty,
// which no check compares, and each edge stands under the root its source key
// spells. A value no document holds, such as a time.Time or a string that is not
// valid UTF-8, fails tb.
func Document(tb testing.TB, s schema.Schema, parts snapparts.Parts) []byte {
	tb.Helper()
	w := documentWriter{tb: tb, s: s, names: make(map[schema.TypeID]tableEntry), rows: make(map[schema.TypeID]int)}
	for cs := range s.Closure() {
		for t := range cs.Types() {
			w.names[t.ID()] = tableEntry{Schema: cs.Name(), Name: t.Name()}
		}
	}
	w.collect(parts)

	type document struct {
		Header    header        `json:"yammm_snapshot"`
		Types     []tableEntry  `json:"types"`
		Instances []rootGroup   `json:"instances"`
		Diags     documentDiags `json:"diagnostics"`
	}
	doc := document{
		Header: header{
			Version:             snapshot.MinReadableVersion,
			SchemaName:          s.Name(),
			SchemaSource:        s.SourceID().String(),
			SchemaHash:          schema.StructuralHash(s),
			SchemaHashAlgorithm: schema.StructuralHashVersion,
			Features:            []string{},
		},
		Types:     w.table,
		Instances: w.groups(parts),
		Diags:     documentDiags{Duplicates: []duplicateRecord{}, Unresolved: []unresolvedRecord{}},
	}
	if a := parts.Attestation; a != nil {
		doc.Header.Attestation = &attestation{Values: a.Values, Associations: a.Associations}
	}
	for _, d := range parts.Duplicates {
		key := w.values(d.PrimaryKey.Clone())
		doc.Diags.Duplicates = append(doc.Diags.Duplicates, duplicateRecord{
			Type: w.row(d.TypeID), Key: key, Instance: w.instance(d, nil, false),
			Conflict: conflict{Type: w.row(d.TypeID), Key: key},
		})
	}
	for _, u := range parts.Unresolved {
		rel := w.relation(u.SourceType, u.Relation)
		doc.Diags.Unresolved = append(doc.Diags.Unresolved, unresolvedRecord{
			SourceType: w.row(u.SourceType), SourceKey: w.values(u.SourceKey.Clone()), Relation: u.Relation,
			TargetType: w.row(rel.TargetID()), TargetKey: w.values(u.TargetKey.Clone()), Required: !rel.IsOptional(),
			Reason: u.Reason, Properties: w.members(u.Properties.Clone()),
		})
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		tb.Fatalf("snappartstest.Document: %v", err)
	}
	return bytes.TrimSpace(buf.Bytes())
}

// The wire shapes the reader decodes, field for field in the order Marshal writes
// them.
type (
	header struct {
		Version             int          `json:"version"`
		SchemaName          string       `json:"schema_name"`
		SchemaSource        string       `json:"schema_source"`
		SchemaHash          string       `json:"schema_hash"`
		SchemaHashAlgorithm int          `json:"schema_hash_algorithm"`
		IntegrityHash       string       `json:"integrity_hash"`
		Features            []string     `json:"features"`
		Attestation         *attestation `json:"attestation,omitempty"`
	}
	attestation struct {
		Values       bool `json:"values"`
		Associations bool `json:"associations"`
	}
	tableEntry struct {
		Schema string `json:"schema"`
		Name   string `json:"name"`
	}
	rootGroup struct {
		Type  int              `json:"type"`
		Items []instanceRecord `json:"items"`
	}
	instanceRecord struct {
		Key        []any                       `json:"key"`
		Type       *int                        `json:"type,omitempty"`
		Properties map[string]any              `json:"properties"`
		Edges      map[string][]edgeRecord     `json:"edges,omitempty"`
		Composed   map[string][]instanceRecord `json:"composed,omitempty"`
		Provenance *provenance                 `json:"provenance"`
	}
	edgeRecord struct {
		TargetType int            `json:"target_type"`
		TargetKey  []any          `json:"target_key"`
		Properties map[string]any `json:"properties"`
	}
	provenance struct {
		SourceName string `json:"source_name"`
		Path       string `json:"path"`
	}
	conflict struct {
		Type int   `json:"type"`
		Key  []any `json:"key"`
	}
	duplicateRecord struct {
		Type     int            `json:"type"`
		Key      []any          `json:"key"`
		Instance instanceRecord `json:"instance"`
		Conflict conflict       `json:"conflict"`
	}
	unresolvedRecord struct {
		SourceType int            `json:"source_type"`
		SourceKey  []any          `json:"source_key"`
		Relation   string         `json:"relation"`
		TargetType int            `json:"target_type"`
		TargetKey  []any          `json:"target_key"`
		Required   bool           `json:"required"`
		Reason     string         `json:"reason"`
		Properties map[string]any `json:"properties,omitempty"`
	}
	documentDiags struct {
		Duplicates []duplicateRecord  `json:"duplicates"`
		Unresolved []unresolvedRecord `json:"unresolved"`
	}
)

type documentWriter struct {
	tb    testing.TB
	s     schema.Schema
	names map[schema.TypeID]tableEntry
	rows  map[schema.TypeID]int
	table []tableEntry
}

// collect fixes the types table: every identity parts names or reaches through an
// association, ordered as Marshal orders it.
func (w *documentWriter) collect(parts snapparts.Parts) {
	seen := make(map[schema.TypeID]struct{})
	var inst func(snapparts.Instance)
	inst = func(ip snapparts.Instance) {
		seen[ip.TypeID] = struct{}{}
		for _, children := range ip.Composed {
			for _, child := range children {
				inst(child)
			}
		}
	}
	for _, id := range parts.Types {
		seen[id] = struct{}{}
	}
	for _, ip := range parts.Instances {
		inst(ip)
	}
	for _, ip := range parts.Duplicates {
		inst(ip)
	}
	for _, e := range parts.Edges {
		seen[w.relation(e.SourceType, e.Relation).TargetID()] = struct{}{}
	}
	for _, u := range parts.Unresolved {
		seen[u.SourceType] = struct{}{}
		seen[w.relation(u.SourceType, u.Relation).TargetID()] = struct{}{}
	}
	ids := slices.SortedFunc(maps.Keys(seen), func(a, b schema.TypeID) int {
		ea, eb := w.entry(a), w.entry(b)
		return cmp.Or(cmp.Compare(ea.Schema, eb.Schema), cmp.Compare(ea.Name, eb.Name))
	})
	for i, id := range ids {
		w.table = append(w.table, w.entry(id))
		w.rows[id] = i
	}
}

func (w *documentWriter) entry(id schema.TypeID) tableEntry {
	w.tb.Helper()
	e, ok := w.names[id]
	if !ok {
		w.tb.Fatalf("snappartstest.Document: the schema's closure holds no type %s", id)
	}
	return e
}

func (w *documentWriter) row(id schema.TypeID) int {
	w.tb.Helper()
	w.entry(id)
	return w.rows[id]
}

func (w *documentWriter) relation(source schema.TypeID, name string) schema.Relation {
	w.tb.Helper()
	t, ok := w.s.TypeByID(source)
	if !ok {
		w.tb.Fatalf("snappartstest.Document: the schema holds no type %s", source)
	}
	rel, ok := t.Relation(name)
	if !ok {
		w.tb.Fatalf("snappartstest.Document: %s declares no relation %q", source, name)
	}
	return rel
}

// groups writes one entry per root type and per denoted type, in table order, and
// files each edge under its source root.
func (w *documentWriter) groups(parts snapparts.Parts) []rootGroup {
	w.tb.Helper()
	byType := make(map[schema.TypeID][]snapparts.Instance)
	for _, id := range parts.Types {
		byType[id] = nil
	}
	for _, ip := range parts.Instances {
		byType[ip.TypeID] = append(byType[ip.TypeID], ip)
	}
	edges := make(map[string][]snapparts.Edge)
	address := func(id schema.TypeID, key string) string { return id.String() + "\x00" + key }
	for _, e := range parts.Edges {
		at := address(e.SourceType, e.SourceKey.String())
		edges[at] = append(edges[at], e)
	}
	filed := 0
	groups := make([]rootGroup, 0, len(byType))
	for _, id := range slices.SortedFunc(maps.Keys(byType), func(a, b schema.TypeID) int {
		return cmp.Compare(w.row(a), w.row(b))
	}) {
		items := make([]instanceRecord, 0, len(byType[id]))
		for _, ip := range byType[id] {
			own := edges[address(id, ip.PrimaryKey.String())]
			filed += len(own)
			items = append(items, w.instance(ip, own, false))
		}
		groups = append(groups, rootGroup{Type: w.row(id), Items: items})
	}
	if filed != len(parts.Edges) {
		w.tb.Fatalf("snappartstest.Document: %d of %d edges name a root by the key it spells", filed, len(parts.Edges))
	}
	return groups
}

func (w *documentWriter) instance(ip snapparts.Instance, edges []snapparts.Edge, carryType bool) instanceRecord {
	w.tb.Helper()
	rec := instanceRecord{
		Key:        w.values(ip.PrimaryKey.Clone()),
		Properties: w.members(ip.Properties.Clone()),
		Provenance: provenanceOf(ip.Provenance),
	}
	if carryType {
		row := w.row(ip.TypeID)
		rec.Type = &row
	}
	for _, e := range edges {
		if rec.Edges == nil {
			rec.Edges = make(map[string][]edgeRecord)
		}
		props := w.members(e.Properties.Clone())
		if props == nil {
			props = map[string]any{}
		}
		rec.Edges[e.Relation] = append(rec.Edges[e.Relation], edgeRecord{
			TargetType: w.row(w.relation(e.SourceType, e.Relation).TargetID()),
			TargetKey:  w.values(e.TargetKey.Clone()),
			Properties: props,
		})
	}
	for rel, children := range ip.Composed {
		if rec.Composed == nil {
			rec.Composed = make(map[string][]instanceRecord, len(ip.Composed))
		}
		kids := make([]instanceRecord, 0, len(children))
		for _, child := range children {
			kids = append(kids, w.instance(child, nil, true))
		}
		rec.Composed[rel] = kids
	}
	return rec
}

func provenanceOf(p location.Provenance) *provenance {
	if p.IsZero() {
		return nil
	}
	text := p.Path().String()
	if p.HasRawPath() {
		text = p.RawPath()
	}
	return &provenance{SourceName: p.SourceName(), Path: text}
}

func (w *documentWriter) values(vs []any) []any {
	w.tb.Helper()
	for i, v := range vs {
		vs[i] = w.value(v)
	}
	return vs
}

func (w *documentWriter) members(m map[string]any) map[string]any {
	w.tb.Helper()
	for k, v := range m {
		if !utf8.ValidString(k) {
			w.tb.Fatalf("snappartstest.Document: no document holds the member %q, which is not valid UTF-8", k)
		}
		m[k] = w.value(v)
	}
	return m
}

// value writes one value as encoding/json would, with a float's indicator kept so
// the reader reads back a float.
func (w *documentWriter) value(v any) any {
	w.tb.Helper()
	switch t := v.(type) {
	case nil, bool, int64, json.Number:
		return v
	case string:
		if !utf8.ValidString(t) {
			w.tb.Fatalf("snappartstest.Document: no document holds the string %q, which is not valid UTF-8", t)
		}
		return v
	case float64:
		if math.IsNaN(t) || math.IsInf(t, 0) {
			w.tb.Fatalf("snappartstest.Document: no document holds the float %v", t)
		}
		text := strconv.FormatFloat(t, 'g', -1, 64)
		if !bytes.ContainsAny([]byte(text), ".eE") {
			text += ".0"
		}
		return json.RawMessage(text)
	case []any:
		return w.values(t)
	case map[string]any:
		return w.members(t)
	}
	w.tb.Fatalf("snappartstest.Document: no document holds a value of type %T", v)
	return nil
}
