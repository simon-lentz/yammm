package main

import "testing"

// pendingRepairs names each instrument case that fails on the current tree
// and the defect it observes. A listed case must keep failing, so a repair
// turns it red until its entry is deleted; an unlisted failure is a regression.
var pendingRepairs = map[string]string{
	"introspect: prefixed graph with the schema filter":                                "the prefix is folded into the schema component of every label, so the filter matches nothing and an unfiltered scaffold names the prefixed component",
	"introspect: prefixed graph without a filter":                                      "the prefix is folded into the schema component of every label, so the filter matches nothing and an unfiltered scaffold names the prefixed component",
	"introspect: a filter the label sanitizes":                                         "the raw filter is compared against the sanitized label component",
	"introspect: a filter matching nothing":                                            "a filter that matched none of the constraints read scaffolds an empty schema silently",
	"snapshot save: the summary's type count is the type table's":                      "the count is the types holding root instances, not the written type table",
	"snapshot save: the Long text states what --into carries":                          "the Long text says no created_at is written by default",
	"label flags: export --to cypher, a prefix that is not identifier text":            "the label is refused only after the schema loads, as a validation failure",
	"label flags: export --to cypher, a separator that is not identifier text":         "the label is refused only after the schema loads, as a validation failure",
	"label flags: export --to cypher, an empty separator":                              "the flag is accepted and composes labels that cannot be split back",
	"label flags: neo4j constraints, a prefix that is not identifier text":             "the label is refused only after the schema loads, as a validation failure",
	"label flags: neo4j constraints, a separator that is not identifier text":          "the label is refused only after the schema loads, as a validation failure",
	"label flags: neo4j constraints, an empty separator":                               "the flag is accepted and composes labels that cannot be split back",
	"label flags: neo4j diff, a prefix that is not identifier text":                    "the label is refused only after the schema loads, as a validation failure",
	"label flags: neo4j diff, a separator that is not identifier text":                 "the label is refused only after the schema loads, as a validation failure",
	"label flags: neo4j diff, an empty separator":                                      "the flags are checked only after a connection is attempted",
	"label flags: neo4j indexes, a prefix that is not identifier text":                 "the label is refused only after the schema loads, as a validation failure",
	"label flags: neo4j indexes, a separator that is not identifier text":              "the label is refused only after the schema loads, as a validation failure",
	"label flags: neo4j indexes, an empty separator":                                   "the flag is accepted and composes labels that cannot be split back",
	"label flags: neo4j introspect, a prefix that is not identifier text":              "the flags are checked only after a connection is attempted",
	"label flags: neo4j introspect, a separator that is not identifier text":           "the flags are checked only after a connection is attempted",
	"label flags: neo4j introspect, an empty separator":                                "the flags are checked only after a connection is attempted",
	"neo4j diff: a warning added after the pre-payload render, json":                   "a result added after the first render is written nowhere",
	"neo4j diff: a warning added after the pre-payload render, text":                   "a result added after the first render is written nowhere",
	"one JSON document: check, csv without --type":                                     "the failure is printed as prose beside the document",
	"one JSON document: check, undetectable data format":                               "the failure is printed as prose beside the document",
	"one JSON document: check, unreadable data":                                        "the failure is printed as prose beside the document",
	"one JSON document: check, wrong arity":                                            "the failure is printed as prose and no document is written",
	"one JSON document: export, contradictory destinations":                            "the failure is printed as prose and no document is written",
	"one JSON document: export, invalid label prefix":                                  "the failure is printed as prose and no document is written",
	"one JSON document: export, unreadable data":                                       "the failure is printed as prose beside the document",
	"one JSON document: export, unsupported target":                                    "the failure is printed as prose and no document is written",
	"one JSON document: export, unwritable output":                                     "the failure is printed as prose beside the document",
	"one JSON document: fmt, syntax error":                                             "the failure is printed as prose and no document is written",
	"one JSON document: fmt, unreadable path":                                          "the failure is printed as prose and no document is written",
	"one JSON document: gen, unsupported target":                                       "the failure is printed as prose and no document is written",
	"one JSON document: load, unreadable data":                                         "the failure is printed as prose beside the document",
	"one JSON document: neo4j constraints, unrecognized edition":                       "the failure is printed as prose beside the document",
	"one JSON document: neo4j diff, no --uri":                                          "the failure is printed as prose and no document is written",
	"one JSON document: neo4j introspect, no --uri":                                    "the failure is printed as prose and no document is written",
	"one JSON document: neo4j introspect, unreachable server":                          "the failure is printed as prose and no document is written",
	"one JSON document: snapshot info, no argument":                                    "the failure is printed as prose and no document is written",
	"one JSON document: snapshot info, unreadable file":                                "the failure is printed as prose and no document is written",
	"one JSON document: snapshot save, no destination":                                 "the failure is printed as prose and no document is written",
	"one JSON document: snapshot save, undetectable data format":                       "the failure is printed as prose beside the document",
	"one JSON document: snapshot update-metadata, no operation":                        "the failure is printed as prose and no document is written",
	"one JSON document: snapshot update-metadata, unreadable file":                     "the failure is printed as prose and no document is written",
	"snapshot info: a directory entry carries the diagnostic wire":                     "the entry carries a three-field issue projection under issues, with no truncation state",
	"snapshot info: a warned directory row names its warning":                          "the row is marked warn and names no code",
	"snapshot info: absent metadata renders as an object, --format json":               "a nil map marshals as null",
	"snapshot info: absent metadata renders as an object, --header-only --format json": "a nil map marshals as null",
	"snapshot info: header-only text reports the file size":                            "the text renderer reads the library struct, which carries no file size",
	"snapshot save: a failed write raises no extension warning":                        "the warning is raised before the write runs",
	"snapshot save: the extension warning names the output path":                       "the warning carries no location and renders <unknown>",
}

// checkRepairState reports a case against pendingRepairs. failure is empty
// when the case passed.
func checkRepairState(t *testing.T, key, failure string) {
	t.Helper()
	defect, pinned := pendingRepairs[key]
	switch {
	case pinned && failure == "":
		t.Errorf("%s: the pinned defect (%s) no longer reproduces; delete its pendingRepairs entry", key, defect)
	case pinned:
		t.Logf("%s: pinned defect reproduces (%s): %s", key, defect, failure)
	case failure != "":
		t.Error(failure)
	}
}
