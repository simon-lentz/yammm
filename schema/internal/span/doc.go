// Package span provides span building utilities for schema parsing.
//
// It handles ANTLR token position conversion to [location.Span] values,
// including byte offset computation via [location.RuneOffsetConverter] and
// line/column resolution via [location.PositionRegistry].
//
// This is an internal package; its API may change without notice.
package span
