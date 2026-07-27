package cli

import (
	"fmt"
	"io"

	"golang.org/x/term"

	"github.com/simon-lentz/yammm/diag"
)

// OutputFormat controls how diagnostic results are rendered.
type OutputFormat string

const (
	FormatText OutputFormat = "text"
	FormatJSON OutputFormat = "json"
)

// ParseOutputFormat validates and returns the output format.
func ParseOutputFormat(s string) (OutputFormat, error) {
	switch s {
	case "text":
		return FormatText, nil
	case "json":
		return FormatJSON, nil
	default:
		return "", fmt.Errorf("invalid output format %q: must be \"text\" or \"json\"", s)
	}
}

// IsTTY reports whether the given file descriptor is a terminal.
func IsTTY(fd uintptr) bool {
	return term.IsTerminal(int(fd))
}

// NewRenderer creates a diag.Renderer configured for CLI output.
//
// When format is FormatJSON, colors and excerpts are disabled regardless
// of TTY state. The provider parameter may be nil.
func NewRenderer(format OutputFormat, isTTY bool, noColor bool, provider diag.SourceProvider, moduleRoot string) *diag.Renderer {
	colorize := isTTY && format == FormatText && !noColor
	excerpts := isTTY && format == FormatText

	opts := []diag.Option{
		diag.WithColors(colorize),
		diag.WithExcerpts(excerpts),
	}

	if provider != nil {
		opts = append(opts, diag.WithSourceProvider(provider))
	}
	if moduleRoot != "" {
		opts = append(opts, diag.WithModuleRoot(moduleRoot))
	}

	return diag.NewRenderer(opts...)
}

// RenderResult writes diagnostic output to w using the given renderer and format.
//
// Truncation at the collector's issue limit is surfaced in both formats even
// when no errors render: the text format appends a summary line, and the JSON
// format emits its wire object (which carries limit, limitReached, and
// droppedCount) rather than nothing — otherwise a machine consumer could not
// tell a truncated result from a clean one.
func RenderResult(w io.Writer, renderer *diag.Renderer, format OutputFormat, result diag.Result) error {
	switch format {
	case FormatJSON:
		// The JSON wire object carries limit/limitReached/droppedCount, so it is
		// the single truncation surface for JSON: emit it whenever there is
		// anything to report — any issue at any severity, or a truncation that an
		// issue-free result would otherwise render as nothing.
		if result.Len() == 0 && !result.LimitReached() {
			return nil
		}
		return writeResultJSON(w, renderer, result)
	default:
		// Every retained issue renders, not only failures: a Warning or Info is
		// reported precisely because the loader chose not to reject, so gating on
		// error severity would make it unreachable in every command.
		if result.Len() > 0 {
			if text := renderer.FormatResult(result); text != "" {
				if _, err := fmt.Fprintln(w, text); err != nil {
					return err
				}
			}
		}
		// Truncation is surfaced uniformly for text — after any errors render,
		// or alone when a clean result was truncated.
		if result.LimitReached() {
			return writeTruncationNote(w, result)
		}
		return nil
	}
}

// writeResultJSON writes the result's JSON wire object followed by a newline.
func writeResultJSON(w io.Writer, renderer *diag.Renderer, result diag.Result) error {
	data := renderer.FormatResultJSON(result)
	if _, err := w.Write(data); err != nil {
		return err
	}
	_, err := w.Write([]byte{'\n'})
	return err
}

// writeTruncationNote reports how many issues were dropped after the
// collector's limit was reached, using the canonical wording from
// [diag.Result.TruncationNote] so the CLI text surface and any future text
// consumer stay in sync (the JSON wire and the LSP log carry the same fact in
// their own shapes).
func writeTruncationNote(w io.Writer, result diag.Result) error {
	_, err := fmt.Fprintf(w, "note: %s\n", result.TruncationNote())
	return err
}
