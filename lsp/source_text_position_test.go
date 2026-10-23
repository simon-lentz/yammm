package lsp_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/lsp"
	"github.com/simon-lentz/yammm/lsp/internal/lsputil"
	"github.com/simon-lentz/yammm/lsp/internal/protocol"
	"github.com/simon-lentz/yammm/lsp/internal/testutil"
)

// A position is a line and a count of code units from that line's start in
// the text the client holds (LSP 3.17, "Position"): UTF-16 units, or bytes
// under the utf-8 position encoding. For an open document that text is what
// didOpen sent. For a file the server read from disk it is the text an editor
// shows when it opens the file, and an editor decodes a leading byte order
// mark away, so the mark is no character of line 1.
//
// Line 1 of every dependency below holds a two-byte rune and a rune outside
// the Basic Multilingual Plane before the region names the table asks for, so
// a count of runes, of UTF-16 units and of bytes each give another answer
// there; the schema name stands before both runes.
const (
	byteOrderMark = "\xef\xbb\xbf"

	positionEntry = "schema \"entry\"\n" +
		"\n" +
		"import \"./dep\" as d\n" +
		"\n" +
		"type L extends d.Region {\n\tx String\n}\n" +
		"\n" +
		"type M extends d.City {\n\ty String\n}\n"

	// Line 0: schema "dep" /* é😀 */ type Region {
	// Line 3: type City {
	positionSoundDep = "schema \"dep\" /* é\U0001F600 */ type Region {\n" +
		"\tid String primary\n" +
		"}\n" +
		"type City {\n" +
		"\tid String primary\n" +
		"}\n"

	// Line 0: schema "dep" /* é😀 */ type Region { id Missing primary }
	// Line 1: /* 😀 */ type City { id Absent primary }
	positionFaultyDep = "schema \"dep\" /* é\U0001F600 */ type Region { id Missing primary }\n" +
		"/* \U0001F600 */ type City { id Absent primary }\n"
)

// span is one line's start and end character.
type span struct{ line, start, end uint32 }

func (s span) shifted(by uint32) span { return span{s.line, s.start + by, s.end + by} }

func (s span) String() string { return fmt.Sprintf("%d:%d-%d:%d", s.line, s.start, s.line, s.end) }

func spanOf(r protocol.Range) span {
	if r.Start.Line != r.End.Line {
		return span{line: ^uint32(0)}
	}
	return span{r.Start.Line, r.Start.Character, r.End.Character}
}

// positionAnswers holds the answers for a client whose text of the dependency
// starts at its first character. A client whose text starts with a mark
// counts the mark too, on line 0 alone: one UTF-16 unit, three bytes.
type positionAnswers struct {
	markWidth uint32

	schemaName span // `schema "dep"`, the target of the import alias
	region     span // `Region`, after the two wide runes
	city       span // `City`, on a later line

	regionFault span // `id Missing primary`, line 0
	cityFault   span // `id Absent primary`, line 1, after one wide rune
}

var answersByEncoding = map[lsputil.PositionEncoding]positionAnswers{
	// é is one unit and 😀 is two.
	lsputil.PositionEncodingUTF16: {
		markWidth:   1,
		schemaName:  span{0, 0, 12},
		region:      span{0, 28, 34},
		city:        span{3, 5, 9},
		regionFault: span{0, 37, 55},
		cityFault:   span{1, 21, 38},
	},
	// é is two bytes and 😀 is four.
	lsputil.PositionEncodingUTF8: {
		markWidth:   3,
		schemaName:  span{0, 0, 12},
		region:      span{0, 31, 37},
		city:        span{3, 5, 9},
		regionFault: span{0, 40, 58},
		cityFault:   span{1, 23, 40},
	},
}

func symbolNamed(symbols []protocol.DocumentSymbol, name string) (protocol.DocumentSymbol, bool) {
	for _, s := range symbols {
		if s.Name == name {
			return s, true
		}
		if found, ok := symbolNamed(s.Children, name); ok {
			return found, true
		}
	}
	return protocol.DocumentSymbol{}, false
}

// A sourceCase is one way the dependency reaches the server.
type sourceCase struct {
	name       string
	diskMarked bool
	// open is whether the client opened the dependency, and sentMarked
	// whether the text it sent starts with the mark.
	open, sentMarked bool
}

var sourceCases = []sourceCase{
	{name: "an unmarked file read from disk"},
	{name: "a marked file read from disk", diskMarked: true},
	{name: "an unmarked file, open", open: true},
	{name: "a marked file, open in an editor that decodes the mark away", diskMarked: true, open: true},
	{name: "a marked file, open in a client that sends the mark as text", diskMarked: true, open: true, sentMarked: true},
}

// startPositionServer writes entry and dep, opens dep as c says, then opens
// entry, so entry's analysis reads the dependency from where the case puts it.
func startPositionServer(t *testing.T, c sourceCase, enc lsputil.PositionEncoding, dep string) (h *testutil.Harness, entryPath, depPath string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	entryPath = filepath.Join(root, "entry.yammm")
	depPath = filepath.Join(root, "dep.yammm")

	onDisk := dep
	if c.diskMarked {
		onDisk = byteOrderMark + dep
	}
	for path, text := range map[string]string{depPath: onDisk, entryPath: positionEntry} {
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	h, srv := newTestHarnessWithServerConfig(t, root, lsp.Config{ModuleRoot: root, DebounceDelay: time.Millisecond})
	t.Cleanup(h.Close)
	srv.Workspace().SetPositionEncoding(enc)

	if c.open {
		sent := dep
		if c.sentMarked {
			sent = byteOrderMark + dep
		}
		if err := h.OpenDocument(depPath, sent); err != nil {
			t.Fatal(err)
		}
		h.Sync()
	}
	if err := h.OpenDocument(entryPath, positionEntry); err != nil {
		t.Fatal(err)
	}
	h.Sync()
	return h, entryPath, depPath
}

// expectSettled polls read until it returns want or analysisTimeout passes, and
// reports the last answer when it is another, so a failure prints what the
// server said.
func expectSettled(t *testing.T, h *testutil.Harness, what string, want span, read func() string) {
	t.Helper()
	deadline := time.Now().Add(analysisTimeout)
	for {
		h.Sync()
		got := read()
		if got == want.String() {
			return
		}
		if time.Now().After(deadline) {
			t.Errorf("%s: got %s, want %s", what, got, want)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestPositions_CountTheTextTheClientHolds(t *testing.T) {
	t.Parallel()

	for _, enc := range []lsputil.PositionEncoding{lsputil.PositionEncodingUTF16, lsputil.PositionEncodingUTF8} {
		answers := answersByEncoding[enc]
		for _, c := range sourceCases {
			var lead uint32
			if c.sentMarked {
				lead = answers.markWidth
			}

			t.Run(fmt.Sprintf("%s/%s/definition", enc, c.name), func(t *testing.T) {
				t.Parallel()
				h, entryPath, depPath := startPositionServer(t, c, enc, positionSoundDep)

				definition := func(line, char int) func() string {
					return func() string {
						loc, err := h.Definition(entryPath, line, char)
						if err != nil || loc == nil {
							return fmt.Sprintf("no location (%v)", err)
						}
						return spanOf(loc.Range).String()
					}
				}
				for _, q := range []struct {
					what       string
					line, char int
					want       span
				}{
					{"the import alias reaches the schema name on line 0", 2, 17, answers.schemaName.shifted(lead)},
					{"d.Region reaches Region on line 0", 4, 18, answers.region.shifted(lead)},
					{"d.City reaches City on line 3", 8, 18, answers.city},
				} {
					expectSettled(t, h, q.what, q.want, definition(q.line, q.char))
				}

				if !c.open {
					return
				}
				region := answers.region.shifted(lead)
				expectSettled(t, h, "the open dependency's own Region symbol", region, func() string {
					symbols, err := h.DocumentSymbols(depPath)
					if err != nil {
						return err.Error()
					}
					if region, ok := symbolNamed(symbols, "Region"); ok {
						return spanOf(region.SelectionRange).String()
					}
					return "no Region symbol"
				})

				// The client asks at its own position of Region's second
				// letter, and the hover's range is Region in its own text.
				expectSettled(t, h, "a hover on Region in the open dependency", region, func() string {
					hover, err := h.Hover(depPath, int(region.line), int(region.start)+1)
					if err != nil || hover == nil || hover.Range == nil {
						return fmt.Sprintf("no hover range (%v)", err)
					}
					return spanOf(*hover.Range).String()
				})
			})

			t.Run(fmt.Sprintf("%s/%s/diagnostics", enc, c.name), func(t *testing.T) {
				t.Parallel()
				h, _, depPath := startPositionServer(t, c, enc, positionFaultyDep)

				unknownType := func(line uint32) func() string {
					return func() string {
						for _, d := range h.Diagnostics(testutil.PathToURI(depPath)) {
							if d.Code != nil && d.Code.Value == "E_UNKNOWN_TYPE" && d.Range.Start.Line == line {
								return spanOf(d.Range).String()
							}
						}
						return "no diagnostic"
					}
				}
				expectSettled(t, h, "the unknown type on line 0", answers.regionFault.shifted(lead), unknownType(0))
				expectSettled(t, h, "the unknown type on line 1", answers.cityFault, unknownType(1))
			})
		}
	}
}
