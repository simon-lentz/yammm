package diag

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/width"

	"github.com/simon-lentz/yammm/location"
)

// An excerpt shows at most excerptCols runes of its source line. A longer line
// is cut to a window that holds the span's start, and each cut end is marked
// with excerptEllipsis.
const (
	excerptCols     = 120
	excerptEllipsis = "..."
)

// SourceProvider provides source content for excerpt rendering.
//
// Implementations should return the content of the source file containing
// the span, if available. Return (nil, false) if the content is not available.
type SourceProvider interface {
	// Content returns the source content for the given span.
	// Returns (nil, false) if the content is not available.
	Content(span location.Span) ([]byte, bool)
}

// rendererConfig holds renderer configuration.
type rendererConfig struct {
	provider         SourceProvider
	excerpts         bool
	moduleRoot       string
	colorize         bool
	distinguishFatal bool
}

// Option configures Renderer behavior.
type Option func(*rendererConfig)

// WithSourceProvider sets the source content provider for excerpt rendering.
//
// If provider is nil, the Renderer omits source excerpts from output without
// error. This is safe and produces valid (albeit less informative) diagnostics.
func WithSourceProvider(p SourceProvider) Option {
	return func(c *rendererConfig) {
		c.provider = p
	}
}

// WithExcerpts enables or disables source excerpts in output.
//
// Excerpts require a SourceProvider. If no provider is set, excerpts are
// silently omitted even if enabled.
func WithExcerpts(on bool) Option {
	return func(c *rendererConfig) {
		c.excerpts = on
	}
}

// WithModuleRoot sets the root that text locations are written relative to.
// root is a host path; the renderer turns it into an identity once, by the
// rule [location.NewCanonicalPath] follows, and writes a file-backed source
// under it relative to it ([location.SourceID.RelativeTo]). JSON output keeps
// each source's identity.
func WithModuleRoot(root string) Option {
	return func(c *rendererConfig) {
		c.moduleRoot = root
	}
}

// WithColors enables or disables ANSI color output.
func WithColors(on bool) Option {
	return func(c *rendererConfig) {
		c.colorize = on
	}
}

// WithDistinguishFatal controls whether Fatal is rendered as "fatal" or "error".
//
// In text output, Fatal severity is typically rendered as "error" for
// user-facing output. Set this to true to preserve the Fatal/Error distinction.
// JSON output always uses the canonical String() values.
func WithDistinguishFatal(distinguish bool) Option {
	return func(c *rendererConfig) {
		c.distinguishFatal = distinguish
	}
}

// Renderer provides formatting for diagnostic output.
//
// Create with [NewRenderer] and configure with [Option] functions.
type Renderer struct {
	cfg  rendererConfig
	root location.CanonicalPath // the module root as an identity; zero when unset or unresolvable
}

// NewRenderer creates a renderer with the given options.
func NewRenderer(opts ...Option) *Renderer {
	var cfg rendererConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	r := &Renderer{cfg: cfg}
	if cfg.moduleRoot != "" {
		if root, err := location.NewCanonicalPath(cfg.moduleRoot); err == nil {
			r.root = root
		}
	}
	return r
}

// FormatResult formats all issues in a result as text.
func (r *Renderer) FormatResult(res Result) string {
	var sb strings.Builder
	first := true
	for issue := range res.Issues() {
		if !first {
			sb.WriteString("\n")
		}
		r.formatIssueToBuilder(&sb, issue)
		first = false
	}
	return sb.String()
}

func (r *Renderer) formatIssueToBuilder(sb *strings.Builder, issue Issue) {
	// Location prefix
	r.writeLocation(sb, issue)

	// Severity and code
	sb.WriteString(": ")
	r.writeSeverity(sb, issue.Severity())
	sb.WriteString("[")
	sb.WriteString(issue.Code().String())
	sb.WriteString("]: ")

	// Message
	sb.WriteString(issue.Message())

	// Hint
	if hint := issue.Hint(); hint != "" {
		sb.WriteString("\n  hint: ")
		sb.WriteString(hint)
	}

	// Source excerpt
	if r.cfg.excerpts && r.cfg.provider != nil && issue.HasSpan() {
		r.writeExcerpt(sb, issue)
	}

	// Related info
	for _, rel := range issue.Related() {
		sb.WriteString("\n  note: ")
		sb.WriteString(rel.Message)
		if !rel.Span.IsZero() {
			sb.WriteString("\n    --> ")
			sb.WriteString(r.formatSpanLocation(rel.Span))
		}
	}
}

func (r *Renderer) writeLocation(sb *strings.Builder, issue Issue) {
	switch {
	case issue.HasSpan():
		sb.WriteString(r.formatSpanLocation(issue.Span()))
	case issue.Path() != "":
		if issue.SourceName() != "" {
			sb.WriteString(issue.SourceName())
			sb.WriteString(" ")
		}
		sb.WriteString(issue.Path())
	case issue.SourceName() != "":
		// File-level provenance without specific path
		sb.WriteString(issue.SourceName())
	default:
		sb.WriteString("<unknown>")
	}
}

func (r *Renderer) formatSpanLocation(span location.Span) string {
	source := span.Source.String()
	if rel, ok := span.Source.RelativeTo(r.root); ok {
		source = rel
	}
	if span.Start.IsKnown() {
		return fmt.Sprintf("%s:%d:%d", source, span.Start.Line, span.Start.Column)
	}
	return source
}

func (r *Renderer) writeSeverity(sb *strings.Builder, sev Severity) {
	label := sev.String()

	// Map Fatal to "error" unless distinguishFatal is set
	if sev == Fatal && !r.cfg.distinguishFatal {
		label = "error"
	}

	if r.cfg.colorize {
		//exhaustive:enforce
		switch sev {
		case Fatal, Error:
			sb.WriteString("\033[1;31m") // Bold red
			sb.WriteString(label)
			sb.WriteString("\033[0m")
		case Warning:
			sb.WriteString("\033[1;33m") // Bold yellow
			sb.WriteString(label)
			sb.WriteString("\033[0m")
		case Info:
			sb.WriteString("\033[1;36m") // Bold cyan
			sb.WriteString(label)
			sb.WriteString("\033[0m")
		case Hint:
			sb.WriteString("\033[1;32m") // Bold green
			sb.WriteString(label)
			sb.WriteString("\033[0m")
		default:
			sb.WriteString(label)
		}
	} else {
		sb.WriteString(label)
	}
}

// writeExcerpt writes the span's first line under a gutter and marks the span
// under it, in terminal columns (see [runeCols] and [blankUnder]). A span that
// runs onto later lines is marked to the end of its first line.
func (r *Renderer) writeExcerpt(sb *strings.Builder, issue Issue) {
	span := issue.Span()
	if !span.Start.IsKnown() {
		return
	}
	content, ok := r.cfg.provider.Content(span)
	if !ok {
		return
	}
	text, ok := extractLine(content, span.Start.Line)
	if !ok {
		return
	}
	line := []rune(text)

	// Rune indexes. start may equal len(line): the column one past the last
	// rune is where an end-of-line diagnostic points.
	start := span.Start.Column - 1
	if start > len(line) {
		return
	}
	end := start + 1
	switch {
	case span.End.Line > span.Start.Line:
		end = len(line)
	case span.End.Line == span.Start.Line && span.End.Column-1 > start:
		end = span.End.Column - 1
	}

	lo, hi := 0, len(line)
	if len(line) > excerptCols {
		lo = min(max(0, start-excerptCols/4), len(line)-excerptCols)
		hi = lo + excerptCols
	}

	var shown, marks strings.Builder
	if lo > 0 {
		shown.WriteString(excerptEllipsis)
		marks.WriteString(strings.Repeat(" ", len(excerptEllipsis)))
	}
	shown.WriteString(string(line[lo:hi]))
	if hi < len(line) {
		shown.WriteString(excerptEllipsis)
	}
	for _, c := range line[lo:start] {
		marks.WriteString(blankUnder(c))
	}
	carets := 0
	for _, c := range line[start:max(start, min(end, hi))] {
		carets += runeCols(c)
	}
	marks.WriteString(strings.Repeat("^", max(carets, 1)))

	num := strconv.Itoa(span.Start.Line)
	gutter := strings.Repeat(" ", len(num))
	sb.WriteString("\n" + gutter + " |\n")
	sb.WriteString(num + " | " + shown.String() + "\n")
	sb.WriteString(gutter + " | " + marks.String())
}

// blankUnder returns what sits under c in the mark row: the tab itself, so a
// terminal expands both alike, or one space per column c takes.
func blankUnder(c rune) string {
	if c == '\t' {
		return "\t"
	}
	return strings.Repeat(" ", runeCols(c))
}

// runeCols returns the terminal columns c takes: none for a combining mark,
// two for an East Asian wide or fullwidth rune, one otherwise.
func runeCols(c rune) int {
	if unicode.In(c, unicode.Mn, unicode.Me) {
		return 0
	}
	switch width.LookupRune(c).Kind() {
	case width.EastAsianWide, width.EastAsianFullwidth:
		return 2
	default:
		return 1
	}
}

// extractLine returns the nth line (1-based) of content without its line
// ending, and whether content has that line. "\n", "\r\n" and "\r" each end a
// line. The empty line after a final line ending exists, so a position at the
// end of input has a line to show.
func extractLine(content []byte, n int) (string, bool) {
	if n < 1 {
		return "", false
	}
	line, start := 1, 0
	for i := 0; i < len(content); i++ {
		c := content[i]
		if c != '\n' && c != '\r' {
			continue
		}
		if line == n {
			return string(content[start:i]), true
		}
		if c == '\r' && i+1 < len(content) && content[i+1] == '\n' {
			i++
		}
		line++
		start = i + 1
	}
	if line == n {
		return string(content[start:]), true
	}
	return "", false
}
