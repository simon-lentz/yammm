package diag

// Collect returns a Result containing the given issues. It is the diagnostic
// equivalent of [errors.Join]: the common case where a caller already holds one
// or more built Issue values and wants a Result to return from a
// (T, Result)-shaped function.
//
//	return nil, diag.Collect(diag.NewIssue(diag.Error, diag.E_INTERNAL, "…").Build())
//
// Issues are validated (a zero or invalid Issue panics, matching
// [Collector.Collect]) and sorted by the same total order [Collector.Result]
// produces. Use a [Collector] for incremental or concurrent accumulation;
// Collect is for the terminal, one-shot construction case.
func Collect(issues ...Issue) Result {
	c := NewCollectorUnlimited()
	c.CollectAll(issues)
	return c.Result()
}
