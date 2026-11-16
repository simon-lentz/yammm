package mapping

import "testing"

func TestSanitize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"book_catalog", "book_catalog"},
		{"geo-regions", "geo_regions"},
		{"foo.bar", "foo_bar"},
		{"foo/bar", "foo_bar"},
		{"foo\\bar", "foo_bar"},
		{"123abc", "_123abc"},
		{" spaced ", "spaced"},
		{"", ""},
		{"café", "caf"},
		{"___", "___"},
		{"Hello World", "Hello_World"},
		{"a-b.c/d\\e", "a_b_c_d_e"},
		{"9lives", "_9lives"},
		{"_private", "_private"},
		{"ALLCAPS", "ALLCAPS"},
		{"mixed123_OK", "mixed123_OK"},
		{"emoji🎉gone", "emojigone"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			if got := sanitize(tt.input); got != tt.want {
				t.Errorf("sanitize(%q) = %q; want %q", tt.input, got, tt.want)
			}
		})
	}
}
