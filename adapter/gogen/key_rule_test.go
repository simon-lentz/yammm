package gogen_test

import (
	"context"
	"errors"
	"path"
	"runtime"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/adapter/gogen"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// keyRuleMain imports its sibling dep.yammm by a relative path, so both the
// entry's key and an import's key are read in one store.
const keyRuleMain = "schema \"main\"\n\nimport \"./dep\" as dep\n\ntype M {\n\tid String primary\n\t--> TO_D (one) dep.D\n}\n"

const keyRuleDep = "schema \"dep\"\n\ntype D {\n\tid String primary\n}\n"

// TestMarshal_KeyTheReloadCannotReadIsRefused pins the refusal of an entry
// whose key the re-load's key rule refuses. A Unix file name may hold a
// backslash, which Windows reads as a separator, and a directory may be named
// like a drive; the key is refused naming the source, not reported as a store
// that fails its own re-load.
func TestMarshal_KeyTheReloadCannotReadIsRefused(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows holds no file name with a backslash and no directory named C:")
	}
	for _, tc := range []struct {
		entry, why string
		is         error
	}{
		{`a\b.yammm`, "holds a backslash", nil},
		{"C:/main.yammm", "must be relative to the synthetic root", location.ErrAbsolutePathSourceID},
	} {
		root := t.TempDir()
		writeTree(t, root, map[string]string{
			tc.entry: keyRuleMain,
			path.Join(path.Dir(tc.entry), "dep.yammm"): keyRuleDep,
		})
		_, err := gogen.Marshal(loadEntry(t, root, tc.entry))
		if err == nil {
			t.Errorf("%s: Marshal succeeded, want the key refusal", tc.entry)
			continue
		}
		msg := err.Error()
		if !strings.Contains(msg, "has no key the re-load can read") || !strings.Contains(msg, tc.why) || !strings.Contains(msg, tc.entry) {
			t.Errorf("%s: Marshal = %v, want the key refusal naming the source and saying %q", tc.entry, err, tc.why)
		}
		if strings.Contains(msg, "does not re-load") {
			t.Errorf("%s: Marshal = %v, reported by the round-trip check rather than refused as a key", tc.entry, err)
		}
		if tc.is != nil && !errors.Is(err, tc.is) {
			t.Errorf("%s: Marshal = %v, want it to wrap %v", tc.entry, err, tc.is)
		}
	}

	// A source nothing imports is keyed under the root as the entry is.
	root := t.TempDir()
	writeTree(t, root, map[string]string{"main.yammm": keyRuleDep, `a\b.yammm`: keyRuleDep})
	s, res := schema.LoadSourcesWithEntry(context.Background(), map[string][]byte{
		"main.yammm": []byte(keyRuleDep), `a\b.yammm`: []byte(keyRuleDep),
	}, "main.yammm", root, schema.WithSourcesOnly(true))
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}
	if _, err := gogen.Marshal(s); err == nil || !strings.Contains(err.Error(), "has no key the re-load can read") || !strings.Contains(err.Error(), "holds a backslash") {
		t.Errorf("Marshal with an unimported a\\b.yammm = %v, want the key refusal", err)
	}
}

// TestMarshal_LoadStringKeyIsInNFC pins the one arm whose key is not already
// normal: LoadString keeps the name it is given, so a decomposed base name is
// written in NFC, the form the re-load registers it under.
func TestMarshal_LoadStringKeyIsInNFC(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(context.Background(), keyRuleDep, "cafe\u0301.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}
	got, err := gogen.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if _, entry := reloadEmitted(t, got, s); entry != "caf\u00e9.yammm" {
		t.Errorf("SerializedEntry = %q, want %q in NFC", entry, "caf\u00e9.yammm")
	}
}

// TestEmbeddedKeys_AgreeWithTheLoadersKeyRule holds gogen's keys to the
// loader's. gogen derives a key from a source's identity on disk and judges it
// by the shared key rule; the loader reads a key as text under a synthetic root.
// Over one set of layouts, gogen writes a key exactly when the loader accepts the
// entry's key as text, and writes the keys the loader registers the entry and
// its import under.
func TestEmbeddedKeys_AgreeWithTheLoadersKeyRule(t *testing.T) {
	t.Parallel()
	layouts := []struct {
		entry    string // the entry's path under the module root, as text
		unixOnly bool   // a name Windows cannot hold
	}{
		{"main.yammm", false},
		{"sub/main.yammm", false},
		{"a/b/main.yammm", false},
		{"cafe\u0301/main.yammm", false},
		{"x y/main.yammm", false},
		{`a\b.yammm`, true},
		{`sub\x/main.yammm`, true},
		{"C:/main.yammm", true},
		{"./C:/main.yammm", true},
		{"\u212a:/main.yammm", true},
		{"x:y/main.yammm", true},
	}
	for _, l := range layouts {
		t.Run(l.entry, func(t *testing.T) {
			t.Parallel()
			if l.unixOnly && runtime.GOOS == "windows" {
				t.Skip("Windows cannot hold this name")
			}
			// The dependency's key is spelled as the entry's is, so the loader
			// reads both through the one rule.
			depKey := l.entry[:strings.LastIndex(l.entry, "/")+1] + "dep.yammm"

			// The verdict is on the entry's key alone, so an import the key
			// breaks cannot hide it.
			_, res := schema.LoadSourcesWithEntry(context.Background(), map[string][]byte{l.entry: []byte(keyRuleDep)},
				l.entry, "", schema.WithSourcesOnly(true), schema.WithSyntheticRoot(consumerRoot))
			loaderAccepts := !res.HasErrors()

			root := t.TempDir()
			writeTree(t, root, map[string]string{l.entry: keyRuleMain, depKey: keyRuleDep})
			disk := loadEntry(t, root, l.entry)
			got, err := gogen.Marshal(disk)
			if gogenAccepts := err == nil; gogenAccepts != loaderAccepts {
				t.Fatalf("gogen accepts %t (err %v), the loader accepts the key as text %t (%v)", gogenAccepts, err, loaderAccepts, res.Err())
			}
			if !loaderAccepts {
				return
			}
			text, res := schema.LoadSourcesWithEntry(context.Background(), map[string][]byte{
				l.entry: []byte(keyRuleMain),
				depKey:  []byte(keyRuleDep),
			}, l.entry, "", schema.WithSourcesOnly(true), schema.WithSyntheticRoot(consumerRoot))
			if res.HasErrors() {
				t.Fatalf("the loader accepts the entry's key but not the layout: %v", res.Err())
			}
			store, entry := reloadEmitted(t, got, disk)
			if want := strings.TrimPrefix(text.SourceID().String(), consumerRoot+"/"); entry != want {
				t.Errorf("gogen keys the entry %q, the loader registers %q under %q", entry, l.entry, want)
			}
			wantDep := strings.TrimPrefix(text.ImportsSlice()[0].ResolvedSourceID().String(), consumerRoot+"/")
			if store[wantDep] == nil {
				t.Errorf("gogen's store %v holds no %q, the key the loader registers the import under", keysOf(store), wantDep)
			}
		})
	}
}
