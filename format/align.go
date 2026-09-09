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
// broken by blank lines, comment lines, non-alignable lines, or kind changes.
func AlignColumns(text string) string {
	if text == "" {
		return ""
	}
	return joinLines(alignColumns(classifyLexed(text)))
}

// alignColumns is AlignColumns over classified lines. Only a content line
// can join a group; a blank or comment line ends the group and passes
// through untouched, whatever its text looks like.
func alignColumns(ls []line) []line {
	result := make([]line, 0, len(ls))
	var group []alignableLine

	i := 0
	for i < len(ls) {
		ln := ls[i]

		if ln.class != lineContent {
			result = flushAlignGroup(result, group)
			group = nil
			result = append(result, ln)
			i++
			continue
		}

		if isMultilineStart(ln) {
			result = flushAlignGroup(result, group)
			group = nil
			result, i = emitMultilineConstruct(result, ls, i)
			continue
		}

		parsed, ok := parseAlignableLine(ln)
		if !ok {
			result = flushAlignGroup(result, group)
			group = nil
			result = append(result, ln)
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
	return flushAlignGroup(result, group)
}

// parseAlignableLine extracts the alignable name column, reading the trailing
// comment's position from the record rather than searching the text for "//".
func parseAlignableLine(ln line) (alignableLine, bool) {
	raw := ln.text
	trimmed := strings.TrimLeft(raw, "\t ")
	indent := raw[:len(raw)-len(trimmed)]

	if trimmed == "" {
		return alignableLine{}, false
	}

	content := trimmed
	comment := ""
	if ln.hasComment() {
		idx := ln.lex.commentAt - len(indent)
		if idx >= 0 && idx <= len(trimmed) {
			content = strings.TrimRight(trimmed[:idx], " ")
			comment = trimmed[idx:]
		}
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
				comment: comment, raw: raw,
			}, true
		}
		restStart := spaceIdx + 1
		for restStart < len(afterArrow) && afterArrow[restStart] == ' ' {
			restStart++
		}
		return alignableLine{
			indent: indent, kind: memberRelationship,
			arrow: arrow, name: afterArrow[:spaceIdx], rest: afterArrow[restStart:],
			comment: comment, raw: raw,
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
			comment: comment, raw: raw,
		}, true
	}

	// Property: first word is lowercase identifier, second word starts with uppercase.
	if len(content) > 0 && (content[0] >= 'a' && content[0] <= 'z' || content[0] == '_') {
		spaceIdx := strings.IndexByte(content, ' ')
		if spaceIdx < 0 {
			return alignableLine{}, false
		}
		// Deny by declaration SHAPE, not by first word. type, schema, extends
		// and abstract are all legal property names, and denying the words
		// split an aligned group into unpadded singletons; only as and part are
		// refused in a type body. A declaration is told apart by what follows:
		// schema and import take a string literal, and a type header ends in a
		// brace, neither of which a property does.
		firstWord := content[:spaceIdx]
		switch firstWord {
		case "as", "part":
			return alignableLine{}, false
		}
		if ln.declaresWith("schema") || ln.declaresWith("import") {
			return alignableLine{}, false
		}
		if strings.HasSuffix(ln.trimmedCode(), "{") {
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
				comment: comment, raw: raw,
			}, true
		}
	}

	return alignableLine{}, false
}

// flushAlignGroup pads names to a common width and rebuilds each line.
// Groups of 0 or 1 members pass through unchanged.
func flushAlignGroup(result []line, group []alignableLine) []line {
	if len(group) <= 1 {
		for _, al := range group {
			result = append(result, contentLine(al.raw))
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
			result = append(result, contentLine(rl.content+strings.Repeat(" ", padding)+rl.comment))
		} else {
			result = append(result, contentLine(rl.content))
		}
	}

	return result
}

// isMultilineStart returns true if a content line has unbalanced [ brackets.
func isMultilineStart(ln line) bool {
	return bracketDelta(ln) > 0
}

// emitMultilineConstruct emits lines until bracket depth returns to zero.
// Only content lines move the depth; a comment inside the construct is
// emitted and not counted.
func emitMultilineConstruct(result, ls []line, startIdx int) ([]line, int) {
	depth := 0
	i := startIdx
	for i < len(ls) {
		result = append(result, ls[i])
		if ls[i].class == lineContent {
			depth += bracketDelta(ls[i])
		}
		i++
		if depth <= 0 {
			break
		}
	}
	return result, i
}

// bracketDelta returns the number of [ minus the number of ] in the line's
// code. Brackets inside a literal or a comment are not code and do not count.
func bracketDelta(ln line) int {
	depth := 0
	for _, ch := range []byte(ln.mask()) {
		switch ch {
		case '[':
			depth++
		case ']':
			depth--
		}
	}
	return depth
}
