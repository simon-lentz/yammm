package docs_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/antlr4-go/antlr/v4"

	"github.com/simon-lentz/yammm/internal/grammar"
)

// codeBlock represents a fenced code block extracted from Markdown.
type codeBlock struct {
	tag     string // "yammm" or "yammm-snippet"
	content string
	line    int // 1-based line number of the opening fence in SPEC.md
}

// extractCodeBlocks extracts all ```yammm and ```yammm-snippet fenced code
// blocks from Markdown source. It uses a simple line scanner rather than a
// full Markdown parser — sufficient because the spec never nests fences.
func extractCodeBlocks(source string) []codeBlock {
	lines := strings.Split(source, "\n")
	var blocks []codeBlock
	var current *codeBlock
	var buf strings.Builder

	for i, line := range lines {
		if current != nil {
			if strings.TrimSpace(line) == "```" {
				current.content = buf.String()
				blocks = append(blocks, *current)
				current = nil
				buf.Reset()
				continue
			}
			buf.WriteString(line)
			buf.WriteByte('\n')
			continue
		}
		switch strings.TrimSpace(line) {
		case "```yammm":
			current = &codeBlock{tag: "yammm", line: i + 1}
		case "```yammm-snippet":
			current = &codeBlock{tag: "yammm-snippet", line: i + 1}
		}
	}
	return blocks
}

// wrapForParsing wraps a code block's content in a minimal schema context
// so the ANTLR parser (whose root rule is `schema`) can parse it.
//
// The wrapping strategy:
//   - Content that already starts with `schema` → no wrapping needed.
//   - Content that starts with a top-level declaration keyword
//     (type, abstract, part, import) → prefix with schema declaration.
//   - Everything else (property lines, relation lines, invariants) →
//     wrap in a schema + type shell.
//
// Multi-file examples (containing more than one `schema "..."` declaration)
// cannot be parsed as a single schema and are skipped by the caller.
func wrapForParsing(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return content
	}

	// Peek past leading comments to find the first real token.
	first := firstNonCommentToken(trimmed)

	switch {
	case first == "schema":
		return content
	case isTopLevelKeyword(first):
		return "schema \"Test\"\n" + content
	default:
		// Property, relation, invariant, or data-type-alias lines.
		return "schema \"Test\"\ntype W {\n" + content + "}\n"
	}
}

// firstNonCommentToken returns the first whitespace-delimited token in s
// after skipping any leading block comments (/* ... */) and line comments (//).
func firstNonCommentToken(s string) string {
	for {
		s = strings.TrimSpace(s)
		if strings.HasPrefix(s, "/*") {
			end := strings.Index(s, "*/")
			if end < 0 {
				return ""
			}
			s = s[end+2:]
			continue
		}
		if strings.HasPrefix(s, "//") {
			nl := strings.IndexByte(s, '\n')
			if nl < 0 {
				return ""
			}
			s = s[nl+1:]
			continue
		}
		break
	}
	token, _, _ := strings.Cut(strings.TrimSpace(s), " ")
	return token
}

// isTopLevelKeyword returns true if the token introduces a top-level
// declaration (type, datatype alias, or import) within a schema body.
func isTopLevelKeyword(token string) bool {
	switch token {
	case "type", "abstract", "part", "import":
		return true
	}
	return false
}

// shouldSkip returns a non-empty reason string if the block cannot be
// meaningfully parsed as YAMMM syntax. Reasons include multi-file examples
// and expression-only fragments.
func shouldSkip(content string) string {
	if strings.Count(content, "schema ") > 1 {
		return "multi-file example"
	}
	// Bare expression fragments (e.g., `$self.name`, `$item.price`) are not
	// valid declarations; they illustrate expression syntax only.
	first := firstNonCommentToken(strings.TrimSpace(content))
	if strings.HasPrefix(first, "$") {
		return "expression fragment"
	}
	return ""
}

// parseErrors runs the ANTLR lexer+parser on source and returns any syntax
// errors. An empty slice means the source parsed successfully.
func parseErrors(source string) []string {
	input := antlr.NewInputStream(source)
	lexer := grammar.NewYammmGrammarLexer(input)
	stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	parser := grammar.NewYammmGrammarParser(stream)

	el := &collectingErrorListener{}
	parser.RemoveErrorListeners()
	parser.AddErrorListener(el)

	parser.Schema() // root rule

	return el.errors
}

// collectingErrorListener collects ANTLR syntax errors as strings.
type collectingErrorListener struct {
	antlr.DefaultErrorListener
	errors []string
}

func (l *collectingErrorListener) SyntaxError(
	_ antlr.Recognizer,
	_ any,
	line, column int,
	msg string,
	_ antlr.RecognitionException,
) {
	l.errors = append(l.errors, fmt.Sprintf("line %d:%d %s", line, column, msg))
}

func TestSpecExamples(t *testing.T) {
	source, err := os.ReadFile("SPEC.md")
	if err != nil {
		t.Fatalf("reading SPEC.md: %v", err)
	}

	blocks := extractCodeBlocks(string(source))
	if len(blocks) == 0 {
		t.Fatal("no yammm code blocks found in SPEC.md")
	}

	t.Logf("found %d code blocks", len(blocks))

	var tested, skipped int
	for _, b := range blocks {
		if reason := shouldSkip(b.content); reason != "" {
			skipped++
			continue
		}

		wrapped := wrapForParsing(b.content)
		errs := parseErrors(wrapped)
		if len(errs) > 0 {
			t.Errorf("SPEC.md:%d (```%s) failed to parse:\n  source:\n%s\n  errors:\n    %s",
				b.line, b.tag, indent(wrapped), strings.Join(errs, "\n    "))
		}
		tested++
	}

	t.Logf("tested %d blocks, skipped %d multi-file examples", tested, skipped)
}

func indent(s string) string {
	var b strings.Builder
	for line := range strings.SplitSeq(strings.TrimRight(s, "\n"), "\n") {
		b.WriteString("    ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}
