// Package refusal marks a writer's refusal with the class a caller matches.
//
// The data adapters' writers report through an error, not a diag.Result, so a
// caller separates a refusal — "this snapshot cannot be written in this
// format", or "this adapter holds a setting it cannot use" — from an encoding
// failure and from an I/O failure with errors.Is against a sentinel the
// adapter exports. A refusal carries that sentinel as its class and the site's
// own text as its message, and marking never changes the text. That text, or
// the wraps the writer adds around it, names what was refused: an instance, a
// property, a column, an edge or the setting.
//
// The sentinels stay in the adapter packages, where a caller imports them. This
// package holds the one implementation both adapters mark through, so the two
// cannot drift.
//
// This is an internal package; its API may change without notice.
package refusal
