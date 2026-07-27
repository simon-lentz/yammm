package mdsnippet

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Fence tags. Each names a genre of example and selects the gate that applies
// to it; see the package documentation for what each one claims.
const (
	// TagYammm marks illustrative but real code that must parse.
	TagYammm = "yammm"
	// TagSnippet marks a fragment of illustrative but real code that must parse.
	TagSnippet = "yammm-snippet"
	// TagSchema marks a complete model that must parse and load clean.
	TagSchema = "yammm-schema"
	// TagInvalid marks a deliberately wrong example that must fail to load.
	TagInvalid = "yammm-invalid"
)

// extractedTags is the set of fence tags this package collects. A tag outside
// it (text, go, bash) is prose or another language and is deliberately ignored.
var extractedTags = map[string]struct{}{
	TagYammm:   {},
	TagSnippet: {},
	TagSchema:  {},
	TagInvalid: {},
}

// Block is one fenced code block extracted from a Markdown document.
type Block struct {
	// Tag is the fence's language tag, one of the Tag* constants.
	Tag string
	// Content is the block's body, without the fences.
	Content string
	// File is the path the block was read from, as supplied to Extract.
	File string
	// Line is the 1-based line number of the OPENING fence, so a failure
	// message points at the fence a reader can search for rather than at an
	// offset into the extracted content.
	Line int
}

// Ref returns a "file:line (```tag)" reference for failure messages.
func (b Block) Ref() string {
	return fmt.Sprintf("%s:%d (```%s)", b.File, b.Line, b.Tag)
}

// Extract returns the fenced yammm blocks in source, tagged with file for
// reporting. It scans lines rather than parsing Markdown, which is sufficient
// because none of the repo's documents nest fences.
func Extract(file, source string) []Block {
	lines := strings.Split(source, "\n")
	var blocks []Block
	var current *Block
	var buf strings.Builder

	for i, line := range lines {
		if current != nil {
			if strings.TrimSpace(line) == "```" {
				current.Content = buf.String()
				blocks = append(blocks, *current)
				current = nil
				buf.Reset()
				continue
			}
			buf.WriteString(line)
			buf.WriteByte('\n')
			continue
		}
		tag, ok := strings.CutPrefix(strings.TrimSpace(line), "```")
		if !ok {
			continue
		}
		if _, extracted := extractedTags[tag]; !extracted {
			continue
		}
		current = &Block{Tag: tag, File: file, Line: i + 1}
	}
	return blocks
}

// ExtractFiles reads each path and returns every yammm block across all of
// them, in path order then document order.
func ExtractFiles(paths ...string) ([]Block, error) {
	var blocks []Block
	for _, p := range paths {
		source, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", p, err)
		}
		blocks = append(blocks, Extract(p, string(source))...)
	}
	return blocks, nil
}

// diagnosticCode matches a diagnostic code named in an example's own comment,
// e.g. "// E_INVALID_ANNOTATION". Anchored to a comment so a code mentioned in
// surrounding prose, or a property that merely starts with E_, cannot be
// mistaken for an expectation.
var diagnosticCode = regexp.MustCompile(`//[^\n]*\b([EW]_[A-Z0-9_]+)\b`)

// DiagnosticCode returns the diagnostic code this block's own comment claims it
// produces, and whether one was named.
func (b Block) DiagnosticCode() (string, bool) {
	m := diagnosticCode.FindStringSubmatch(b.Content)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// SkipReason returns a non-empty reason when the block cannot be meaningfully
// gated as yammm source.
func (b Block) SkipReason() string {
	if strings.Count(b.Content, "schema ") > 1 {
		return "multi-file example"
	}
	// Bare expression fragments (e.g. `$self.name`) are not declarations; they
	// illustrate expression syntax only.
	if strings.HasPrefix(firstNonCommentToken(strings.TrimSpace(b.Content)), "$") {
		return "expression fragment"
	}
	return ""
}
