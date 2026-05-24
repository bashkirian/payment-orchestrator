package http

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/rueidis"
	"go.uber.org/zap"

	"github.com/bashkirian/fintech-project/libs/observability"
	"github.com/bashkirian/fintech-project/services/webhook/internal/dedup"
	"github.com/bashkirian/fintech-project/services/webhook/internal/grpc"
	"github.com/bashkirian/fintech-project/services/webhook/internal/stripeadapter"
)

// Dependencies holds all dependencies needed by the router.
type Dependencies struct {
	Log          *zap.Logger
	RedisClient  rueidis.Client
	Orchestrator *grpc.OrchestratorClient
	StripeSecret string
}

// NewRouter creates the HTTP router with all endpoints configured.
func NewRouter(deps Dependencies) http.Handler {
	r := chi.NewRouter()
	r.Use(requestIDMiddleware)
	r.Use(middleware.Recoverer)
	r.Use(accessLog(deps.Log))

	// HTTP metrics middleware
	r.Use(observability.HTTPMetricsMiddleware(observability.NamespaceWebhook))

	// Prometheus metrics endpoint
	r.Get("/metrics", promhttp.Handler().ServeHTTP)

	// Health check endpoint
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// Stripe webhook endpoint
	stripeParser := stripeadapter.NewEventParser()
	dedupService := dedup.NewService(deps.RedisClient, 0) // uses default TTL
	stripeHandler := NewStripeWebhookHandler(
		stripeParser,
		dedupService,
		deps.Orchestrator.Payout,
		deps.StripeSecret,
		deps.Log,
	)
	r.Post("/v1/webhooks/stripe", stripeHandler.ServeHTTP)

	return r
}

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" {
			id = observability.NewRequestID()
		}
		ctx := observability.WithRequestID(r.Context(), id)
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func accessLog(log *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			wrapped := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			next.ServeHTTP(wrapped, r)

			log.Info("http_request",
				zap.String("request_id", observability.RequestIDFromContext(r.Context())),
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.String("remote_addr", r.RemoteAddr),
				zap.Int("status", wrapped.Status()),
				zap.Duration("duration", time.Since(start)),
			)
		})
	}
}
