package jschema

import "testing"

func TestWithDescription_LeavesItsArgumentUnchanged(t *testing.T) {
	for _, base := range []val{
		object(kv{"type", scalar("string")}, kv{"description", scalar("generated")}),
		object(kv{"type", scalar("string")}),
	} {
		before := normalize(t, base)
		got, err := withDescription(base, "doc")
		if err != nil {
			t.Fatal(err)
		}
		if after := normalize(t, base); after != before {
			t.Errorf("withDescription changed its argument from %s to %s", before, after)
		}
		if desc, _ := got.obj[len(got.obj)-1].V.stringValue(); desc != "doc" && desc != "generated doc" {
			t.Errorf("result description = %q", desc)
		}
	}
}

// A description on a fragment that holds no members would never be rendered.
func TestWithDescription_RefusesANonObject(t *testing.T) {
	for _, v := range []val{array(scalar("a")), scalar(true)} {
		if got, err := withDescription(v, "doc"); err == nil {
			t.Errorf("withDescription(%s) = %s, nil; want an error", renderToString(v), renderToString(got))
		}
	}
}
