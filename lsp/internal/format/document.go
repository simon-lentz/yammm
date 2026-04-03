package format

import "strings"

func finalizeFormattedText(text string) string {
	lines := strings.Split(text, "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		result = append(result, strings.TrimRight(line, " \t"))
	}

	for len(result) > 0 && result[len(result)-1] == "" {
		result = result[:len(result)-1]
	}

	formatted := strings.Join(result, "\n")
	if formatted != "" && !strings.HasSuffix(formatted, "\n") {
		formatted += "\n"
	}
	return formatted
}

// NormalizeIndentation converts spaces to tabs for indentation.
// Each 4 spaces at the start of a line becomes 1 tab.
func NormalizeIndentation(line string) string {
	if line == "" {
		return line
	}

	// Count leading whitespace
	leadingWS := 0
	for _, r := range line {
		if r == ' ' || r == '\t' {
			leadingWS++
		} else {
			break
		}
	}

	if leadingWS == 0 {
		return line
	}

	// Extract leading whitespace and content
	leading := line[:leadingWS]
	content := line[leadingWS:]

	// Convert to tabs: count equivalent spaces (tab = 4 spaces)
	spaceCount := 0
	for _, r := range leading {
		if r == '\t' {
			spaceCount += 4
		} else {
			spaceCount++
		}
	}

	// Convert to tabs
	tabs := spaceCount / 4
	remaining := spaceCount % 4

	return strings.Repeat("\t", tabs) + strings.Repeat(" ", remaining) + content
}
