package gogen

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/simon-lentz/yammm/schema"
)

// dateGoName is the emitted Date type. It is in reservedNames rather than
// reserved on demand, so the name is taken before any schema entity is
// assigned and cannot depend on whether a Date position exists.
const dateGoName = "Date"

// dateLayout is the stored form of a Date, the one adapter/json writes.
const dateLayout = time.DateOnly

// temporalTypes holds the generated types that carry a stored temporal
// string form: the Date type, and one type per distinct custom Timestamp
// layout. A default-layout Timestamp stays time.Time, whose own JSON codec
// already speaks RFC 3339 with nanoseconds — the form the library stores.
type temporalTypes struct {
	date    string            // dateGoName once emission names Date at some position
	layouts map[string]string // custom layout -> reserved Go type name
	helpers bool              // any codec-bearing type is emitted
}

// temporalDemand collects the temporal types emission will name while
// registerTemporalTypes dry-runs it.
type temporalDemand struct {
	date    bool
	layouts map[string]bool
}

// registerTemporalTypes dry-runs emission's own type resolution in collect
// mode over every position emission renders, so a Date or layout is registered
// exactly when emission reaches it. A layout's name depends on no order; the
// package doc's Type Mapping section states the rule.
func (g *generator) registerTemporalTypes() error {
	g.collect = &temporalDemand{layouts: map[string]bool{}}
	defer func() { g.collect = nil }()
	for _, sc := range g.schema.Closure() {
		for _, dt := range sc.DataTypesSlice() {
			// A temporal DataType is its own carrier (emitNamedTypes).
			if temporalLayout(dt.Constraint()) != "" {
				g.temporal.helpers = true
				continue
			}
			if isDefaultTimestamp(dt.Constraint()) {
				continue
			}
			if _, err := g.dataTypeBase(sc, dt); err != nil {
				return fmt.Errorf("gogen: datatype %q: %w", dt.Name(), err)
			}
		}
	}
	for _, t := range g.closureTypes() {
		owner := g.typeOwner(t)
		for _, p := range t.AllPropertiesSlice() {
			if _, err := g.goFieldType(owner, p); err != nil {
				return fmt.Errorf("%s property %q: %w", owner.label, p.Name(), err)
			}
		}
	}
	for _, e := range g.edges {
		targetOwner := g.typeOwner(e.target)
		for _, pk := range e.target.PrimaryKeysSlice() {
			if _, err := g.goFieldType(targetOwner, pk); err != nil {
				return err
			}
		}
		owner := g.edgeOwner(e.rel)
		for _, p := range e.rel.PropertiesSlice() {
			if _, err := g.goFieldType(owner, p); err != nil {
				return fmt.Errorf("%s property %q: %w", owner.label, p.Name(), err)
			}
		}
	}
	if g.collect.date {
		g.temporal.date = dateGoName
	}
	layouts := slices.Sorted(maps.Keys(g.collect.layouts))
	claimants := map[string]int{}
	for _, layout := range layouts {
		claimants[layoutTypeBase(layout)]++
	}
	g.temporal.layouts = make(map[string]string, len(layouts))
	for _, layout := range layouts {
		name := layoutTypeBase(layout)
		if claimants[name] > 1 || g.names.taken[name] {
			name = layoutTypeExact(layout)
		}
		if g.names.taken[name] {
			return fmt.Errorf("gogen: layout %q derived the Go name %q, which another declaration holds", layout, name)
		}
		g.names.taken[name] = true
		g.temporal.layouts[layout] = name
	}
	if g.temporal.date != "" || len(g.temporal.layouts) > 0 {
		g.temporal.helpers = true
	}
	return nil
}

// temporalLayout returns the stored string layout for a constraint that
// resolves to Date or to a custom-layout Timestamp, and "" for every other
// constraint, including a default-layout Timestamp.
func temporalLayout(c schema.Constraint) string {
	resolved := schema.ResolveAlias(c)
	switch resolved.Kind() {
	case schema.KindDate:
		return dateLayout
	case schema.KindTimestamp:
		if tc, ok := resolved.(schema.TimestampConstraint); ok {
			return tc.Format()
		}
	}
	return ""
}

// isDefaultTimestamp reports whether c resolves to a Timestamp without a
// declared layout.
func isDefaultTimestamp(c schema.Constraint) bool {
	tc, ok := schema.ResolveAlias(c).(schema.TimestampConstraint)
	return ok && tc.Format() == ""
}

// layoutTypeBase is a per-layout type's name when nothing else claims it:
// "Timestamp" and every letter and digit of the layout.
func layoutTypeBase(layout string) string {
	var b strings.Builder
	b.WriteString("Timestamp")
	for _, r := range layout {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// layoutTypeExact is a per-layout type's name when its base is claimed:
// "Timestamp_", then the layout with each other rune written "_<hex>_", so no
// two layouts share it. A layout is valid UTF-8, since the DSL and the Builder
// refuse any other. No other emitted name starts "Timestamp_": each is a
// reserved name, an "EDGE_" name, or built from identifiers goExportedIdent
// derives, which hold "_" only after their leading "X" or between two digit
// runs.
func layoutTypeExact(layout string) string {
	var b strings.Builder
	b.WriteString("Timestamp_")
	for _, r := range layout {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			continue
		}
		fmt.Fprintf(&b, "_%X_", r)
	}
	return b.String()
}

// emitTemporalTypes writes the codec helpers, the Date type and every
// per-layout type, before the named types that may reference them.
func (g *generator) emitTemporalTypes() {
	if g.temporal.helpers {
		g.emitTemporalHelpers()
	}
	if g.temporal.date != "" {
		g.emitTemporalDecl(g.temporal.date, dateLayout)
	}
	for _, layout := range slices.Sorted(maps.Keys(g.temporal.layouts)) {
		g.emitTemporalDecl(g.temporal.layouts[layout], layout)
	}
}

// emitTemporalHelpers writes the two unexported functions every temporal
// codec delegates to. Unexported names cannot collide with schema-derived
// identifiers, which are always exported, so they need no reservation.
func (g *generator) emitTemporalHelpers() {
	g.needsTime = true
	g.needsJSON = true
	g.buf.WriteString(`// marshalTemporal renders t through layout as a JSON string.
func marshalTemporal(t time.Time, layout string) ([]byte, error) {
	return json.Marshal(t.Format(layout))
}

// unmarshalTemporal parses a JSON string through layout into *dst. A JSON
// null leaves *dst unchanged, as time.Time's own UnmarshalJSON does.
func unmarshalTemporal(b []byte, layout string, dst *time.Time) error {
	if string(b) == "null" {
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	t, err := time.Parse(layout, s)
	if err != nil {
		return err
	}
	*dst = t
	return nil
}

`)
}

// emitTemporalDecl writes a struct embedding time.Time, its unexported layout
// const, and a JSON codec pair exchanging the value in that layout — the
// string form adapter/json writes. Embedding keeps time.Time's methods
// promoted, so a .Format caller compiles unchanged.
func (g *generator) emitTemporalDecl(name, layout string) {
	g.needsTime = true
	g.needsJSON = true
	layoutConst := lowerFirst(name) + "Layout"
	fmt.Fprintf(g.buf, "// %s is exchanged as a JSON string in the layout %s.\n", name, strconv.Quote(layout))
	fmt.Fprintf(g.buf, "type %s struct{ time.Time }\n\n", name)
	fmt.Fprintf(g.buf, "const %s = %s\n\n", layoutConst, strconv.Quote(layout))
	fmt.Fprintf(g.buf, "func (v %s) MarshalJSON() ([]byte, error) { return marshalTemporal(v.Time, %s) }\n\n", name, layoutConst)
	fmt.Fprintf(g.buf, "func (v *%s) UnmarshalJSON(b []byte) error { return unmarshalTemporal(b, %s, &v.Time) }\n\n", name, layoutConst)
}

// emitDefaultTimestampDecl writes a struct embedding time.Time and nothing
// else: the promoted codec already exchanges RFC 3339 with nanoseconds,
// which is the stored form of a default-layout Timestamp.
func (g *generator) emitDefaultTimestampDecl(name string) {
	g.needsTime = true
	fmt.Fprintf(g.buf, "type %s struct{ time.Time }\n\n", name)
}

// lowerFirst returns s with its first rune lower-cased, the unexported
// spelling of an exported identifier.
func lowerFirst(s string) string {
	for i, r := range s {
		return string(unicode.ToLower(r)) + s[i+len(string(r)):]
	}
	return s
}
