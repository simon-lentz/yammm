package diag

import (
	"encoding/json"

	"github.com/simon-lentz/yammm/location"
)

// The wire types define the stable JSON output format: field names are
// camelCase, and optional fields use omitzero.

// issueWire is the JSON wire format for Issue.
type issueWire struct {
	Span       *spanWire         `json:"span,omitzero"`
	SourceName string            `json:"sourceName,omitzero"`
	Path       string            `json:"path,omitzero"`
	Severity   string            `json:"severity"`
	Code       string            `json:"code"`
	Message    string            `json:"message"`
	Hint       string            `json:"hint,omitzero"`
	Related    []relatedInfoWire `json:"related,omitzero"`
	Details    []detailWire      `json:"details,omitzero"`
}

// spanWire is the JSON wire format for location.Span.
type spanWire struct {
	Source string       `json:"source"`
	Start  positionWire `json:"start"`
	End    positionWire `json:"end"`
}

// positionWire is the JSON wire format for location.Position. A valid
// position writes its offset, 0 included; one that is not valid writes none.
type positionWire struct {
	Line   int  `json:"line"`
	Column int  `json:"column"`
	Offset *int `json:"offset,omitzero"`
}

// relatedInfoWire is the JSON wire format for RelatedInfo.
type relatedInfoWire struct {
	Message string    `json:"message"`
	Span    *spanWire `json:"span,omitzero"`
}

// detailWire is the JSON wire format for Detail.
type detailWire struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// resultWire is the JSON wire format for Result.
type resultWire struct {
	Issues       []issueWire `json:"issues"`
	LimitReached bool        `json:"limitReached,omitzero"`
	DroppedCount int         `json:"droppedCount,omitzero"`
}

// FormatResultJSON returns the JSON representation of a diagnostic result: an
// array of issues, and the truncation facts when issues were dropped.
// Every span's source is its identity, whatever module root the renderer
// holds.
func (r *Renderer) FormatResultJSON(res Result) json.RawMessage {
	wire := toResultWire(res)
	//nolint:errchkjson // Wire types are safe; error check is defensive
	data, err := json.Marshal(wire)
	if err != nil {
		// This should never happen with our wire types
		panic("diag: unexpected JSON marshal error: " + err.Error())
	}
	return data
}

// toResultWire converts a Result to its JSON wire format.
func toResultWire(res Result) resultWire {
	var issues []issueWire
	for issue := range res.Issues() {
		issues = append(issues, toIssueWire(issue))
	}

	// Ensure empty slice becomes empty array, not null
	if issues == nil {
		issues = []issueWire{}
	}

	wire := resultWire{
		Issues: issues,
	}

	// Only include the truncation facts when issues were dropped
	if res.LimitReached() {
		wire.LimitReached = true
		wire.DroppedCount = res.DroppedCount()
	}

	return wire
}

// toIssueWire converts an Issue to its JSON wire format.
func toIssueWire(issue Issue) issueWire {
	wire := issueWire{
		Severity: issue.Severity().String(),
		Code:     issue.Code().String(),
		Message:  issue.Message(),
	}

	// Optional span
	if issue.HasSpan() {
		wire.Span = toSpanWire(issue.Span())
	}

	// Optional source name
	if name := issue.SourceName(); name != "" {
		wire.SourceName = name
	}

	// Optional path
	if path := issue.Path(); path != "" {
		wire.Path = path
	}

	// Optional hint
	if hint := issue.Hint(); hint != "" {
		wire.Hint = hint
	}

	// Optional related info
	related := issue.Related()
	if len(related) > 0 {
		wire.Related = make([]relatedInfoWire, len(related))
		for i, rel := range related {
			wire.Related[i] = toRelatedInfoWire(rel)
		}
	}

	// Optional details
	details := issue.Details()
	if len(details) > 0 {
		wire.Details = make([]detailWire, len(details))
		for i, d := range details {
			wire.Details[i] = detailWire(d)
		}
	}

	return wire
}

// toSpanWire converts a location.Span to its JSON wire format.
// Returns nil for zero spans.
func toSpanWire(span location.Span) *spanWire {
	if span.IsZero() {
		return nil
	}
	return &spanWire{
		Source: span.Source.String(),
		Start:  toPositionWire(span.Start),
		End:    toPositionWire(span.End),
	}
}

// toPositionWire converts a Position to its JSON wire format.
func toPositionWire(pos location.Position) positionWire {
	wire := positionWire{
		Line:   pos.Line,
		Column: pos.Column,
	}
	if pos.IsValid() {
		offset := pos.Offset
		wire.Offset = &offset
	}
	return wire
}

// toRelatedInfoWire converts a RelatedInfo to its JSON wire format.
func toRelatedInfoWire(rel RelatedInfo) relatedInfoWire {
	wire := relatedInfoWire{
		Message: rel.Message,
	}
	if !rel.Span.IsZero() {
		wire.Span = toSpanWire(rel.Span)
	}
	return wire
}
