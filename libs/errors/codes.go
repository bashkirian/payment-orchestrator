package errors

// Standard error codes for the payment orchestrator.
// These codes are used in gRPC status details and HTTP error responses
// to provide consistent, machine-readable error identifiers.
const (
	// Idempotency errors
	CodeIdempotencyConflict = "IDEMPOTENCY_CONFLICT" // Idempotency key reused with different request

	// Validation errors
	CodeInvalidArgument = "INVALID_ARGUMENT" // Request validation failed
	CodeInvalidState    = "INVALID_STATE"    // Operation not allowed in current state
	CodeInvalidUUID     = "INVALID_UUID"     // UUID format is invalid

	// Resource errors
	CodeNotFound = "NOT_FOUND" // Requested resource not found

	// Provider errors
	CodeProviderTimeout  = "PROVIDER_TIMEOUT"  // Provider request timed out
	CodeProviderError    = "PROVIDER_ERROR"    // Generic provider error
	CodeProviderRejected = "PROVIDER_REJECTED" // Provider rejected the request

	// Internal errors
	CodeInternal = "INTERNAL" // Internal server error
)
