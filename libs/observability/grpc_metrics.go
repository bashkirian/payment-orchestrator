package observability

import (
	"context"
	"time"

	"github.com/grpc-ecosystem/go-grpc-prometheus"
	"google.golang.org/grpc"
)

// GRPCMetrics holds the gRPC Prometheus metrics.
// It wraps grpc-ecosystem/go-grpc-prometheus default metrics.
// Note: The grpc-prometheus library auto-registers metrics via init(),
// so we use the DefaultServerMetrics which is already registered.
type GRPCMetrics struct{}

// NewGRPCMetrics creates a new GRPCMetrics instance.
// Uses the default server metrics that are already registered by init().
func NewGRPCMetrics() *GRPCMetrics {
	// Enable histograms on the default server metrics
	grpc_prometheus.EnableHandlingTimeHistogram()
	return &GRPCMetrics{}
}

// UnaryServerInterceptor returns a gRPC unary server interceptor that records metrics.
func (m *GRPCMetrics) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return grpc_prometheus.UnaryServerInterceptor
}

// StreamServerInterceptor returns a gRPC stream server interceptor that records metrics.
func (m *GRPCMetrics) StreamServerInterceptor() grpc.StreamServerInterceptor {
	return grpc_prometheus.StreamServerInterceptor
}

// Register is a no-op since metrics are auto-registered by the library's init().
// Kept for API compatibility.
func (m *GRPCMetrics) Register() {
	// Already registered by grpc_prometheus init()
}

// GRPCServerOptions returns a slice of grpc.ServerOption that includes the
// Prometheus interceptors. Use this when creating a new gRPC server.
func (m *GRPCMetrics) GRPCServerOptions() []grpc.ServerOption {
	return []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(m.UnaryServerInterceptor()),
		grpc.ChainStreamInterceptor(m.StreamServerInterceptor()),
	}
}

// DefaultGRPCServerOptions returns grpc.ServerOptions with default Prometheus interceptors.
func DefaultGRPCServerOptions() []grpc.ServerOption {
	metrics := NewGRPCMetrics()
	return metrics.GRPCServerOptions()
}

// GRPCClientMetrics holds client-side gRPC metrics.
type GRPCClientMetrics struct {
	client *grpc_prometheus.ClientMetrics
}

// NewGRPCClientMetrics creates client-side gRPC metrics.
func NewGRPCClientMetrics() *GRPCClientMetrics {
	return &GRPCClientMetrics{client: grpc_prometheus.DefaultClientMetrics}
}

// UnaryClientInterceptor returns a gRPC unary client interceptor.
func (m *GRPCClientMetrics) UnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return m.client.UnaryClientInterceptor()
}

// StreamClientInterceptor returns a gRPC stream client interceptor.
func (m *GRPCClientMetrics) StreamClientInterceptor() grpc.StreamClientInterceptor {
	return m.client.StreamClientInterceptor()
}

// DialOption returns a grpc.DialOption that includes the client interceptors.
func (m *GRPCClientMetrics) DialOption() grpc.DialOption {
	return grpc.WithUnaryInterceptor(m.UnaryClientInterceptor())
}

// grpcContextKey is used for storing values in context.
type grpcContextKey string

const startTimeKey grpcContextKey = "start_time"

// StartTimeFromContext retrieves the start time from context.
func StartTimeFromContext(ctx context.Context) (time.Time, bool) {
	t, ok := ctx.Value(startTimeKey).(time.Time)
	return t, ok
}

// ContextWithStartTime adds a start time to the context.
func ContextWithStartTime(ctx context.Context, t time.Time) context.Context {
	return context.WithValue(ctx, startTimeKey, t)
}
