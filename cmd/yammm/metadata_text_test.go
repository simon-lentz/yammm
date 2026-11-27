package main

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/snapshot"
)

// TestMetadataFlags_TextIsJudgedBeforeAnyRead pins that a metadata key or
// value that is not valid UTF-8, given to -m, --set or --unset, and an empty
// -m or --set pair, is a usage error judged before the command reads a file:
// the exit is 2 whether the schema or the snapshot exists, and nothing is
// written.
func TestMetadataFlags_TextIsJudgedBeforeAnyRead(t *testing.T) {
	dir := t.TempDir()
	fixture := createYSFixtureWithMetadata(t, dir)
	before, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.ys")
	missing := filepath.Join(dir, "missing")

	tests := []struct {
		name string
		args []string
		want string
	}{
		{"save, a key", []string{"snapshot", "save", "-m", "k\xff=v", "-o", out, "testdata/valid.yammm", "testdata/data.json"}, `invalid metadata "k\xff=v": key is not valid UTF-8`},
		{"save, a value", []string{"snapshot", "save", "-m", "k=\xff", "-o", out, "testdata/valid.yammm", "testdata/data.json"}, `invalid metadata "k=\xff": value is not valid UTF-8`},
		{"save, beside a schema that does not exist", []string{"snapshot", "save", "-m", "k=\xff", "-o", out, missing + ".yammm", "testdata/data.json"}, "value is not valid UTF-8"},
		{"update-metadata --set, a key", []string{"snapshot", "update-metadata", "--set", "k\xff=v", fixture}, `invalid metadata "k\xff=v": key is not valid UTF-8`},
		{"update-metadata --set, a value", []string{"snapshot", "update-metadata", "--set", "k=\xff", fixture}, "value is not valid UTF-8"},
		{"update-metadata --unset", []string{"snapshot", "update-metadata", "--unset", "k\xff", fixture}, `invalid --unset "k\xff": key is not valid UTF-8`},
		{"update-metadata --unset, a snapshot that does not exist", []string{"snapshot", "update-metadata", "--unset", "k\xff", missing + ".ys"}, "key is not valid UTF-8"},
		{"update-metadata, a second --unset", []string{"snapshot", "update-metadata", "--unset", "ok", "--unset", "k\xff", fixture}, `invalid --unset "k\xff"`},
		{"save, an empty -m", []string{"snapshot", "save", "-m", "", "-o", out, "testdata/valid.yammm", "testdata/data.json"}, `invalid metadata "": expected key=value format`},
		{"update-metadata, an empty --set", []string{"snapshot", "update-metadata", "--set", "", fixture}, `invalid metadata "": expected key=value format`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := runCLI(t, tt.args...)
			if code != cli.ExitUsage {
				t.Errorf("exit = %d, want %d; stderr:\n%s", code, cli.ExitUsage, stderr)
			}
			if !strings.Contains(stderr, tt.want) {
				t.Errorf("stderr lacks %q:\n%s", tt.want, stderr)
			}
			if strings.Contains(stderr, "[E_") {
				t.Errorf("a diagnostic was drawn, so a file was read:\n%s", stderr)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want nothing", stdout)
			}
			if _, err := os.Lstat(out); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("a refused save left a file at the output path: %v", err)
			}
			if after, err := os.ReadFile(fixture); err != nil || !bytes.Equal(after, before) {
				t.Errorf("a refused update-metadata changed the snapshot (read error %v)", err)
			}
		})
	}
}

// TestMetadataFlags_StoreTheValueAsTyped pins that a metadata value reaches the
// header byte for byte: a CR LF inside it stays a CR LF.
func TestMetadataFlags_StoreTheValueAsTyped(t *testing.T) {
	const value = "a\r\nb"
	dir := t.TempDir()
	out := filepath.Join(dir, "out.ys")
	if code, _, stderr := runCLI(t, "snapshot", "save", "-m", "k="+value, "-o", out, "testdata/valid.yammm", "testdata/data.json"); code != cli.ExitOK {
		t.Fatalf("save: exit = %d; stderr:\n%s", code, stderr)
	}
	assertMetadataValue(t, out, "save -m", value)

	fixture := createYSFixtureWithMetadata(t, dir)
	if code, _, stderr := runCLI(t, "snapshot", "update-metadata", "--set", "k="+value, fixture); code != cli.ExitOK {
		t.Fatalf("update-metadata: exit = %d; stderr:\n%s", code, stderr)
	}
	assertMetadataValue(t, fixture, "update-metadata --set", value)
}

// TestMetadataFlags_UnsetRemovesTheEmptyKey pins that --unset "" names the
// empty key, which the library writes and reads, and removes it.
func TestMetadataFlags_UnsetRemovesTheEmptyKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty_key.ys")
	data, err := os.ReadFile(createYSFixtureWithMetadata(t, filepath.Dir(path)))
	if err != nil {
		t.Fatal(err)
	}
	updated, err := snapshot.UpdateMetadata(t.Context(), data, map[string]string{"": "x", "keep": "1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, updated, 0o600); err != nil {
		t.Fatal(err)
	}

	if code, _, stderr := runCLI(t, "snapshot", "update-metadata", "--unset", "", path); code != cli.ExitOK {
		t.Fatalf("exit = %d; stderr:\n%s", code, stderr)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	header, err := snapshot.ReadHeader(t.Context(), bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := header.Metadata[""]; ok || header.Metadata["keep"] != "1" {
		t.Errorf("metadata = %q, want the empty key removed and keep=1 kept", header.Metadata)
	}
}

// assertMetadataValue checks that the header of the .ys file at path holds want
// under the key k.
func assertMetadataValue(t *testing.T, path, flag, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	header, err := snapshot.ReadHeader(t.Context(), bytes.NewReader(data))
	if err != nil {
		t.Fatalf("%s: read the header: %v", flag, err)
	}
	if got := header.Metadata["k"]; got != want {
		t.Errorf("%s: metadata k = %q, want %q", flag, got, want)
	}
}
