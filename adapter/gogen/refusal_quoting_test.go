package gogen_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	"github.com/simon-lentz/yammm/adapter/gogen"
	"github.com/simon-lentz/yammm/schema"
)

// unprintableRoot returns a module root whose name holds a no-break space, a
// character that does not print, so every source identity under it holds one.
func unprintableRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "r\u00a0s")
	if err := os.Mkdir(root, 0o750); err != nil {
		t.Fatal(err)
	}
	return root
}

// wantQuotedRefusal holds a Marshal refusal to its text and to every rune of
// it printing: a source named raw would carry the root's no-break space.
func wantQuotedRefusal(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("Marshal succeeded, want the refusal %q", want)
	}
	msg := err.Error()
	if !strings.Contains(msg, want) {
		t.Errorf("the refusal %q does not say %q", msg, want)
	}
	for _, r := range msg {
		if !unicode.IsPrint(r) {
			t.Errorf("the refusal holds %U unescaped: %q", r, msg)
		}
	}
}

// TestMarshal_EveryRefusalQuotesTheSourcesItNames pins each refusal that names
// a source or an importer of a schema loaded from files, other than the key
// rule's, which TestMarshal_AnUnprintableIdentityIsNamedQuoted pins: a source
// under two keys, two sources under one key, an import whose key the rule
// refuses, and a source with no key under the root.
// TestMarshal_LoadStringKeysByBaseName pins a LoadString name that names no
// file, and a loaded schema always holds its sources' content. Each source sits under a root whose name
// does not print.
func TestMarshal_EveryRefusalQuotesTheSourcesItNames(t *testing.T) {
	t.Parallel()
	t.Run("a source under two keys", func(t *testing.T) {
		t.Parallel()
		root := unprintableRoot(t)
		writeTree(t, root, map[string]string{
			"main.yammm":     "schema \"main\"\n\nimport \"a\" as a\nimport \"b\" as b\n\ntype M {\n\tid String primary\n\t--> TO_A (one) a.A\n\t--> TO_B (one) b.B\n}\n",
			"a.yammm":        "schema \"a\"\n\nimport \"lib/dep\" as dep\n\ntype A {\n\tid String primary\n\t--> TO_D (one) dep.D\n}\n",
			"b.yammm":        "schema \"b\"\n\nimport \"real/dep\" as dep\n\ntype B {\n\tid String primary\n\t--> TO_D (one) dep.D\n}\n",
			"real/dep.yammm": "schema \"dep\"\n\ntype D {\n\tid String primary\n}\n",
		})
		if err := os.Symlink("real", filepath.Join(root, "lib")); err != nil {
			t.Skipf("symlinks not supported: %v", err)
		}
		_, err := gogen.Marshal(loadEntry(t, root, "main.yammm"))
		wantQuotedRefusal(t, err, "under one key")
	})
	t.Run("two sources under one key", func(t *testing.T) {
		t.Parallel()
		root := unprintableRoot(t)
		writeTree(t, root, map[string]string{
			"main.yammm":        "schema \"main\"\n\nimport \"dep\" as dep\nimport \"lib/x\" as x\n\ntype M {\n\tid String primary\n\t--> TO_D (one) dep.D\n\t--> TO_X (one) x.X\n}\n",
			"dep.yammm":         "schema \"dep\"\n\ntype D {\n\tid String primary\n}\n",
			"deep/real/x.yammm": "schema \"x\"\n\nimport \"../dep\" as other\n\ntype X {\n\tid String primary\n\t--> TO_O (one) other.O\n}\n",
			"deep/dep.yammm":    "schema \"other\"\n\ntype O {\n\tid String primary\n}\n",
		})
		if err := os.Symlink(filepath.Join("deep", "real"), filepath.Join(root, "lib")); err != nil {
			t.Skipf("symlinks not supported: %v", err)
		}
		_, err := gogen.Marshal(loadEntry(t, root, "main.yammm"))
		wantQuotedRefusal(t, err, "both take the embedded key")
	})
	t.Run("an import whose key the rule refuses", func(t *testing.T) {
		t.Parallel()
		root := unprintableRoot(t)
		writeTree(t, root, map[string]string{
			"main.yammm":     "schema \"main\"\n\nimport \"./x\u00a0y\" as dep\n\ntype M {\n\tid String primary\n\t--> TO_D (one) dep.D\n}\n",
			"x\u00a0y.yammm": "schema \"dep\"\n\ntype D {\n\tid String primary\n}\n",
		})
		_, err := gogen.Marshal(loadEntry(t, root, "main.yammm"))
		wantQuotedRefusal(t, err, "gogen: import")
	})
	t.Run("a source with no key under the root", func(t *testing.T) {
		t.Parallel()
		root := unprintableRoot(t)
		s, res := schema.LoadSourcesWithEntry(context.Background(),
			map[string][]byte{".": []byte("schema \"p\"\n\ntype P {\n\tid String primary\n}\n")}, ".", root, schema.WithSourcesOnly(true))
		if res.HasErrors() {
			t.Fatalf("load: %v", res.Err())
		}
		_, err := gogen.Marshal(s)
		wantQuotedRefusal(t, err, "has no key relative to the schema's root")
	})
}

// TestMarshal_ABuildLineEndingInAFormFeedEndsInASpace pins the bytes of a
// build-line doc that ends in a form feed: the form feed is written as a
// space, inside the block comment, as every other form feed in such a line is.
func TestMarshal_ABuildLineEndingInAFormFeedEndsInASpace(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(context.Background(), "schema \"g\"\n\n/* +build x\f\n   tail. */\ntype Car {\n\tid String primary\n}\n", "gen.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}
	out, err := gogen.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "/*+build x */\n") {
		t.Errorf("the build line is not written as /*+build x */:\n%s", out)
	}
}
