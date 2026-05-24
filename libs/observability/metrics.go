package observability

// Namespace constants for metric naming.
const (
	NamespaceAPI          = "api"
	NamespaceOrchestrator = "orchestrator"
	NamespaceWebhook      = "webhook"
)

// Default histogram buckets for HTTP/gRPC latency (in seconds).
var DefBuckets = []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}
