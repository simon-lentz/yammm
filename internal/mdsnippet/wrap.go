package mdsnippet

import "strings"

// probeKey is the primary key injected into a synthesized type shell. Every
// concrete type needs one and a bare property fragment has no way to carry it,
// so without this the shell — not the example — is what fails to load.
const probeKey = "probe_id UUID primary"

// WrapForParsing wraps the block's content in the minimal schema context the
// parser needs, whose root production is the schema header.
//
// Content that already declares a schema is returned untouched. Content that
// starts with a top-level keyword gets a schema declaration. Everything else —
// property lines, relation lines, invariants — additionally gets a type shell.
func (b Block) WrapForParsing() string { return b.wrap(false) }

// WrapForLoading wraps as [Block.WrapForParsing] does, and additionally gives a
// synthesized type shell a primary key.
//
// The key is injected ONLY into a shell this package synthesized; a block that
// brings its own type declarations is untouched and keeps whatever keys it
// declares.
func (b Block) WrapForLoading() string { return b.wrap(true) }

func (b Block) wrap(withKey bool) string {
	content := b.Content
	if strings.TrimSpace(content) == "" {
		return content
	}
	if firstNonCommentToken(strings.TrimSpace(content)) == "schema" {
		return content
	}

	// Import declarations belong at schema level, but a fragment may pair one
	// with a member line that belongs inside a type ("import ./products as
	// products" followed by "--> PRODUCT (one) products.Product"). That is
	// legal prose spanning two scopes, so the two are separated before
	// wrapping rather than being forced into one.
	imports, body := hoistImports(content)

	var b2 strings.Builder
	b2.WriteString("schema \"Test\"\n")
	for _, imp := range imports {
		b2.WriteString(imp)
		b2.WriteByte('\n')
	}
	if isTopLevelKeyword(firstNonCommentToken(strings.TrimSpace(body))) {
		b2.WriteString(body)
		return b2.String()
	}
	b2.WriteString("type W {\n")
	if withKey {
		b2.WriteString(probeKey)
		b2.WriteByte('\n')
	}
	b2.WriteString(body)
	b2.WriteString("}\n")
	return b2.String()
}

// hoistImports splits leading import declarations out of a fragment, returning
// them and the remaining content.
func hoistImports(content string) (imports []string, body string) {
	var rest []string
	for line := range strings.SplitSeq(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "import ") {
			imports = append(imports, line)
			continue
		}
		rest = append(rest, line)
	}
	return imports, strings.Join(rest, "\n")
}

// firstNonCommentToken returns the first whitespace-delimited token in s after
// skipping any leading block comments and line comments.
func firstNonCommentToken(s string) string {
	for {
		s = strings.TrimSpace(s)
		if after, ok := strings.CutPrefix(s, "/*"); ok {
			_, rest, closed := strings.Cut(after, "*/")
			if !closed {
				return ""
			}
			s = rest
			continue
		}
		if after, ok := strings.CutPrefix(s, "//"); ok {
			_, rest, hasNewline := strings.Cut(after, "\n")
			if !hasNewline {
				return ""
			}
			s = rest
			continue
		}
		break
	}
	token, _, _ := strings.Cut(strings.TrimSpace(s), " ")
	return token
}

// isTopLevelKeyword reports whether token introduces a top-level declaration.
func isTopLevelKeyword(token string) bool {
	switch token {
	case "type", "abstract", "part", "import":
		return true
	}
	return false
}

// Indent indents every line of s by four spaces, for embedding a wrapped
// example in a failure message.
func Indent(s string) string {
	var b strings.Builder
	for line := range strings.SplitSeq(strings.TrimRight(s, "\n"), "\n") {
		b.WriteString("    ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}
