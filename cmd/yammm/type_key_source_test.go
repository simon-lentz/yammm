package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/simon-lentz/yammm/location"
)

const typeKeySchema = `schema "t"

abstract type Base {
    id String primary
}

type Thing extends Base {
    n Integer
}
`

// TestDataCommands_AnchorATypeIssueAtTheFileThatStatesTheType pins where a
// diagnostic about a type name stands: at the root of the first data file whose
// key states the type, whether the type's array is empty, holds only elements
// the parser refused, or follows an earlier file's refused elements.
func TestDataCommands_AnchorATypeIssueAtTheFileThatStatesTheType(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		files []string // each file's body, in operand order
		code  string
	}{
		{"unknown type, every element refused", []string{`{"Nope":[{"id":"\ud800"}]}`}, "E_INSTANCE_TYPE_NOT_FOUND"},
		{"unknown type, empty array", []string{`{"Nope":[]}`}, "E_INSTANCE_TYPE_NOT_FOUND"},
		{"abstract type, every element refused", []string{`{"Base":[{"id":"\ud800"}]}`}, "E_ABSTRACT_TYPE"},
		{"abstract type, empty array", []string{`{"Base":[]}`}, "E_ABSTRACT_TYPE"},
		{"unknown type, the first file's elements refused", []string{`{"Nope":[{"id":"\ud800"}]}`, `{"Nope":[{"id":"x"}]}`}, "E_INSTANCE_TYPE_NOT_FOUND"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			schemaPath := filepath.Join(dir, "t.yammm")
			if err := os.WriteFile(schemaPath, []byte(typeKeySchema), 0o600); err != nil {
				t.Fatal(err)
			}
			args := []string{"check", "--json", schemaPath}
			for i, body := range tc.files {
				p := filepath.Join(dir, string(rune('a'+i))+".json")
				if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
				args = append(args, p)
			}
			first, err := location.SourceIDFromPath(args[3])
			if err != nil {
				t.Fatal(err)
			}

			_, _, errOut := executeCmdOutput(t, args...)
			var doc struct {
				Issues []struct {
					Code       string          `json:"code"`
					SourceName string          `json:"sourceName"`
					Path       string          `json:"path"`
					Span       json.RawMessage `json:"span"`
				} `json:"issues"`
			}
			if err := json.Unmarshal([]byte(errOut), &doc); err != nil {
				t.Fatalf("decode the diagnostic document: %v\n%s", err, errOut)
			}
			found := 0
			for _, iss := range doc.Issues {
				if iss.Code != tc.code {
					continue
				}
				found++
				if iss.SourceName != first.String() || iss.Path != "$" || len(iss.Span) != 0 {
					t.Errorf("%s at source %q, path %q, span %s; want %q, \"$\", no span",
						iss.Code, iss.SourceName, iss.Path, iss.Span, first.String())
				}
			}
			if found != 1 {
				t.Errorf("%d %s issues; want 1\n%s", found, tc.code, errOut)
			}
		})
	}
}
