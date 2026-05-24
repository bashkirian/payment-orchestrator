package observability

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// httpMetricsCache holds cached metrics per namespace to avoid re-registration.
var httpMetricsCache = make(map[string]*httpMetrics)
var httpMetricsMu sync.Mutex

// httpMetrics holds the Prometheus metrics for HTTP instrumentation.
type httpMetrics struct {
	latency        *prometheus.HistogramVec
	requestsTotal  *prometheus.CounterVec
	inFlight       *prometheus.GaugeVec
}

// getHTTPMetrics returns cached metrics or creates new ones for the namespace.
func getHTTPMetrics(namespace string) *httpMetrics {
	httpMetricsMu.Lock()
	defer httpMetricsMu.Unlock()

	if m, ok := httpMetricsCache[namespace]; ok {
		return m
	}

	m := &httpMetrics{
		latency: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Subsystem: "http",
				Name:      "request_duration_seconds",
				Help:      "HTTP request latency in seconds",
				Buckets:   DefBuckets,
			},
			[]string{"method", "path", "status"},
		),
		requestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "http",
				Name:      "requests_total",
				Help:      "Total number of HTTP requests",
			},
			[]string{"method", "path", "status"},
		),
		inFlight: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "http",
				Name:      "requests_in_flight",
				Help:      "Current number of HTTP requests being processed",
			},
			[]string{"method"},
		),
	}

	httpMetricsCache[namespace] = m
	return m
}

// HTTPMetricsMiddleware returns a chi middleware that records HTTP metrics.
// It tracks request latency, counts by method/path/status, and in-flight requests.
func HTTPMetricsMiddleware(namespace string) func(http.Handler) http.Handler {
	metrics := getHTTPMetrics(namespace)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			method := r.Method

			// Track in-flight requests
			metrics.inFlight.WithLabelValues(method).Inc()
			defer metrics.inFlight.WithLabelValues(method).Dec()

			// Wrap response writer to capture status code
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			next.ServeHTTP(ww, r)

			// Record metrics
			duration := time.Since(start).Seconds()
			status := strconv.Itoa(ww.Status())
			path := normalizePath(r)

			metrics.latency.WithLabelValues(method, path, status).Observe(duration)
			metrics.requestsTotal.WithLabelValues(method, path, status).Inc()
		})
	}
}

// normalizePath extracts a normalized path pattern from the request.
// It replaces URL parameters with placeholders to avoid high cardinality.
func normalizePath(r *http.Request) string {
	rctx := chi.RouteContext(r.Context())
	if rctx == nil {
		// Fallback: use the actual path (may have high cardinality)
		return r.URL.Path
	}

	// Use the route pattern if available (e.g., /v1/payouts/{id})
	pattern := rctx.RoutePattern()
	if pattern == "" || pattern == "/*" {
		// No route pattern matched, use actual path
		return r.URL.Path
	}

	return pattern
}

