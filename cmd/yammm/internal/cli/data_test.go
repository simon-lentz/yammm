package cli

import (
	"strings"
	"testing"
)

func TestIsSnapshot(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		doc  string
		want bool
	}{
		"a snapshot":                         {`{"yammm_snapshot":{"version":1},"types":[]}`, true},
		"a snapshot past leading whitespace": {" \n\t" + `{"yammm_snapshot":{}}`, true},
		"a snapshot past 512 bytes of space": {strings.Repeat(" ", 600) + `{"yammm_snapshot":{}}`, true},
		"a snapshot past a byte order mark":  {"\xef\xbb\xbf" + `{"yammm_snapshot":{}}`, true},
		"a snapshot past a line comment":     {"// c\n" + `{"yammm_snapshot":{}}`, true},
		"a snapshot past a block comment":    {`{/* c */"yammm_snapshot":{}}`, true},
		"a snapshot past two comments": {"/* a */ // b\n" + `{ // c
 "yammm_snapshot":{}}`, true},
		"a key past a key":          {`{"types":[],"yammm_snapshot":{}}`, false},
		"a data document":           {`{"Person":[{"id":"a"}]}`, false},
		"an array":                  {`["yammm_snapshot"]`, false},
		"a bare string":             {`"yammm_snapshot"`, false},
		"text":                      {"id,name\na,b\n", false},
		"an unterminated comment":   {`/*{"yammm_snapshot":{}}`, false},
		"a line comment and no end": {`//{"yammm_snapshot":{}}`, false},
		"a second byte order mark":  {"\xef\xbb\xbf\xef\xbb\xbf" + `{"yammm_snapshot":{}}`, false},
		"an escaped key":            {`{"yammm\u005fsnapshot":{}}`, true},
		"nothing":                   {"", false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := IsSnapshot([]byte(c.doc)); got != c.want {
				t.Errorf("IsSnapshot(%q) = %v, want %v", c.doc, got, c.want)
			}
		})
	}
}
