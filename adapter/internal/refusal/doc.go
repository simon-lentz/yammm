// Package refusal marks a writer's refusal with the class a caller matches.
//
// The data adapters' writers report through an error, not a diag.Result, so a
// caller separates "this snapshot cannot be written in this format" from an
// encoding failure and from an I/O failure with errors.Is against a sentinel
// the adapter exports. A refusal carries that sentinel as its class and the
// site's own text as its message: the text names the instance, the column or
// the edge, and marking never changes it.
//
// The sentinels stay in the adapter packages, where a caller imports them. This
// package holds the one implementation both adapters mark through, so the two
// cannot drift.
//
// This is an internal package; its API may change without notice.
package refusal
