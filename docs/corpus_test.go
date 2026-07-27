package docs_test

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/mdsnippet"
)

// The prose corpus outside SPEC.md is gated by the same fence vocabulary.
//
// README.md is the project's front door, and the plugin reference corpus is
// read by a model that will reproduce whatever it says — so an example that
// stopped being valid there is copied forward rather than merely misread. Both
// were ungated until this test existed; the gates themselves live in
// [github.com/simon-lentz/yammm/internal/mdsnippet], shared with SPEC.md so a
// fence tag cannot come to mean two different things.
//
// These tests live here rather than beside the files they read because
// claude-plugin/ ships as a Claude Code plugin, and Go source does not belong
// in a distributed artifact.

const (
	readmeFile = "../README.md"
	pluginRoot = "../claude-plugin/skills"
)

// corpusFiles returns README.md plus every Markdown file in the plugin skills
// tree, in a deterministic order.
func corpusFiles(t *testing.T) []string {
	t.Helper()
	files := []string{readmeFile}
	err := filepath.WalkDir(pluginRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".md") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", pluginRoot, err)
	}
	// The walk is the only thing standing between "the plugin corpus is gated"
	// and "the gate quietly covers one file", so a shrunken tree is an error
	// rather than a silent pass.
	if len(files) < 10 {
		t.Fatalf("expected the plugin skills tree to contribute many Markdown files, walked %d: %v",
			len(files), files)
	}
	return files
}

func corpusBlocks(t *testing.T) []mdsnippet.Block {
	t.Helper()
	blocks, err := mdsnippet.ExtractFiles(corpusFiles(t)...)
	if err != nil {
		t.Fatalf("extracting blocks: %v", err)
	}
	if len(blocks) == 0 {
		t.Fatal("no yammm code blocks found across README.md and the plugin corpus")
	}
	return blocks
}

func TestCorpusExamples(t *testing.T) {
	t.Parallel()
	checked := mdsnippet.AssertParses(t, corpusBlocks(t))
	if checked == 0 {
		t.Error("no block was parse-checked; this test is asserting nothing")
	}
	t.Logf("parse-checked %d blocks", checked)
}

func TestCorpusLoadableExamples(t *testing.T) {
	t.Parallel()
	t.Logf("load-checked %d blocks", mdsnippet.AssertLoads(t, corpusBlocks(t)))
}

// TestCorpusInvalidExamples is the gate with no counterpart elsewhere: the
// plugin's common-mistakes reference is built entirely from WRONG/RIGHT pairs,
// and a WRONG half that a loosened loader has quietly made valid is worse than
// an untested one — the document keeps presenting it as a mistake to avoid.
func TestCorpusInvalidExamples(t *testing.T) {
	t.Parallel()
	checked := mdsnippet.AssertInvalid(t, corpusBlocks(t))
	if checked == 0 {
		t.Errorf("no ```%s blocks found; the WRONG-example gate is asserting nothing",
			mdsnippet.TagInvalid)
	}
	t.Logf("invalid-checked %d blocks", checked)
}

func TestCorpusDocumentedDiagnostics(t *testing.T) {
	t.Parallel()
	checked := mdsnippet.AssertDocumentedDiagnostics(t, corpusBlocks(t))
	if checked == 0 {
		t.Error("no example names a diagnostic code in a comment; this test is asserting nothing")
	}
	t.Logf("diagnostic-checked %d blocks", checked)
}
