package snapshot_test

import (
	"bytes"
	"context"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/internal/snappartstest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

const textSchema = `schema "text"

type Note {
	id String primary
	body String
	tags List<String>
}
`

// textDocument is one Note in a document whose metadata value, key component and
// id, body and list element are placeholders. Each edit replaces one placeholder
// with text exactly as the document spells it.
func textDocument(t *testing.T) (string, schema.Schema) {
	t.Helper()
	s, res := schema.LoadString(t.Context(), textSchema, "text.yammm")
	if res.HasErrors() {
		t.Fatalf("schema: %s", res)
	}
	note, ok := s.Type("Note")
	if !ok {
		t.Fatal("no type Note")
	}
	doc := string(snappartstest.Document(t, s, snapparts.Parts{Instances: []snapparts.Instance{{
		TypeID: note.ID(), PrimaryKey: immutable.WrapKey([]any{"KEY"}),
		Properties: immutable.WrapProperties(map[string]any{"id": "KEY", "body": "BODY", "tags": []any{"ELEMENT"}}),
	}}}))
	return editOnce(t, doc, `"features":[]`, `"features":[],"metadata":{"run":"META"}`), s
}

// textPosition is a place a document spells a string, as the edit that puts text
// there.
type textPosition struct {
	name string
	from string
	to   func(text string) string
}

func spelledAs(name, from, prefix, suffix string) textPosition {
	return textPosition{name, from, func(text string) string { return prefix + text + suffix }}
}

var (
	metadataValue = spelledAs("a metadata value", `"run":"META"`, `"run":"`, `"`)
	typesRow      = spelledAs("a types row", `"name":"Note"`, `"name":"Note`, `"`)
	pastTheTypes  = []textPosition{
		spelledAs("a value", `"body":"BODY"`, `"body":"`, `"`),
		spelledAs("a key component", `"key":["KEY"]`, `"key":["`, `"]`),
		spelledAs("a property name", `"body":"BODY"`, `"body`, `":"BODY"`),
		spelledAs("a list element", `"tags":["ELEMENT"]`, `"tags":["`, `"]`),
	}
)

func (p textPosition) edit(t *testing.T, doc, text string) []byte {
	t.Helper()
	return []byte(editOnce(t, doc, p.from, p.to(text)))
}

// wholeDocumentReaders are the surfaces that hold the whole document.
var wholeDocumentReaders = []struct {
	name string
	read func(ctx context.Context, doc []byte, s schema.Schema) diag.Result
}{
	{"Load", func(ctx context.Context, doc []byte, s schema.Schema) diag.Result {
		_, res := snapshot.Load(ctx, doc, s)
		return res
	}},
	{"Verify", func(ctx context.Context, doc []byte, s schema.Schema) diag.Result {
		return snapshot.Verify(ctx, doc, s)
	}},
	{"Info", func(ctx context.Context, doc []byte, _ schema.Schema) diag.Result {
		_, res := snapshot.Info(ctx, doc)
		return res
	}},
	{"HeaderOnly", func(ctx context.Context, doc []byte, _ schema.Schema) diag.Result {
		_, res := snapshot.HeaderOnly(ctx, doc)
		return res
	}},
	{"UpdateMetadata", func(ctx context.Context, doc []byte, _ schema.Schema) diag.Result {
		_, res := snapshot.UpdateMetadata(ctx, doc, map[string]string{"k": "v"})
		return res
	}},
}

// uEscape returns the six bytes of a JSON unicode escape as a document spells it.
func uEscape(hex string) string { return `\` + "u" + hex }

// unreadableText is each spelling encoding/json would read as U+FFFD with no
// error, and the fragment of the refusal it draws.
var unreadableText = []struct{ name, spelling, want string }{
	{"a raw invalid byte", "\xff", "not valid UTF-8"},
	{"a raw lead byte with no continuation", "\xc3", "not valid UTF-8"},
	{"a raw surrogate in UTF-8", "\xed\xa0\x80", "not valid UTF-8"},
	{"a high surrogate alone", `\ud800`, `the surrogate escape \ud800`},
	{"a low surrogate alone", `\udc00`, `the surrogate escape \udc00`},
	{"a high surrogate before a letter", `\ud800x`, `the surrogate escape \ud800`},
	{"a high surrogate before a hex digit", `\ud800A`, `the surrogate escape \ud800`},
	{"two high surrogates", `\ud800\ud800`, `the surrogate escape \ud800`},
	{"a high surrogate before an escape that is no surrogate", uEscape("d83d") + uEscape("0041"), "the surrogate escape " + uEscape("d83d")},
	{"a pair in the wrong order", `\ude00\ud83d`, `the surrogate escape \ude00`},
	{"a lone surrogate after a valid pair", uEscape("d83d") + uEscape("de00") + uEscape("d800"), "the surrogate escape " + uEscape("d800")},
	{"a high surrogate alone in upper case", uEscape("D800"), "the surrogate escape " + uEscape("D800")},
	{"a low surrogate alone in upper case", uEscape("DC00"), "the surrogate escape " + uEscape("DC00")},
	{"a high surrogate alone in mixed case", uEscape("D83d"), "the surrogate escape " + uEscape("D83d")},
	{"a lone surrogate after an escaped backslash", `\\` + uEscape("d800"), "the surrogate escape " + uEscape("d800")},
}

// Every surface that holds the whole document refuses a string encoding/json
// would rewrite, wherever the document spells it, before it reads anything else.
func TestReaders_RefuseTextEncodingJSONWouldRewrite(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	doc, s := textDocument(t)
	for _, r := range wholeDocumentReaders {
		if res := r.read(ctx, []byte(doc), s); res.HasErrors() {
			t.Fatalf("control, %s: %s", r.name, res)
		}
	}
	for _, p := range append([]textPosition{metadataValue, typesRow}, pastTheTypes...) {
		for _, c := range unreadableText {
			edited := p.edit(t, doc, c.spelling)
			for _, r := range wholeDocumentReaders {
				res := r.read(ctx, edited, s)
				if res.Len() != 1 || !res.HasCode(diag.E_SNAPSHOT_MALFORMED) || !strings.Contains(res.String(), c.want) {
					t.Errorf("%s, %s, %s: %s, want one %s naming %q", r.name, p.name, c.name, res, diag.E_SNAPSHOT_MALFORMED, c.want)
				}
			}
		}
	}
}

// Two member names of invalid bytes would both read as U+FFFD, one repeated
// member. The text rule refuses the first before any name compares.
func TestReaders_RefuseInvalidMemberNamesAsTextNotAsARepeat(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	doc, s := textDocument(t)
	for name, members := range map[string]string{
		"raw bytes":        "\"k\xff\":1,\"k\xfe\":2",
		"surrogate halves": `"k\ud800":1,"k\ud801":2`,
	} {
		edited := []byte(editOnce(t, doc, `"body":"BODY"`, members+`,"body":"BODY"`))
		for _, r := range wholeDocumentReaders {
			res := r.read(ctx, edited, s)
			if res.Len() != 1 || !res.HasCode(diag.E_SNAPSHOT_MALFORMED) || !strings.Contains(res.String(), "the string at byte") {
				t.Errorf("%s, %s: %s, want one %s refusing the text", r.name, name, res, diag.E_SNAPSHOT_MALFORMED)
			}
			if strings.Contains(res.String(), "repeats the member") {
				t.Errorf("%s, %s: refused as a repeat: %s", r.name, name, res)
			}
		}
	}
}

// readableText is each spelling of text the rule leaves alone, and the text it
// reads back as.
var readableText = []struct{ name, spelling, want string }{
	{"a pair as two escapes", uEscape("d83d") + uEscape("de00"), "\U0001F600"},
	{"a pair as two escapes in upper case", uEscape("D83D") + uEscape("DE00"), "\U0001F600"},
	{"a pair raw", "\U0001F600", "\U0001F600"},
	{"U+FFFD raw", "�", "�"},
	{"U+FFFD as an escape", uEscape("fffd"), "\xef\xbf\xbd"},
	{"an escaped backslash before the text of a surrogate", `\\ud800`, `\ud800`},
	{"text around a pair", "a" + uEscape("d83d") + uEscape("de00") + "b", "a\U0001F600b"},
}

// Valid text reads back unchanged at every position a reader returns, however the
// document spells it, and survives a second write. UpdateMetadata keeps the body's
// bytes.
func TestReaders_ReadValidTextBackUnchanged(t *testing.T) {
	t.Parallel()
	base, s := textDocument(t)
	note, ok := s.Type("Note")
	if !ok {
		t.Fatal("no type Note")
	}
	for _, c := range readableText {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			doc := base
			doc = editOnce(t, doc, `"run":"META"`, `"run":"`+c.spelling+`"`)
			doc = editOnce(t, doc, `"key":["KEY"]`, `"key":["`+c.spelling+`"]`)
			doc = editOnce(t, doc, `"id":"KEY"`, `"id":"`+c.spelling+`"`)
			doc = editOnce(t, doc, `"body":"BODY"`, `"body":"`+c.spelling+`"`)
			doc = editOnce(t, doc, `"tags":["ELEMENT"]`, `"tags":["`+c.spelling+`"]`)
			data := []byte(doc)

			for _, r := range wholeDocumentReaders {
				if res := r.read(ctx, data, s); res.HasErrors() {
					t.Errorf("%s: %s", r.name, res)
				}
			}

			loaded := loadTextNote(t, data, s, note.ID(), c.want)
			written, res := snapshot.Marshal(ctx, loaded)
			if res.HasErrors() {
				t.Fatalf("Marshal: %s", res)
			}
			loadTextNote(t, written, s, note.ID(), c.want)

			info, res := snapshot.Info(ctx, data)
			if res.HasErrors() {
				t.Fatalf("Info: %s", res)
			}
			if got := info.Metadata["run"]; got != c.want {
				t.Errorf("Info metadata = %q, want %q", got, c.want)
			}
			header, res := snapshot.HeaderOnly(ctx, data)
			if res.HasErrors() {
				t.Fatalf("HeaderOnly: %s", res)
			}
			if got := header.Metadata["run"]; got != c.want {
				t.Errorf("HeaderOnly metadata = %q, want %q", got, c.want)
			}

			updated, res := snapshot.UpdateMetadata(ctx, data, map[string]string{"run": c.want})
			if res.HasErrors() {
				t.Fatalf("UpdateMetadata: %s", res)
			}
			if !strings.Contains(string(updated), `"body":"`+c.spelling+`"`) {
				t.Errorf("UpdateMetadata changed the body's spelling: %s", updated)
			}
			loadTextNote(t, updated, s, note.ID(), c.want)
			after, res := snapshot.HeaderOnly(ctx, updated)
			if res.HasErrors() {
				t.Fatalf("HeaderOnly after UpdateMetadata: %s", res)
			}
			if got := after.Metadata["run"]; got != c.want {
				t.Errorf("metadata after UpdateMetadata = %q, want %q", got, c.want)
			}
		})
	}
}

// loadTextNote loads doc and requires want at the Note's key, id, body and list
// element.
func loadTextNote(t *testing.T, doc []byte, s schema.Schema, id schema.TypeID, want string) graph.Snapshot {
	t.Helper()
	snap, res := snapshot.Load(t.Context(), doc, s)
	if res.HasErrors() {
		t.Fatalf("Load: %s", res)
	}
	notes := slices.Collect(snap.InstancesOf(id))
	if len(notes) != 1 {
		t.Fatalf("Load read %d notes, want 1", len(notes))
	}
	if got := notes[0].PrimaryKey().Clone(); !slices.Equal(got, []any{any(want)}) {
		t.Errorf("key = %q, want %q", got, want)
	}
	props := notes[0].Properties().Clone()
	if props["id"] != want || props["body"] != want {
		t.Errorf("properties = %q, want id and body %q", props, want)
	}
	if tags, ok := props["tags"].([]any); !ok || !slices.Equal(tags, []any{any(want)}) {
		t.Errorf("tags = %q, want one element %q", props["tags"], want)
	}
	return snap
}

// headerReaders are the surfaces that read only up to the end of the types table.
var headerReaders = []struct {
	name string
	read func(t *testing.T, doc []byte) (*snapshot.HeaderInfo, diag.Result)
}{
	{"HeaderOnlyRead", func(t *testing.T, doc []byte) (*snapshot.HeaderInfo, diag.Result) {
		t.Helper()
		return snapshot.HeaderOnlyRead(t.Context(), strings.NewReader(string(doc)))
	}},
	{"ScanDir", func(t *testing.T, doc []byte) (*snapshot.HeaderInfo, diag.Result) {
		t.Helper()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "doc.ys"), doc, 0o600); err != nil {
			t.Fatalf("write the document: %v", err)
		}
		entries, err := collectScanDir(t.Context(), dir)
		if err != nil {
			t.Fatalf("ScanDir: %v", err)
		}
		if len(entries) != 1 {
			t.Fatalf("ScanDir yielded %d entries, want 1", len(entries))
		}
		return entries[0].Header, entries[0].Result
	}},
}

// A reader of the header holds the text it reads to the same rule: a string up to
// the end of the types table.
func TestHeaderReaders_RefuseBadTextUpToTheEndOfTheTypes(t *testing.T) {
	t.Parallel()
	doc, _ := textDocument(t)
	for _, r := range headerReaders {
		for _, p := range []textPosition{metadataValue, typesRow} {
			for _, c := range unreadableText {
				info, res := r.read(t, p.edit(t, doc, c.spelling))
				if info != nil || res.Len() != 1 || !res.HasCode(diag.E_SNAPSHOT_MALFORMED) || !strings.Contains(res.String(), c.want) {
					t.Errorf("%s, %s, %s: info %v, %s; want none and one %s naming %q", r.name, p.name, c.name, info != nil, res, diag.E_SNAPSHOT_MALFORMED, c.want)
				}
			}
		}
	}
}

// HeaderOnlyRead and ScanDir stop at the types table, so bad text past it goes
// unread, as their documentation states. HeaderOnly holds the whole document and
// refuses the same bytes.
func TestHeaderReaders_LeaveBadTextPastTheTypesUnread(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	doc, _ := textDocument(t)
	for _, r := range headerReaders {
		for _, p := range pastTheTypes {
			for _, c := range unreadableText {
				edited := p.edit(t, doc, c.spelling)
				info, res := r.read(t, edited)
				if res.HasErrors() || info == nil {
					t.Errorf("%s, %s, %s: info %v, %s; want a header", r.name, p.name, c.name, info != nil, res)
				}
				if _, whole := snapshot.HeaderOnly(ctx, edited); !whole.HasCode(diag.E_SNAPSHOT_MALFORMED) {
					t.Errorf("HeaderOnly, %s, %s: %s, want %s", p.name, c.name, whole, diag.E_SNAPSHOT_MALFORMED)
				}
			}
		}
	}
}

// Valid text in the header and the types table reads back unchanged.
func TestHeaderReaders_ReadValidTextBackUnchanged(t *testing.T) {
	t.Parallel()
	doc, _ := textDocument(t)
	for _, r := range headerReaders {
		for _, c := range readableText {
			edited := metadataValue.edit(t, doc, c.spelling)
			edited = typesRow.edit(t, string(edited), c.spelling)
			info, res := r.read(t, edited)
			if res.HasErrors() || info == nil {
				t.Errorf("%s, %s: info %v, %s; want a header", r.name, c.name, info != nil, res)
				continue
			}
			if !maps.Equal(info.Metadata, map[string]string{"run": c.want}) {
				t.Errorf("%s, %s: metadata = %q, want %q", r.name, c.name, info.Metadata, c.want)
			}
			if !slices.ContainsFunc(info.Types, func(ref snapshot.TypeRef) bool { return ref.Name == "Note"+c.want }) {
				t.Errorf("%s, %s: types = %q, want the name %q", r.name, c.name, info.Types, "Note"+c.want)
			}
		}
	}
}

// A whole-document reader holds the document to its text before it reads the
// header, so invalid text under an unsupported version is malformed. A reader of
// the header alone judges the version first.
func TestReaders_JudgeTextBeforeVersionOverTheWholeDocument(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	doc, s := textDocument(t)
	doc = editOnce(t, doc, `"version":`+strconv.Itoa(snapshot.MinReadableVersion), `"version":`+strconv.Itoa(snapshot.MinReadableVersion-1))
	onlyVersion := []byte(doc)
	badText := []byte(editOnce(t, doc, `"run":"META"`, "\"run\":\"\xff\""))
	for _, r := range wholeDocumentReaders {
		if res := r.read(ctx, badText, s); res.Len() != 1 || !res.HasCode(diag.E_SNAPSHOT_MALFORMED) {
			t.Errorf("%s, bad text under an unsupported version: %s, want one %s", r.name, res, diag.E_SNAPSHOT_MALFORMED)
		}
		if res := r.read(ctx, onlyVersion, s); !res.HasCode(diag.E_SNAPSHOT_UNSUPPORTED_VERSION) {
			t.Errorf("%s, an unsupported version alone: %s, want %s", r.name, res, diag.E_SNAPSHOT_UNSUPPORTED_VERSION)
		}
	}
	_, res := snapshot.HeaderOnlyRead(ctx, bytes.NewReader(badText))
	if res.Len() != 1 || !res.HasCode(diag.E_SNAPSHOT_UNSUPPORTED_VERSION) {
		t.Errorf("HeaderOnlyRead: %s, want one %s", res, diag.E_SNAPSHOT_UNSUPPORTED_VERSION)
	}
}
