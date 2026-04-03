package format

import "strings"

// lineClass classifies a source line for blank-line collapsing.
type lineClass int

const (
	lineBlank   lineClass = iota // empty or whitespace-only
	lineComment                  // only contains // or part of /* */ block
	lineContent                  // declaration tokens (possibly with trailing comment)
)

// classifyLines classifies each line as blank, comment-only, or content.
// Tracks multiline /* ... */ blocks so interior lines are lineComment.
func classifyLines(lines []string) []lineClass {
	classes := make([]lineClass, len(lines))
	inDocComment := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		if inDocComment {
			classes[i] = lineComment
			if strings.Contains(trimmed, "*/") {
				inDocComment = false
			}
			continue
		}

		if trimmed == "" {
			classes[i] = lineBlank
			continue
		}

		if strings.HasPrefix(trimmed, "/*") {
			classes[i] = lineComment
			if !strings.Contains(trimmed, "*/") {
				inDocComment = true
			}
			continue
		}

		if strings.HasPrefix(trimmed, "//") {
			classes[i] = lineComment
			continue
		}

		classes[i] = lineContent
	}
	return classes
}

// collapseBlankLines enforces the Section 10 blank line rules on Phase 1 output.
// It operates in two passes: first collapse/remove excess blank lines, then
// ensure required blank lines after schema and import declarations.
func collapseBlankLines(text string) string {
	if text == "" {
		return ""
	}

	lines := strings.Split(text, "\n")
	classes := classifyLines(lines)

	// Pass 1: collapse/remove blank lines.
	var result []string
	var resultClasses []lineClass

	for i := range lines {
		cls := classes[i]

		if cls != lineBlank {
			result = append(result, lines[i])
			resultClasses = append(resultClasses, cls)
			continue
		}

		// Rule 1: No blank lines at start of file.
		if len(result) == 0 {
			continue
		}

		// Rule 2/3/4: Max 1 blank line (collapse consecutive blanks).
		if resultEndsWithBlank(resultClasses) {
			continue
		}

		// Rule 7/9: No blank lines after '{'.
		if prevNonBlankEndsWithBrace(result, resultClasses) {
			continue
		}

		// Rule 8/9: No blank lines before '}'.
		if nextNonBlankStartsWithCloseBrace(lines, classes, i) {
			continue
		}

		// Otherwise: emit one blank line.
		result = append(result, "")
		resultClasses = append(resultClasses, lineBlank)
	}

	// Pass 2: ensure required blank lines.
	result, resultClasses = ensureBlankAfterSchema(result, resultClasses)
	result, resultClasses = ensureBlankAfterLastImport(result, resultClasses)
	_ = resultClasses // silence unused after final assignment

	return strings.Join(result, "\n")
}

// resultEndsWithBlank returns true if the last entry in result classes is blank.
func resultEndsWithBlank(classes []lineClass) bool {
	return len(classes) > 0 && classes[len(classes)-1] == lineBlank
}

// prevNonBlankEndsWithBrace returns true if the previous non-blank result line
// ends with '{'.
func prevNonBlankEndsWithBrace(result []string, classes []lineClass) bool {
	for i := len(result) - 1; i >= 0; i-- {
		if classes[i] == lineBlank {
			continue
		}
		trimmed := strings.TrimSpace(result[i])
		return strings.HasSuffix(trimmed, "{")
	}
	return false
}

// nextNonBlankStartsWithCloseBrace returns true if the next non-blank line in
// the input starts with '}'.
func nextNonBlankStartsWithCloseBrace(lines []string, classes []lineClass, startIdx int) bool {
	for i := startIdx + 1; i < len(lines); i++ {
		if classes[i] == lineBlank {
			continue
		}
		trimmed := strings.TrimSpace(lines[i])
		return strings.HasPrefix(trimmed, "}")
	}
	return false
}

// ensureBlankAfterSchema inserts a blank line after 'schema "..."' if the next
// line is non-blank.
func ensureBlankAfterSchema(lines []string, classes []lineClass) ([]string, []lineClass) {
	for i := range lines {
		if classes[i] != lineContent {
			continue
		}
		trimmed := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(trimmed, "schema ") {
			continue
		}

		// Found schema line. Check if next line is non-blank.
		if i+1 < len(lines) && classes[i+1] != lineBlank {
			// Insert blank line after schema.
			newLines := make([]string, 0, len(lines)+1)
			newClasses := make([]lineClass, 0, len(classes)+1)
			newLines = append(newLines, lines[:i+1]...)
			newClasses = append(newClasses, classes[:i+1]...)
			newLines = append(newLines, "")
			newClasses = append(newClasses, lineBlank)
			newLines = append(newLines, lines[i+1:]...)
			newClasses = append(newClasses, classes[i+1:]...)
			return newLines, newClasses
		}
		break
	}
	return lines, classes
}

// ensureBlankAfterLastImport inserts a blank line after the last import
// declaration if the following line is non-blank.
func ensureBlankAfterLastImport(lines []string, classes []lineClass) ([]string, []lineClass) {
	lastImportIdx := -1
	for i := range lines {
		if classes[i] != lineContent {
			continue
		}
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "import ") {
			lastImportIdx = i
		}
	}

	if lastImportIdx < 0 {
		return lines, classes
	}

	// Check if next line after last import is non-blank.
	if lastImportIdx+1 < len(lines) && classes[lastImportIdx+1] != lineBlank {
		newLines := make([]string, 0, len(lines)+1)
		newClasses := make([]lineClass, 0, len(classes)+1)
		newLines = append(newLines, lines[:lastImportIdx+1]...)
		newClasses = append(newClasses, classes[:lastImportIdx+1]...)
		newLines = append(newLines, "")
		newClasses = append(newClasses, lineBlank)
		newLines = append(newLines, lines[lastImportIdx+1:]...)
		newClasses = append(newClasses, classes[lastImportIdx+1:]...)
		return newLines, newClasses
	}

	return lines, classes
}
