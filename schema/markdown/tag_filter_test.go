package markdown

import "testing"

// TestTagFilter pins the filter to what cmark-gfm 0.29.0.gfm.13 escapes, read
// from that program for each of these tags inside an HTML block.
func TestTagFilter(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		tag      string
		filtered bool
	}{
		{"<style>", true},
		{"<style x>", true},
		{"<style\tx>", true},
		{"<style\nx>", true},
		{"<style\rx>", true},
		{"<style\vx>", false},
		{"<style\fx>", false},
		{"<style/>", true},
		{"<style / >", true},
		{"<style/x>", false},
		{"<style", true},
		{"<STYLE>", true},
		{"<sTyLe >", true},
		{"<\u017ftyle>", false},
		{"<\u212abd>", false},
		{"</style>", true},
		{"</style x>", true},
		{"</style/>", true},
		{"<//style>", false},
		{"< style>", false},
		{"<stylex>", false},
		{"<style-x>", false},
		{"<style=x>", false},
		{"<style\"x\">", false},
		{"<style\x00>", false},
		{"<title>", true},
		{"<textarea>", true},
		{"<xmp>", true},
		{"<iframe>", true},
		{"<noembed>", true},
		{"<noframes>", true},
		{"<script>", true},
		{"<plaintext>", true},
		{"<noscript>", false},
		{"<template>", false},
		{"<pre>", false},
	} {
		want := tt.tag
		if tt.filtered {
			want = "&lt;" + tt.tag[1:]
		}
		if got := string(tagFilter([]byte(tt.tag))); got != want {
			t.Errorf("tagFilter(%q) = %q, want %q", tt.tag, got, want)
		}
		in := "<p>a</p>\n" + tt.tag + "\n<b>" + tt.tag
		if got, want := string(tagFilter([]byte(in))), "<p>a</p>\n"+want+"\n<b>"+want; got != want {
			t.Errorf("tagFilter(%q) = %q, want %q", in, got, want)
		}
	}
	plain := []byte("<p>no filtered tag</p>")
	if got := tagFilter(plain); &got[0] != &plain[0] {
		t.Error("tagFilter copied a text it changes nothing in")
	}
}
