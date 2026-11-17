package lsp_test

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	lsp "github.com/simon-lentz/yammm/internal/lsp"
)

// TestConfigValidate_ARegularFileWarnsHoweverItIsSpelled holds a module root
// that names a regular file to one answer, a warning, whether it is spelled
// with a trailing separator or a "." element or not: each spelling can name a
// directory once the file is replaced by one.
func TestConfigValidate_ARegularFileWarnsHoweverItIsSpelled(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	sep := string(filepath.Separator)
	var resolved []string
	for _, typed := range []string{file, file + sep, file + sep + "."} {
		var log bytes.Buffer
		cfg := lsp.Config{ModuleRoot: typed}
		if err := cfg.Validate(slog.New(slog.NewTextHandler(&log, nil))); err != nil {
			t.Errorf("Validate(%q) = %v, want a warning", typed, err)
			continue
		}
		if !strings.Contains(log.String(), "module root is not a directory") {
			t.Errorf("Validate(%q) logged %q, want the not-a-directory warning", typed, log.String())
		}
		resolved = append(resolved, cfg.ModuleRoot)
	}
	for _, r := range resolved[1:] {
		if r != resolved[0] {
			t.Errorf("one file resolved as %q and as %q", resolved[0], r)
		}
	}
}
