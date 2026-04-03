package format

import "strings"

// memberKind identifies the type of an alignable declaration member.
type memberKind int

const (
	memberProperty     memberKind = iota // field_name Type [modifier]
	memberRelationship                   // --> or *-> REL_NAME [(mult)] Target
	memberAlias                          // type Name = TypeExpr
)

// alignableLine holds the parsed structure of a single-line declaration for alignment.
type alignableLine struct {
	indent  string // leading whitespace (preserved as-is)
	kind    memberKind
	arrow   string // "-->" or "*->" for relationships, empty otherwise
	name    string // the column to be padded
	rest    string // everything after name
	comment string // inline // comment (includes "//"), empty if none
	raw     string // original line text
}

// AlignColumns pads the name column within alignment groups to produce
// columnar output. Groups are contiguous runs of the same member kind,
// broken by blank lines, comment-only lines, non-alignable lines, or kind changes.
func AlignColumns(text string) string {
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	result := make([]string, 0, len(lines))
	var group []alignableLine

	i := 0
	for i < len(lines) {
		line := lines[i]

		if isMultilineStart(line) {
			result = flushAlignGroup(result, group)
			group = nil
			result, i = emitMultilineConstruct(result, lines, i)
			continue
		}

		parsed, ok := parseAlignableLine(line)
		if !ok {
			result = flushAlignGroup(result, group)
			group = nil
			result = append(result, line)
			i++
			continue
		}

		if len(group) > 0 && group[0].kind != parsed.kind {
			result = flushAlignGroup(result, group)
			group = nil
		}

		group = append(group, parsed)
		i++
	}
	result = flushAlignGroup(result, group)
	return strings.Join(result, "\n")
}

// parseAlignableLine classifies a line and extracts the alignable name column.
func parseAlignableLine(line string) (alignableLine, bool) {
	trimmed := strings.TrimLeft(line, "\t ")
	indent := line[:len(line)-len(trimmed)]

	if trimmed == "" {
		return alignableLine{}, false
	}

	// Split off inline comment (bracket/quote-aware).
	content := trimmed
	comment := ""
	if idx := findInlineComment(trimmed); idx >= 0 {
		content = strings.TrimRight(trimmed[:idx], " ")
		comment = trimmed[idx:]
	}

	// Relationship: starts with --> or *->
	if strings.HasPrefix(content, "-->") || strings.HasPrefix(content, "*->") {
		arrow := content[:3]
		afterArrow := content[3:]
		if len(afterArrow) == 0 || afterArrow[0] != ' ' {
			return alignableLine{}, false
		}
		afterArrow = afterArrow[1:] // skip space
		spaceIdx := strings.IndexByte(afterArrow, ' ')
		if spaceIdx < 0 {
			return alignableLine{
				indent: indent, kind: memberRelationship,
				arrow: arrow, name: afterArrow, rest: "",
				comment: comment, raw: line,
			}, true
		}
		restStart := spaceIdx + 1
		for restStart < len(afterArrow) && afterArrow[restStart] == ' ' {
			restStart++
		}
		return alignableLine{
			indent: indent, kind: memberRelationship,
			arrow: arrow, name: afterArrow[:spaceIdx], rest: afterArrow[restStart:],
			comment: comment, raw: line,
		}, true
	}

	// Alias: starts with "type " and contains " = " without "{"
	if strings.HasPrefix(content, "type ") && strings.Contains(content, " = ") && !strings.Contains(content, "{") {
		afterType := content[5:] // skip "type "
		spaceIdx := strings.IndexByte(afterType, ' ')
		if spaceIdx < 0 {
			return alignableLine{}, false
		}
		restStart := spaceIdx + 1
		for restStart < len(afterType) && afterType[restStart] == ' ' {
			restStart++
		}
		return alignableLine{
			indent: indent, kind: memberAlias,
			name: afterType[:spaceIdx], rest: afterType[restStart:],
			comment: comment, raw: line,
		}, true
	}

	// Property: first word is lowercase identifier, second word starts with uppercase.
	if len(content) > 0 && (content[0] >= 'a' && content[0] <= 'z' || content[0] == '_') {
		spaceIdx := strings.IndexByte(content, ' ')
		if spaceIdx < 0 {
			return alignableLine{}, false
		}
		firstWord := content[:spaceIdx]
		switch firstWord {
		case "schema", "import", "type", "abstract", "part", "extends":
			return alignableLine{}, false
		}
		restStart := spaceIdx + 1
		for restStart < len(content) && content[restStart] == ' ' {
			restStart++
		}
		rest := content[restStart:]
		if len(rest) > 0 && rest[0] >= 'A' && rest[0] <= 'Z' {
			return alignableLine{
				indent: indent, kind: memberProperty,
				name: firstWord, rest: rest,
				comment: comment, raw: line,
			}, true
		}
	}

	return alignableLine{}, false
}

// findInlineComment returns the byte index of "//" outside brackets and quotes, or -1.
func findInlineComment(s string) int {
	bracketDepth := 0
	sc := newQuoteAwareScanner(s)
	for i, ch := sc.next(); i >= 0; i, ch = sc.next() {
		if ch == '[' {
			bracketDepth++
			continue
		}
		if ch == ']' {
			bracketDepth--
			continue
		}
		if ch == '/' && i+1 < len(s) && s[i+1] == '/' && bracketDepth == 0 {
			return i
		}
	}
	return -1
}

// flushAlignGroup pads names to a common width and rebuilds each line.
// Groups of 0 or 1 members pass through unchanged.
func flushAlignGroup(result []string, group []alignableLine) []string {
	if len(group) <= 1 {
		for _, al := range group {
			result = append(result, al.raw)
		}
		return result
	}

	maxNameWidth := 0
	for _, al := range group {
		if len(al.name) > maxNameWidth {
			maxNameWidth = len(al.name)
		}
	}

	type rebuiltLine struct {
		content string
		comment string
	}
	rebuilt := make([]rebuiltLine, len(group))
	hasComments := false
	maxContentWidth := 0

	for i, al := range group {
		var b strings.Builder
		b.WriteString(al.indent)

		switch al.kind {
		case memberProperty:
			b.WriteString(al.name)
			b.WriteString(strings.Repeat(" ", maxNameWidth-len(al.name)))
			b.WriteByte(' ')
			b.WriteString(al.rest)

		case memberRelationship:
			b.WriteString(al.arrow)
			b.WriteByte(' ')
			b.WriteString(al.name)
			b.WriteString(strings.Repeat(" ", maxNameWidth-len(al.name)))
			b.WriteByte(' ')
			b.WriteString(al.rest)

		case memberAlias:
			b.WriteString("type ")
			b.WriteString(al.name)
			b.WriteString(strings.Repeat(" ", maxNameWidth-len(al.name)))
			b.WriteByte(' ')
			b.WriteString(al.rest)
		}

		content := b.String()
		rebuilt[i] = rebuiltLine{content: content, comment: al.comment}
		if al.comment != "" {
			hasComments = true
		}
		if len(content) > maxContentWidth {
			maxContentWidth = len(content)
		}
	}

	for _, rl := range rebuilt {
		if hasComments && rl.comment != "" {
			padding := max(maxContentWidth-len(rl.content)+1, 1)
			result = append(result, rl.content+strings.Repeat(" ", padding)+rl.comment)
		} else {
			result = append(result, rl.content)
		}
	}

	return result
}

// isMultilineStart returns true if the line has unbalanced [ brackets.
func isMultilineStart(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") {
		return false
	}

	depth := 0
	sc := newQuoteAwareScanner(trimmed)
	for i, ch := sc.next(); i >= 0; i, ch = sc.next() {
		if ch == '[' {
			depth++
		}
		if ch == ']' {
			depth--
		}
	}
	return depth > 0
}

// emitMultilineConstruct emits lines until bracket depth returns to zero.
func emitMultilineConstruct(result, lines []string, startIdx int) ([]string, int) {
	depth := 0
	i := startIdx
	for i < len(lines) {
		result = append(result, lines[i])
		sc := newQuoteAwareScanner(lines[i])
		for j, ch := sc.next(); j >= 0; j, ch = sc.next() {
			if ch == '[' {
				depth++
			}
			if ch == ']' {
				depth--
			}
		}
		i++
		if depth <= 0 {
			break
		}
	}
	return result, i
}
