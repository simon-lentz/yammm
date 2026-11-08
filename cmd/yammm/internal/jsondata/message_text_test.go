package jsondata

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/location"
)

// A message about a name the document writes repeats the name's head alone, as
// a message about a value does: a member or a type tag can run as long as a
// value can.
func TestParse_RepeatsANameCutToItsHead(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("Z", 4096)
	for _, doc := range []string{
		`{"T": [{"id": "a", "` + long + `": 1, "` + long + `": 2}]}`,
		`{"T": [], "` + long + `": [], "` + long + `": []}`,
		`{"` + long + `!": []}`,
	} {
		_, r := Parse(t.Context(), location.NewSourceID("test://names.json"), []byte(doc))
		if r.OK() {
			t.Errorf("Parse accepted %.40s", doc)
		}
		for issue := range r.Issues() {
			msg := issue.Message()
			if !utf8.ValidString(msg) {
				t.Errorf("the message is not valid UTF-8: %.200q", msg)
			}
			if strings.Contains(msg, strings.Repeat("Z", 33)) {
				t.Errorf("the message repeats more than 32 bytes of a name: %.200q", msg)
			}
			for _, d := range issue.Details() {
				if !utf8.ValidString(d.Value) {
					t.Errorf("detail %s is not valid UTF-8: %.200q", d.Key, d.Value)
				}
			}
		}
	}
}
