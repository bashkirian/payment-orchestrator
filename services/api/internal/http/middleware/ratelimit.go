package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/redis/rueidis"
	"go.uber.org/zap"
)

// Prometheus metrics for rate limiting
var (
	rateLimitRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "api",
			Subsystem: "ratelimit",
			Name:      "requests_total",
			Help:      "Total number of rate limit checks",
		},
		[]string{"result"}, // allowed, rejected
	)

	rateLimitBucketRefills = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "api",
			Subsystem: "ratelimit",
			Name:      "bucket_refills_total",
			Help:      "Total number of token bucket refills",
		},
	)
)

// RateLimitConfig holds configuration for the rate limiter.
type RateLimitConfig struct {
	// KeyPrefix is the Redis key prefix for rate limit counters.
	KeyPrefix string
	// RequestsPerSecond is the rate at which tokens are added to the bucket.
	RequestsPerSecond float64
	// BurstSize is the maximum number of tokens in the bucket.
	BurstSize int64
}

// DefaultRateLimitConfig returns sensible defaults for API rate limiting.
func DefaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		KeyPrefix:         "ratelimit:global",
		RequestsPerSecond: 10.0, // 10 requests per second
		BurstSize:         20,   // Allow burst of 20 requests
	}
}

// Metrics holds rate limiting metrics counters.
// Deprecated: Use Prometheus metrics instead. Kept for backward compatibility.
type Metrics struct {
	// RequestsTotal is the total number of requests processed.
	RequestsTotal int64
	// RequestsAllowed is the number of requests allowed through.
	RequestsAllowed int64
	// RequestsRejected is the number of requests rejected (429).
	RequestsRejected int64
	// BucketRefills is the number of times tokens were refilled.
	BucketRefills int64
}

// RateLimiter implements token bucket rate limiting using Redis.
type RateLimiter struct {
	client  rueidis.Client
	config  RateLimitConfig
	log     *zap.Logger
	metrics Metrics // Deprecated: kept for GetMetrics() backward compatibility
}

// NewRateLimiter creates a new Redis-backed rate limiter.
func NewRateLimiter(client rueidis.Client, config RateLimitConfig, log *zap.Logger) *RateLimiter {
	return &RateLimiter{
		client: client,
		config: config,
		log:    log,
	}
}

// Allow attempts to consume a token from the bucket.
// Returns true if the request should be allowed, false if rate limited.
func (rl *RateLimiter) Allow(ctx context.Context, key string) (bool, error) {
	fullKey := rl.config.KeyPrefix + ":" + key

	// Token bucket algorithm using Redis:
	// 1. Get current token count and last refill time
	// 2. Calculate tokens to add based on elapsed time
	// 3. Add tokens (up to burst size)
	// 4. Try to consume one token
	// 5. Update state

	now := time.Now().UnixMilli()

	// Use a Lua-like approach with rueidis commands
	// We'll use a simpler approach: check and decrement atomically

	// First, get current bucket state
	result, err := rl.client.Do(ctx, rl.client.B().Hmget().Key(fullKey).Field("tokens", "last_refill").Build()).ToArray()
	if err != nil && !rueidis.IsRedisNil(err) {
		return false, fmt.Errorf("redis hmget: %w", err)
	}

	var tokens float64
	var lastRefill int64

	if len(result) == 2 {
		if !result[0].IsNil() {
			tokensStr, _ := result[0].ToString()
			tokens, _ = strconv.ParseFloat(tokensStr, 64)
		} else {
			tokens = float64(rl.config.BurstSize)
		}
		if !result[1].IsNil() {
			lastRefillStr, _ := result[1].ToString()
			lastRefill, _ = strconv.ParseInt(lastRefillStr, 10, 64)
		} else {
			lastRefill = now
		}
	} else {
		// Initialize bucket with full tokens
		tokens = float64(rl.config.BurstSize)
		lastRefill = now
	}

	// Calculate refill
	elapsed := float64(now-lastRefill) / 1000.0 // seconds
	tokensToAdd := elapsed * rl.config.RequestsPerSecond
	tokens = min(tokens+tokensToAdd, float64(rl.config.BurstSize))

	// Track bucket refills in Prometheus
	if tokensToAdd > 0 {
		rateLimitBucketRefills.Inc()
	}

	// Check if we have tokens
	if tokens >= 1.0 {
		tokens -= 1.0

		// Update bucket state
		err = rl.client.Do(ctx, rl.client.B().Hmset().Key(fullKey).FieldValue().
			FieldValue("tokens", fmt.Sprintf("%.2f", tokens)).
			FieldValue("last_refill", strconv.FormatInt(now, 10)).
			Build()).Error()
		if err != nil {
			return false, fmt.Errorf("redis hmset: %w", err)
		}

		// Set expiry for cleanup (5 minutes of inactivity)
		rl.client.Do(ctx, rl.client.B().Expire().Key(fullKey).Seconds(300).Build())

		// Record Prometheus metrics
		rateLimitRequestsTotal.WithLabelValues("allowed").Inc()

		rl.metrics.RequestsAllowed++
		rl.metrics.RequestsTotal++
		return true, nil
	}

	// No tokens available - rate limited
	// Record Prometheus metrics
	rateLimitRequestsTotal.WithLabelValues("rejected").Inc()

	rl.metrics.RequestsRejected++
	rl.metrics.RequestsTotal++
	return false, nil
}

// GetMetrics returns current rate limiting metrics.
func (rl *RateLimiter) GetMetrics() Metrics {
	return rl.metrics
}

// RateLimit returns a middleware that enforces rate limiting using Redis token bucket.
// When enabled is false (or client is nil) the middleware is a no-op passthrough.
func RateLimit(log *zap.Logger, client rueidis.Client, enabled bool) func(http.Handler) http.Handler {
	return RateLimitWithConfig(log, client, enabled, DefaultRateLimitConfig())
}

// RateLimitWithConfig returns a middleware with custom configuration.
func RateLimitWithConfig(log *zap.Logger, client rueidis.Client, enabled bool, config RateLimitConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if !enabled || client == nil {
			return next
		}

		limiter := NewRateLimiter(client, config, log)

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Use global key for single-tenant MVP
			// In multi-tenant, use: extractAPIKey(r) or extractUserID(r)
			key := "global"

			allowed, err := limiter.Allow(r.Context(), key)
			if err != nil {
				log.Error("rate limit check failed", zap.Error(err))
				// On error, allow the request through (fail-open)
				next.ServeHTTP(w, r)
				return
			}

			if !allowed {
				log.Warn("rate limit exceeded",
					zap.String("remote_addr", r.RemoteAddr),
					zap.String("path", r.URL.Path),
				)

				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"error":"rate limit exceeded","retry_after":1}`))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
