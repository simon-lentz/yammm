package diag

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/location"
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

// LineIndexProvider is an optional extension for efficient LSP UTF-16
// offset computation.
//
// Registry-backed implementations (*SourceRegistry, *Sources) implement
// this interface. The Renderer checks if the SourceProvider also implements
// LineIndexProvider for optimized byte-to-character offset conversion.
type LineIndexProvider interface {
	// LineStartByte returns the byte offset of the start of the given line.
	// Lines are 1-based. Returns (0, false) if the source or line is unknown.
	LineStartByte(source location.SourceID, line int) (int, bool)
}

// LSPByteFallback controls behavior when byte offsets are unknown for LSP output.
type LSPByteFallback uint8

const (
	// LSPByteFallbackOmit omits diagnostics with unknown byte offsets from LSP output.
	// This is the default and ensures correctness.
	LSPByteFallbackOmit LSPByteFallback = iota

	// LSPByteFallbackApproximate uses Column-1 as the UTF-16 offset when byte
	// offset is unknown. This is correct for ASCII/BMP text but incorrect for
	// non-BMP characters (emoji, etc.).
	LSPByteFallbackApproximate
)

// rendererConfig holds renderer configuration.
type rendererConfig struct {
	provider            SourceProvider
	excerpts            bool
	maxCols             int
	moduleRoot          string
	colorize            bool
	distinguishFatal    bool
	truncationIndicator string
	lspByteFallback     LSPByteFallback
}

// RendererOption configures Renderer behavior.
type RendererOption func(*rendererConfig)

// WithSourceProvider sets the source content provider for excerpt rendering.
//
// If provider is nil, the Renderer omits source excerpts from output without
// error. This is safe and produces valid (albeit less informative) diagnostics.
func WithSourceProvider(p SourceProvider) RendererOption {
	return func(c *rendererConfig) {
		c.provider = p
	}
}

// WithExcerpts enables or disables source excerpts in output.
//
// Excerpts require a SourceProvider. If no provider is set, excerpts are
// silently omitted even if enabled.
func WithExcerpts(on bool) RendererOption {
	return func(c *rendererConfig) {
		c.excerpts = on
	}
}

// WithMaxLineColumns sets the maximum line length before truncation.
//
// Lines longer than this are truncated with the truncation indicator.
// Default is 120.
func WithMaxLineColumns(n int) RendererOption {
	return func(c *rendererConfig) {
		c.maxCols = n
	}
}

// WithModuleRoot sets the module root for path relativization.
//
// When set, absolute paths that start with this root are displayed
// relative to the root for cleaner output.
func WithModuleRoot(root string) RendererOption {
	return func(c *rendererConfig) {
		c.moduleRoot = root
	}
}

// WithColors enables or disables ANSI color output.
func WithColors(on bool) RendererOption {
	return func(c *rendererConfig) {
		c.colorize = on
	}
}

// WithDistinguishFatal controls whether Fatal is rendered as "fatal" or "error".
//
// In text output, Fatal severity is typically rendered as "error" for
// user-facing output. Set this to true to preserve the Fatal/Error distinction.
// JSON output always uses the canonical String() values.
func WithDistinguishFatal(distinguish bool) RendererOption {
	return func(c *rendererConfig) {
		c.distinguishFatal = distinguish
	}
}

// WithTruncationIndicator sets the indicator for truncated lines.
//
// Default is "...".
func WithTruncationIndicator(s string) RendererOption {
	return func(c *rendererConfig) {
		c.truncationIndicator = s
	}
}

// WithLSPByteFallback sets the behavior when byte offsets are unknown for LSP output.
func WithLSPByteFallback(mode LSPByteFallback) RendererOption {
	return func(c *rendererConfig) {
		c.lspByteFallback = mode
	}
}

// Renderer provides formatting for diagnostic output.
//
// Create with [NewRenderer] and configure with [RendererOption] functions.
type Renderer struct {
	provider            SourceProvider
	excerpts            bool
	maxCols             int
	moduleRoot          string
	colorize            bool
	distinguishFatal    bool
	truncationIndicator string
	lspByteFallback     LSPByteFallback
}

// NewRenderer creates a renderer with the given options.
func NewRenderer(opts ...RendererOption) *Renderer {
	cfg := &rendererConfig{
		maxCols:             120,
		truncationIndicator: "...",
	}

	for _, opt := range opts {
		opt(cfg)
	}

	return &Renderer{
		provider:            cfg.provider,
		excerpts:            cfg.excerpts,
		maxCols:             cfg.maxCols,
		moduleRoot:          cfg.moduleRoot,
		colorize:            cfg.colorize,
		distinguishFatal:    cfg.distinguishFatal,
		truncationIndicator: cfg.truncationIndicator,
		lspByteFallback:     cfg.lspByteFallback,
	}
}

// FormatIssue formats a single issue as text.
func (r *Renderer) FormatIssue(issue Issue) string {
	var sb strings.Builder
	r.formatIssueToBuilder(&sb, issue)
	return sb.String()
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

// FormatIssues formats a slice of issues as text.
func (r *Renderer) FormatIssues(issues []Issue) string {
	var sb strings.Builder
	for i, issue := range issues {
		if i > 0 {
			sb.WriteString("\n")
		}
		r.formatIssueToBuilder(&sb, issue)
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
	if r.excerpts && r.provider != nil && issue.HasSpan() {
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

	// Relativize path if module root is set.
	// Uses string manipulation rather than filepath.Rel because:
	// - SourceID.String() always returns forward-slash paths (CanonicalPath invariant)
	// - filepath.Rel would emit backslashes on Windows, breaking the invariant
	if root := strings.TrimSuffix(r.moduleRoot, "/"); root != "" {
		if source == root {
			source = "."
		} else if rel, ok := strings.CutPrefix(source, root+"/"); ok {
			source = rel
		}
	}

	if span.Start.IsKnown() {
		return fmt.Sprintf("%s:%d:%d", source, span.Start.Line, span.Start.Column)
	}
	return source
}

func (r *Renderer) writeSeverity(sb *strings.Builder, sev Severity) {
	label := sev.String()

	// Map Fatal to "error" unless distinguishFatal is set
	if sev == Fatal && !r.distinguishFatal {
		label = "error"
	}

	if r.colorize {
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

func (r *Renderer) writeExcerpt(sb *strings.Builder, issue Issue) {
	span := issue.Span()
	if !span.Start.IsKnown() {
		return
	}

	content, ok := r.provider.Content(span)
	if !ok {
		return
	}

	// Find the line containing the start of the span
	line := r.extractLine(content, span.Start.Line)
	if line == "" {
		return
	}

	// Truncate long lines
	displayLine := line
	if r.maxCols > 0 && utf8.RuneCountInString(line) > r.maxCols {
		runes := []rune(line)
		displayLine = string(runes[:r.maxCols]) + r.truncationIndicator
	}

	// Format the excerpt
	lineNum := strconv.Itoa(span.Start.Line)
	padding := strings.Repeat(" ", len(lineNum))

	sb.WriteString("\n   ")
	sb.WriteString(padding)
	sb.WriteString("|\n")

	sb.WriteString(lineNum)
	sb.WriteString(" | ")
	sb.WriteString(displayLine)
	sb.WriteString("\n")

	// Underline
	sb.WriteString("   ")
	sb.WriteString(padding)
	sb.WriteString("| ")

	// Calculate underline position (rune-based column)
	startCol := max(span.Start.Column, 1)

	// Calculate display width - use original line length for multi-line clamp,
	// but cap underline to truncated display width.
	lineRuneCount := utf8.RuneCountInString(line)
	displayRuneCount := utf8.RuneCountInString(displayLine)

	// Cap startCol to displayed width; if beyond display, skip underline
	if startCol > displayRuneCount {
		return
	}

	// Add spaces before the underline
	sb.WriteString(strings.Repeat(" ", startCol-1))

	// Calculate underline length, clamping to line boundaries
	endCol := span.End.Column
	if span.IsPoint() || endCol <= startCol {
		endCol = startCol + 1
	}

	// Clamp endCol to line length for multi-line spans
	if endCol > lineRuneCount+1 {
		endCol = lineRuneCount + 1
	}

	// Clamp endCol to displayed width after truncation
	if endCol > displayRuneCount+1 {
		endCol = displayRuneCount + 1
	}

	underlineLen := max(endCol-startCol, 1)
	sb.WriteString(strings.Repeat("^", underlineLen))
}

// extractLine extracts the nth line (1-based) from content.
func (r *Renderer) extractLine(content []byte, lineNum int) string {
	if lineNum < 1 {
		return ""
	}

	currentLine := 1
	start := 0

	for i := 0; i < len(content); i++ {
		if currentLine == lineNum {
			// Found the start of the target line
			end := i
			for end < len(content) && content[end] != '\n' && content[end] != '\r' {
				end++
			}
			return string(content[i:end])
		}
		switch content[i] {
		case '\n':
			currentLine++
			start = i + 1
		case '\r':
			currentLine++
			if i+1 < len(content) && content[i+1] == '\n' {
				i++ // Skip \n after \r
			}
			start = i + 1
		}
	}

	// Handle last line without newline
	if currentLine == lineNum && start < len(content) {
		return string(content[start:])
	}

	return ""
}
