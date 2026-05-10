package provider

// RetryableError wraps a provider error and indicates whether the caller may
// safely retry the operation.  The SDK already retries network errors and rate
// limits internally; this type surfaces errors that survive all SDK retries so
// the gRPC layer can decide whether to surface them as transient or terminal.
type RetryableError struct {
	Retryable bool
	Err       error
}

func (e *RetryableError) Error() string { return e.Err.Error() }
func (e *RetryableError) Unwrap() error { return e.Err }
