package ttyproxy

// OutputError marks a fatal failure forwarding a proxied child's output to the
// host's stdout. The original sink error is preserved for classification and
// reporting by the launch caller.
type OutputError struct {
	Err error
}

func (e *OutputError) Error() string { return "tty proxy output: " + e.Err.Error() }

func (e *OutputError) Unwrap() error { return e.Err }
