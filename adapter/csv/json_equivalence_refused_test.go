package csv

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location/path"
	"github.com/simon-lentz/yammm/schema"
)

// unrefused copies props with each [instance.Refused] replaced by the text it
// holds: the object the JSON adapter makes from the equivalent document.
func unrefused(props map[string]any) map[string]any {
	if props == nil {
		return nil
	}
	out := make(map[string]any, len(props))
	for k, v := range props {
		out[k] = unrefusedValue(v)
	}
	return out
}

func unrefusedValue(v any) any {
	switch x := v.(type) {
	case instance.Refused:
		return x.Value
	case map[string]any:
		return unrefused(x)
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = unrefusedValue(e)
		}
		return out
	}
	return v
}

// refusedPaths is the path of each [instance.Refused] in props, as the
// validator names the position: a property, or an edge object's member.
func refusedPaths(props map[string]any) map[string]bool {
	out := map[string]bool{}
	var walk func(at path.Builder, v any)
	walk = func(at path.Builder, v any) {
		switch x := v.(type) {
		case instance.Refused:
			out[at.String()] = true
		case map[string]any:
			for k, e := range x {
				walk(at.Key(k), e)
			}
		case []any:
			for i, e := range x {
				walk(at.Index(i), e)
			}
		}
	}
	walk(path.Root(), props)
	return out
}

// pathedVerdict is [verdict] with each issue's path, sorted, and whether the
// instance validated.
func pathedVerdict(t *testing.T, s *schema.Schema, raw instance.RawInstance, opts ...instance.Option) ([]string, bool) {
	t.Helper()
	raw.Provenance = nil
	valid, res := instance.NewValidator(s, opts...).ValidateOne(t.Context(), "Person", raw)
	var out []string
	for issue := range res.Issues() {
		out = append(out, fmt.Sprintf("%s\t%s %s", issue.Path(), issue.Code(), issue.Message()))
	}
	slices.Sort(out)
	return out, valid != nil
}

// judgedAlike holds the one-break rule between a CSV row and its JSON
// equivalent, and returns why they differ, or "". The validator answers the
// row as it answers the document, less each issue the document draws at a cell
// the parser refused: the parser's E_CSV_COERCE stands in its place. A row
// that holds a refused cell never validates; any other validates as the
// document does.
func judgedAlike(t *testing.T, s *schema.Schema, csvRaw, jsonRaw instance.RawInstance, opts ...instance.Option) string {
	t.Helper()
	refused := refusedPaths(csvRaw.Properties)
	got, rowValid := pathedVerdict(t, s, csvRaw, opts...)
	want, docValid := pathedVerdict(t, s, jsonRaw, opts...)
	left := map[string]int{}
	for _, issue := range got {
		left[issue]++
	}
	var why []string
	for _, issue := range want {
		if left[issue] > 0 {
			left[issue]--
			continue
		}
		if at, _, _ := strings.Cut(issue, "\t"); !refused[at] {
			why = append(why, "the row draws no "+issue)
		}
	}
	for _, issue := range slices.Sorted(maps.Keys(left)) {
		for range left[issue] {
			why = append(why, "the document draws no "+issue)
		}
	}
	switch {
	case len(refused) > 0 && rowValid:
		why = append(why, "the row validates beside a refused cell")
	case len(refused) == 0 && rowValid != docValid:
		why = append(why, fmt.Sprintf("the row validates %v, the document %v", rowValid, docValid))
	}
	return strings.Join(why, "\n  ")
}
