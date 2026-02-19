package lsp

import (
	"fmt"
	"strings"

	"github.com/simon-lentz/yammm/location"
)

// CodeBlock represents a yammm fenced code block extracted from a markdown document.
// SourceID is intentionally zero-valued after extraction; Phase 2's workspace
// integration populates it via VirtualSourceID.
type CodeBlock struct {
	Content   string            // Block content (without fences), lines joined by "\n"
	SourceID  location.SourceID // Virtual SourceID — zero-value from ExtractCodeBlocks
	StartLine int               // 0-based line where content starts (line after opening fence)
	EndLine   int               // 0-based line of closing fence
	FenceChar byte              // '`' or '~'
}

// markdownState represents the current state of the markdown parser.
type markdownState int

const (
	stateNormal  markdownState = iota
	stateInBlock markdownState = iota
)

// ExtractCodeBlocks extracts yammm fenced code blocks from markdown content.
// Content should have normalized line endings (LF only), matching the workspace
// normalization contract (normalizeLineEndings in server.go).
// Returns blocks in document order with accurate line positions.
func ExtractCodeBlocks(content string) []CodeBlock {
	lines := strings.Split(content, "\n")
	state := stateNormal

	var blocks []CodeBlock
	var fenceChar byte
	var fenceLen int
	var blockStartLine int
	var contentLines []string

	for lineNum, line := range lines {
		switch state {
		case stateNormal:
			// Measure leading spaces.
			trimmed := strings.TrimLeft(line, " ")
			indent := len(line) - len(trimmed)

			// 4+ spaces is indented code block territory, not a fence.
			if indent > 3 {
				continue
			}

			// 1-3 space indented fences are explicitly skipped per §6.1.
			if indent >= 1 {
				continue
			}

			// Only zero-indent reaches here — scan for 3+ consecutive '`' or '~'.
			ch, count := scanFenceChars(line)
			if count < 3 {
				continue
			}

			// Parse info string (everything after fence chars).
			infoString := strings.TrimSpace(line[count:])
			if !strings.EqualFold(infoString, "yammm") {
				continue
			}

			// Enter IN_BLOCK state.
			fenceChar = ch
			fenceLen = count
			blockStartLine = lineNum + 1
			contentLines = nil
			state = stateInBlock

		case stateInBlock:
			// Strip up to 3 leading spaces for closing fence check.
			stripped, closingIndent := stripUpTo3Spaces(line)

			// Check for closing fence.
			if closingIndent <= 3 && len(stripped) > 0 && stripped[0] == fenceChar {
				count := countLeadingChar(stripped, fenceChar)
				if count >= fenceLen && isBlankOrEmpty(stripped[count:]) {
					// Valid closing fence — emit block if non-empty.
					joined := strings.Join(contentLines, "\n")
					if strings.TrimSpace(joined) != "" {
						blocks = append(blocks, CodeBlock{
							Content:   joined,
							StartLine: blockStartLine,
							EndLine:   lineNum,
							FenceChar: fenceChar,
						})
					}
					state = stateNormal
					continue
				}
			}

			// Not a closing fence — accumulate content line.
			contentLines = append(contentLines, line)
		}
	}

	return blocks
}

// VirtualSourceID creates a virtual SourceID for a code block within a markdown file.
// markdownPath must be an absolute path (from URIToPath). blockIndex is the 0-based
// index of the block within the markdown file.
func VirtualSourceID(markdownPath string, blockIndex int) (location.SourceID, error) {
	virtualPath := fmt.Sprintf("%s#block-%d", markdownPath, blockIndex)
	id, err := location.SourceIDFromAbsolutePath(virtualPath)
	if err != nil {
		return location.SourceID{}, fmt.Errorf("virtual source ID for %s block %d: %w",
			markdownPath, blockIndex, err)
	}
	return id, nil
}

// scanFenceChars returns the fence character and count of consecutive fence
// characters from the start of the line. Returns (0, 0) if the line doesn't
// start with '`' or '~'.
func scanFenceChars(line string) (byte, int) {
	if len(line) == 0 {
		return 0, 0
	}
	ch := line[0]
	if ch != '`' && ch != '~' {
		return 0, 0
	}
	count := 0
	for count < len(line) && line[count] == ch {
		count++
	}
	return ch, count
}

// stripUpTo3Spaces strips 0-3 leading spaces from a line.
// Returns the stripped line and the number of spaces removed.
func stripUpTo3Spaces(line string) (string, int) {
	indent := 0
	for indent < 3 && indent < len(line) && line[indent] == ' ' {
		indent++
	}
	return line[indent:], indent
}

// countLeadingChar counts consecutive occurrences of ch from the start of s.
func countLeadingChar(s string, ch byte) int {
	count := 0
	for count < len(s) && s[count] == ch {
		count++
	}
	return count
}

// isBlankOrEmpty reports whether s is empty or contains only whitespace.
func isBlankOrEmpty(s string) bool {
	return strings.TrimSpace(s) == ""
}

// MarkdownDocument tracks code blocks in an open markdown file.
// This is workspace-internal mutable state — server handlers must NOT
// access this type directly. Use MarkdownDocumentSnapshot (obtained via
// GetMarkdownDocumentSnapshot) for safe concurrent reads.
//
// ATOMICITY INVARIANT: Blocks and Snapshots must only be replaced together,
// atomically under the workspace lock, by AnalyzeMarkdownAndPublish.
type MarkdownDocument struct {
	URI       string
	Version   int
	Text      string
	Blocks    []CodeBlock
	Snapshots []*Snapshot
}

// MarkdownDocumentSnapshot is an immutable view of a MarkdownDocument.
// Text is deliberately excluded — handlers never need the full markdown content.
type MarkdownDocumentSnapshot struct {
	URI       string
	Version   int
	Blocks    []CodeBlock
	Snapshots []*Snapshot
}

// BlockPosition maps a markdown position to a specific block.
// Returned as a pointer from MarkdownPositionToBlock; nil means outside all blocks.
type BlockPosition struct {
	BlockIndex int
	LocalLine  int
	LocalChar  int
}

// MarkdownPositionToBlock converts a markdown position to block-local coordinates.
// Only line numbers are adjusted; character offsets pass through unchanged.
func (snap *MarkdownDocumentSnapshot) MarkdownPositionToBlock(line, char int) *BlockPosition {
	for i, block := range snap.Blocks {
		contentEndLine := block.EndLine - 1
		if line >= block.StartLine && line <= contentEndLine {
			return &BlockPosition{
				BlockIndex: i,
				LocalLine:  line - block.StartLine,
				LocalChar:  char,
			}
		}
	}
	return nil
}

// BlockPositionToMarkdown converts block-local coordinates to markdown position.
// Only line numbers are adjusted; character offsets pass through unchanged.
func (snap *MarkdownDocumentSnapshot) BlockPositionToMarkdown(blockIndex, localLine, localChar int) (int, int) {
	if blockIndex < 0 || blockIndex >= len(snap.Blocks) {
		return -1, -1
	}
	return snap.Blocks[blockIndex].StartLine + localLine, localChar
}

// buildBlockDocumentSnapshot creates a DocumentSnapshot for a single code block
// within a markdown document. This is the shared utility used by all feature
// providers (hover, completion, definition, symbols) to bridge between
// markdown-level state and block-level analysis.
//
// URI and SourceID intentionally differ: URI is the markdown file URI (for
// display/logging), while SourceID is the virtual block identifier (for source
// registry lookups in position conversion).
func (s *Server) buildBlockDocumentSnapshot(mdSnap *MarkdownDocumentSnapshot, block CodeBlock) *DocumentSnapshot {
	depths, inComment := ComputeBraceDepths(block.Content)
	return &DocumentSnapshot{
		URI:      mdSnap.URI,
		SourceID: block.SourceID,
		Version:  mdSnap.Version,
		Text:     block.Content,
		LineState: &LineState{
			Version:        mdSnap.Version,
			BraceDepth:     depths,
			InBlockComment: inComment,
		},
	}
}
