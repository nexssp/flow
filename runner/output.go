package runner

// OutputSuppressed marks an action result that the runner should not render as
// its final stdout payload. Construct it with SuppressOutput.
type OutputSuppressed struct {
	Value any
}

// SuppressOutput marks a successful action result as not intended for the
// runner's final stdout rendering. The value still becomes the execution
// result supplied to post-run assertions and callers.
func SuppressOutput(value any) OutputSuppressed {
	return OutputSuppressed{Value: value}
}

func unwrapOutput(value any) (any, bool) {
	marked, ok := value.(OutputSuppressed)
	if !ok {
		return value, false
	}
	return marked.Value, true
}
