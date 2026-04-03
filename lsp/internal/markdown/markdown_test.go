package markdown_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/lsp/internal/docstate"
	"github.com/simon-lentz/yammm/lsp/internal/markdown"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractCodeBlocks_BasicExtraction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		input      string
		wantCount  int
		wantBlocks []markdown.CodeBlock
	}{
		{
			name:      "single backtick block",
			input:     "# Heading\n\n```yammm\nschema \"test\"\n```\n",
			wantCount: 1,
			wantBlocks: []markdown.CodeBlock{
				{
					Content:   "schema \"test\"",
					StartLine: 3,
					EndLine:   4,
					FenceChar: '`',
				},
			},
		},
		{
			name:      "single tilde block",
			input:     "~~~yammm\nschema \"test\"\n~~~\n",
			wantCount: 1,
			wantBlocks: []markdown.CodeBlock{
				{
					Content:   "schema \"test\"",
					StartLine: 1,
					EndLine:   2,
					FenceChar: '~',
				},
			},
		},
		{
			name:      "multiple blocks",
			input:     "```yammm\nschema \"one\"\n```\n\n~~~yammm\nschema \"two\"\n~~~\n",
			wantCount: 2,
			wantBlocks: []markdown.CodeBlock{
				{
					Content:   "schema \"one\"",
					StartLine: 1,
					EndLine:   2,
					FenceChar: '`',
				},
				{
					Content:   "schema \"two\"",
					StartLine: 5,
					EndLine:   6,
					FenceChar: '~',
				},
			},
		},
		{
			name:      "no yammm blocks",
			input:     "# Heading\n\nSome text.\n",
			wantCount: 0,
		},
		{
			name:      "non-yammm block ignored",
			input:     "```go\nfunc main() {}\n```\n",
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			blocks := markdown.ExtractCodeBlocks(tt.input)
			assert.Len(t, blocks, tt.wantCount)
			for i, want := range tt.wantBlocks {
				if i >= len(blocks) {
					break
				}
				assert.Equal(t, want.Content, blocks[i].Content, "block %d content", i)
				assert.Equal(t, want.StartLine, blocks[i].StartLine, "block %d start line", i)
				assert.Equal(t, want.EndLine, blocks[i].EndLine, "block %d end line", i)
				assert.Equal(t, want.FenceChar, blocks[i].FenceChar, "block %d fence char", i)
				assert.True(t, blocks[i].SourceID.IsZero(), "block %d SourceID should be zero", i)
			}
		})
	}
}

func TestExtractCodeBlocks_InfoStringMatching(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantCount int
	}{
		{
			name:      "uppercase YAMMM",
			input:     "```YAMMM\nschema \"test\"\n```\n",
			wantCount: 1,
		},
		{
			name:      "mixed case Yammm",
			input:     "```Yammm\nschema \"test\"\n```\n",
			wantCount: 1,
		},
		{
			name:      "mixed case yAmMm",
			input:     "```yAmMm\nschema \"test\"\n```\n",
			wantCount: 1,
		},
		{
			name:      "whitespace around info string",
			input:     "```  yammm  \nschema \"test\"\n```\n",
			wantCount: 1,
		},
		{
			name:      "trailing token rejected",
			input:     "```yammm schema\ntype T { id String primary }\n```\n",
			wantCount: 0,
		},
		{
			name:      "empty info string not matched",
			input:     "```\nschema \"test\"\n```\n",
			wantCount: 0,
		},
		{
			name:      "partial yam not matched",
			input:     "```yam\nschema \"test\"\n```\n",
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			blocks := markdown.ExtractCodeBlocks(tt.input)
			assert.Len(t, blocks, tt.wantCount)
		})
	}
}

func TestExtractCodeBlocks_FenceMechanics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantCount int
	}{
		{
			name:      "long opening fence needs matching close",
			input:     "`````yammm\nschema \"test\"\n`````\n",
			wantCount: 1,
		},
		{
			name:      "mismatched char backtick open tilde close",
			input:     "```yammm\nschema \"test\"\n~~~\n",
			wantCount: 0,
		},
		{
			name:      "short close not valid",
			input:     "`````yammm\nschema \"test\"\n```\n",
			wantCount: 0,
		},
		{
			name:      "long close valid per CommonMark",
			input:     "```yammm\nschema \"test\"\n`````\n",
			wantCount: 1,
		},
		{
			name:      "closing with trailing whitespace valid",
			input:     "```yammm\nschema \"test\"\n```   \n",
			wantCount: 1,
		},
		{
			name:      "closing with trailing text not valid",
			input:     "```yammm\nschema \"test\"\n``` text\n",
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			blocks := markdown.ExtractCodeBlocks(tt.input)
			assert.Len(t, blocks, tt.wantCount)
		})
	}
}

func TestExtractCodeBlocks_IndentHandling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantCount int
	}{
		{
			name:      "1-space indented opening skipped",
			input:     " ```yammm\nschema \"test\"\n ```\n",
			wantCount: 0,
		},
		{
			name:      "3-space indented opening skipped",
			input:     "   ```yammm\nschema \"test\"\n   ```\n",
			wantCount: 0,
		},
		{
			name:      "closing with 1-3 spaces valid",
			input:     "```yammm\nschema \"test\"\n   ```\n",
			wantCount: 1,
		},
		{
			name:      "closing with 4 spaces not valid",
			input:     "```yammm\nschema \"test\"\n    ```\n",
			wantCount: 0,
		},
		{
			name:      "4+ space indented line ignored",
			input:     "    ```yammm\nschema \"test\"\n    ```\n",
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			blocks := markdown.ExtractCodeBlocks(tt.input)
			assert.Len(t, blocks, tt.wantCount)
		})
	}
}

func TestExtractCodeBlocks_EmptyWhitespaceBlocks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantCount int
	}{
		{
			name:      "empty block excluded",
			input:     "```yammm\n```\n",
			wantCount: 0,
		},
		{
			name:      "whitespace-only block excluded",
			input:     "```yammm\n   \n\n  \n```\n",
			wantCount: 0,
		},
		{
			name:      "comment-only block included",
			input:     "```yammm\n// TODO\n```\n",
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			blocks := markdown.ExtractCodeBlocks(tt.input)
			assert.Len(t, blocks, tt.wantCount)
		})
	}
}

func TestExtractCodeBlocks_NestedContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantCount int
	}{
		{
			name:      "backticks inside backtick block shorter than opening",
			input:     "`````yammm\n// ```\nschema \"test\"\n`````\n",
			wantCount: 1,
		},
		{
			name:      "tildes inside backtick block",
			input:     "```yammm\n// ~~~\nschema \"test\"\n```\n",
			wantCount: 1,
		},
		{
			name:      "markdown syntax in content",
			input:     "```yammm\n// # heading\n// **bold**\nschema \"test\"\n```\n",
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			blocks := markdown.ExtractCodeBlocks(tt.input)
			assert.Len(t, blocks, tt.wantCount)
		})
	}
}

func TestExtractCodeBlocks_EdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantCount int
	}{
		{
			name:      "unclosed fence no block",
			input:     "```yammm\nschema \"test\"\n",
			wantCount: 0,
		},
		{
			name:      "consecutive blocks both extracted",
			input:     "```yammm\nschema \"a\"\n```\n```yammm\nschema \"b\"\n```\n",
			wantCount: 2,
		},
		{
			name:      "orphan closing fence ignored",
			input:     "```\n\n```yammm\nschema \"test\"\n```\n",
			wantCount: 1,
		},
		{
			name:      "empty input",
			input:     "",
			wantCount: 0,
		},
		{
			name:      "no trailing newline",
			input:     "```yammm\nschema \"test\"\n```",
			wantCount: 1,
		},
		{
			name: "mixed valid invalid empty",
			input: "```yammm\n```\n\n" + // empty — excluded
				"```yammm\nschema \"valid\"\n```\n\n" + // valid
				"```yammm\nschema \"unclosed\"\n", // unclosed
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			blocks := markdown.ExtractCodeBlocks(tt.input)
			assert.Len(t, blocks, tt.wantCount)
		})
	}
}

func TestExtractCodeBlocks_PositionAccuracy(t *testing.T) {
	t.Parallel()

	// Lines (0-based):
	// 0: "# Heading"
	// 1: ""
	// 2: "```yammm"
	// 3: "schema \"test\""
	// 4: ""
	// 5: "type Foo {"
	// 6: "    id String primary"
	// 7: "}"
	// 8: "```"
	// 9: ""
	input := "# Heading\n\n```yammm\nschema \"test\"\n\ntype Foo {\n    id String primary\n}\n```\n"

	blocks := markdown.ExtractCodeBlocks(input)
	require.Len(t, blocks, 1)

	block := blocks[0]
	assert.Equal(t, 3, block.StartLine, "StartLine is line after opening fence")
	assert.Equal(t, 8, block.EndLine, "EndLine is line of closing fence")
	assert.Equal(t, "schema \"test\"\n\ntype Foo {\n    id String primary\n}", block.Content)
}

func TestVirtualSourceID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		path       string
		blockIndex int
		wantErr    bool
		wantSuffix string
	}{
		{
			name:       "basic path",
			path:       "/home/user/docs/README.md",
			blockIndex: 0,
			wantSuffix: "#block-0",
		},
		{
			name:       "second block",
			path:       "/home/user/docs/README.md",
			blockIndex: 1,
			wantSuffix: "#block-1",
		},
		{
			name:       "third block",
			path:       "/home/user/docs/README.md",
			blockIndex: 2,
			wantSuffix: "#block-2",
		},
		{
			name:       "non-absolute path errors",
			path:       "relative/path.md",
			blockIndex: 0,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			id, err := markdown.VirtualSourceID(tt.path, tt.blockIndex)
			if tt.wantErr {
				assert.Error(t, err)
				assert.True(t, id.IsZero())
				return
			}
			require.NoError(t, err)
			assert.False(t, id.IsZero())
			assert.Contains(t, id.String(), tt.wantSuffix)
		})
	}
}

func TestVirtualSourceID_Distinct(t *testing.T) {
	t.Parallel()

	id0, err := markdown.VirtualSourceID("/path/to/file.md", 0)
	require.NoError(t, err)
	id1, err := markdown.VirtualSourceID("/path/to/file.md", 1)
	require.NoError(t, err)
	id2, err := markdown.VirtualSourceID("/path/to/file.md", 2)
	require.NoError(t, err)

	assert.NotEqual(t, id0, id1)
	assert.NotEqual(t, id1, id2)
	assert.NotEqual(t, id0, id2)
}

func TestExtractCodeBlocks_Fixtures(t *testing.T) {
	t.Parallel()

	fixtureDir := filepath.Join("..", "..", "testdata", "lsp", "markdown")

	tests := []struct {
		name      string
		file      string
		wantCount int
	}{
		{
			name:      "simple.md",
			file:      "simple.md",
			wantCount: 1,
		},
		{
			name:      "multiple.md",
			file:      "multiple.md",
			wantCount: 2,
		},
		{
			name:      "empty.md",
			file:      "empty.md",
			wantCount: 1,
		},
		{
			name:      "malformed.md",
			file:      "malformed.md",
			wantCount: 0,
		},
		{
			name:      "nested.md",
			file:      "nested.md",
			wantCount: 1,
		},
		{
			name:      "indented.md",
			file:      "indented.md",
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data, err := os.ReadFile(filepath.Join(fixtureDir, tt.file))
			require.NoError(t, err)

			// Normalize line endings like the workspace does.
			content := docstate.NormalizeLineEndings(string(data))
			blocks := markdown.ExtractCodeBlocks(content)
			assert.Len(t, blocks, tt.wantCount)
		})
	}
}

func TestExtractCodeBlocks_FixtureSimple_Details(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "lsp", "markdown", "simple.md"))
	require.NoError(t, err)
	content := docstate.NormalizeLineEndings(string(data))

	blocks := markdown.ExtractCodeBlocks(content)
	require.Len(t, blocks, 1)

	block := blocks[0]
	assert.Equal(t, byte('`'), block.FenceChar)
	assert.Contains(t, block.Content, "schema \"test_simple\"")
	assert.Contains(t, block.Content, "type Person")
	assert.Contains(t, block.Content, "id String primary")
}

func TestExtractCodeBlocks_FixtureMultiple_Details(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "lsp", "markdown", "multiple.md"))
	require.NoError(t, err)
	content := docstate.NormalizeLineEndings(string(data))

	blocks := markdown.ExtractCodeBlocks(content)
	require.Len(t, blocks, 2)

	assert.Equal(t, byte('`'), blocks[0].FenceChar)
	assert.Contains(t, blocks[0].Content, "schema \"block_one\"")

	assert.Equal(t, byte('~'), blocks[1].FenceChar)
	assert.Contains(t, blocks[1].Content, "schema \"block_two\"")

	// Second block starts after first block ends.
	assert.Greater(t, blocks[1].StartLine, blocks[0].EndLine)
}

func TestExtractCodeBlocks_FixtureNested_Details(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "lsp", "markdown", "nested.md"))
	require.NoError(t, err)
	content := docstate.NormalizeLineEndings(string(data))

	blocks := markdown.ExtractCodeBlocks(content)
	require.Len(t, blocks, 1)

	block := blocks[0]
	assert.Equal(t, byte('`'), block.FenceChar)
	assert.Contains(t, block.Content, "schema \"nested\"")
	// Shorter backtick and tilde lines should be content, not closers.
	assert.Contains(t, block.Content, "```")
	assert.Contains(t, block.Content, "~~~")
}

func TestExtractCodeBlocks_FixtureIndented_Details(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "lsp", "markdown", "indented.md"))
	require.NoError(t, err)
	content := docstate.NormalizeLineEndings(string(data))

	blocks := markdown.ExtractCodeBlocks(content)
	require.Len(t, blocks, 1)

	block := blocks[0]
	assert.Contains(t, block.Content, "schema \"valid_indent\"")
	assert.Contains(t, block.Content, "type IndentClose")
}

// --- hasSchemaDeclaration tests ---

func TestHasSchemaDeclaration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "standard schema declaration",
			content: "schema \"test\"\n\ntype Foo {\n    id String primary\n}",
			want:    true,
		},
		{
			name:    "no schema declaration",
			content: "type Foo {\n    id String primary\n}",
			want:    false,
		},
		{
			name:    "indented schema declaration",
			content: "  schema \"test\"",
			want:    true,
		},
		{
			name:    "tab after schema keyword",
			content: "schema\t\"test\"",
			want:    true,
		},
		{
			name:    "commented schema (conservative match)",
			content: "// schema \"test\"",
			want:    false, // Comments don't start with "schema " after trim
		},
		{
			name:    "empty content",
			content: "",
			want:    false,
		},
		{
			name:    "whitespace only",
			content: "   \n\n  ",
			want:    false,
		},
		{
			name:    "schema in field name does not match",
			content: "type Foo {\n    schema_name String required\n}",
			want:    false, // "schema_name" doesn't match "schema " or "schema\t"
		},
		{
			name:    "schema at middle of file",
			content: "// header\n\nschema \"test\"\n\ntype Foo {}",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, markdown.HasSchemaDeclaration(tt.content))
		})
	}
}

// --- Benchmarks ---

func BenchmarkExtractCodeBlocks_ManyBlocks(b *testing.B) {
	var sb strings.Builder
	for i := range 50 {
		fmt.Fprintf(&sb, "# Block %d\n\n```yammm\nschema \"block_%d\"\n\ntype Type%d {\n\tid String primary\n}\n```\n\n", i, i, i)
	}
	content := sb.String()
	b.ResetTimer()
	for b.Loop() {
		_ = markdown.ExtractCodeBlocks(content)
	}
}
