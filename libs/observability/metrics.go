package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Namespace and subsystem constants for metric naming.
const (
	NamespaceAPI          = "api"
	NamespaceOrchestrator = "orchestrator"
	NamespaceWebhook      = "webhook"
)

// CounterOpts wraps prometheus.CounterOpts with sensible defaults.
type CounterOpts struct {
	Namespace string // Service namespace (api, orchestrator, webhook)
	Subsystem string // Subsystem within service (http, grpc, payout, etc.)
	Name      string // Metric name (without _total suffix)
	Help      string // Metric description
}

// HistogramOpts wraps prometheus.HistogramOpts with sensible defaults.
type HistogramOpts struct {
	Namespace string
	Subsystem string
	Name      string
	Help      string
	Buckets   []float64 // Optional: defaults to DefBuckets if nil
}

// GaugeOpts wraps prometheus.GaugeOpts with sensible defaults.
type GaugeOpts struct {
	Namespace string
	Subsystem string
	Name      string
	Help      string
}

// Default histogram buckets for HTTP/gRPC latency (in seconds).
var DefBuckets = []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}

// NewCounter creates a new counter vector with labels.
func NewCounter(opts CounterOpts, labels ...string) *prometheus.CounterVec {
	return promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: opts.Namespace,
			Subsystem: opts.Subsystem,
			Name:      opts.Name + "_total",
			Help:      opts.Help,
		},
		labels,
	)
}

// NewHistogram creates a new histogram vector with labels.
func NewHistogram(opts HistogramOpts, labels ...string) *prometheus.HistogramVec {
	buckets := opts.Buckets
	if buckets == nil {
		buckets = DefBuckets
	}
	return promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: opts.Namespace,
			Subsystem: opts.Subsystem,
			Name:      opts.Name + "_seconds",
			Help:      opts.Help,
			Buckets:   buckets,
		},
		labels,
	)
}

// NewGauge creates a new gauge vector with labels.
func NewGauge(opts GaugeOpts, labels ...string) *prometheus.GaugeVec {
	return promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: opts.Namespace,
			Subsystem: opts.Subsystem,
			Name:      opts.Name,
			Help:      opts.Help,
		},
		labels,
	)
}

// NewCounter creates a simple counter without labels.
func NewSimpleCounter(opts CounterOpts) prometheus.Counter {
	return promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: opts.Namespace,
			Subsystem: opts.Subsystem,
			Name:      opts.Name + "_total",
			Help:      opts.Help,
		},
	)
}

// NewGauge creates a simple gauge without labels.
func NewSimpleGauge(opts GaugeOpts) prometheus.Gauge {
	return promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: opts.Namespace,
			Subsystem: opts.Subsystem,
			Name:      opts.Name,
			Help:      opts.Help,
		},
	)
}
