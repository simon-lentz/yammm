package format

import (
	"slices"
	"strings"
)

// collapseBlankLines enforces SPEC.md blank line rules on the token-rewritten output.
// It operates in two passes: first collapse/remove excess blank lines, then
// ensure required blank lines after schema and import declarations.
func collapseBlankLines(ls []line) []line {
	if len(ls) == 0 {
		return ls
	}

	// Pass 1: collapse/remove blank lines.
	var result []line

	for i, ln := range ls {
		if ln.class != lineBlank {
			result = append(result, ln)
			continue
		}

		// Rule 1: No blank lines at start of file.
		if len(result) == 0 {
			continue
		}

		// Rule 2/3/4: Max 1 blank line (collapse consecutive blanks).
		if resultEndsWithBlank(result) {
			continue
		}

		// Rule 7/9: No blank lines after '{'.
		if prevNonBlankEndsWithBrace(result) {
			continue
		}

		// Rule 8/9: No blank lines before '}'.
		if nextNonBlankStartsWithCloseBrace(ls, i) {
			continue
		}

		// Otherwise: emit one blank line.
		result = append(result, line{class: lineBlank})
	}

	// Pass 2: ensure required blank lines.
	result = ensureBlankAfterSchema(result)
	result = ensureBlankAfterLastImport(result)
	return result
}

// resultEndsWithBlank returns true if the last result line is blank.
func resultEndsWithBlank(result []line) bool {
	return len(result) > 0 && result[len(result)-1].class == lineBlank
}

// prevNonBlankEndsWithBrace returns true if the previous non-blank result line
// ends with '{'.
func prevNonBlankEndsWithBrace(result []line) bool {
	for _, ln := range slices.Backward(result) {
		if ln.class == lineBlank {
			continue
		}
		return strings.HasSuffix(strings.TrimSpace(ln.text), "{")
	}
	return false
}

// nextNonBlankStartsWithCloseBrace returns true if the next non-blank line in
// the input starts with '}'.
func nextNonBlankStartsWithCloseBrace(ls []line, startIdx int) bool {
	for _, ln := range ls[startIdx+1:] {
		if ln.class == lineBlank {
			continue
		}
		return strings.HasPrefix(strings.TrimSpace(ln.text), "}")
	}
	return false
}

// ensureBlankAfterSchema inserts a blank line after 'schema "..."' if the next
// line is non-blank.
func ensureBlankAfterSchema(ls []line) []line {
	for i, ln := range ls {
		if ln.class != lineContent {
			continue
		}
		if !strings.HasPrefix(strings.TrimSpace(ln.text), "schema ") {
			continue
		}
		if i+1 < len(ls) && ls[i+1].class != lineBlank {
			return slices.Insert(slices.Clone(ls), i+1, line{class: lineBlank})
		}
		break
	}
	return ls
}

// ensureBlankAfterLastImport inserts a blank line after the last import
// declaration if the following line is non-blank.
func ensureBlankAfterLastImport(ls []line) []line {
	lastImportIdx := -1
	for i, ln := range ls {
		if ln.class != lineContent {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(ln.text), "import ") {
			lastImportIdx = i
		}
	}
	if lastImportIdx < 0 {
		return ls
	}
	if lastImportIdx+1 < len(ls) && ls[lastImportIdx+1].class != lineBlank {
		return slices.Insert(slices.Clone(ls), lastImportIdx+1, line{class: lineBlank})
	}
	return ls
}
