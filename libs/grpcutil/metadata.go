package grpcutil

import (
	"context"

	"google.golang.org/grpc/metadata"
)

const (
	// RequestIDKey is the gRPC metadata key for request ID propagation.
	RequestIDKey = "x-request-id"
)

// SetRequestID adds a request ID to the outgoing gRPC metadata.
func SetRequestID(ctx context.Context, requestID string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, RequestIDKey, requestID)
}

// GetRequestID retrieves the request ID from incoming gRPC metadata.
// Returns an empty string if not present.
func GetRequestID(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	values := md.Get(RequestIDKey)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
