package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/redis/rueidis"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// setupRedisClient creates a real Redis client for integration tests.
// Uses REDIS_ADDR and REDIS_PASSWORD env vars, or skips if not available.
func setupRedisClient(t *testing.T) rueidis.Client {
	t.Helper()

	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	password := os.Getenv("REDIS_PASSWORD")

	client, err := rueidis.NewClient(rueidis.ClientOption{
		InitAddress: []string{addr},
		Password:    password,
	})
	if err != nil {
		t.Skipf("failed to connect to Redis at %s: %v", addr, err)
	}

	t.Cleanup(func() { client.Close() })

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Do(ctx, client.B().Ping().Build()).Error(); err != nil {
		t.Skipf("Redis ping failed: %v", err)
	}

	return client
}

func TestRateLimiter_AllowsFirstRequest(t *testing.T) {
	client := setupRedisClient(t)
	log := zap.NewNop()

	config := RateLimitConfig{
		KeyPrefix:         "test:ratelimit:allow",
		RequestsPerSecond: 1.0,
		BurstSize:         5,
	}

	// Clean up any existing keys
	ctx := context.Background()
	key := config.KeyPrefix + ":global"
	_ = client.Do(ctx, client.B().Del().Key(key).Build()).Error()

	limiter := NewRateLimiter(client, config, log)

	allowed, err := limiter.Allow(ctx, "global")
	require.NoError(t, err)
	require.True(t, allowed, "first request should be allowed")
}

func TestRateLimiter_RejectsWhenBurstExhausted(t *testing.T) {
	client := setupRedisClient(t)
	log := zap.NewNop()

	config := RateLimitConfig{
		KeyPrefix:         "test:ratelimit:reject",
		RequestsPerSecond: 1.0,
		BurstSize:         2, // Very small burst for testing
	}

	// Clean up
	ctx := context.Background()
	key := config.KeyPrefix + ":global"
	_ = client.Do(ctx, client.B().Del().Key(key).Build()).Error()

	limiter := NewRateLimiter(client, config, log)

	// Exhaust the burst
	allowed, err := limiter.Allow(ctx, "global")
	require.NoError(t, err)
	require.True(t, allowed)

	allowed, err = limiter.Allow(ctx, "global")
	require.NoError(t, err)
	require.True(t, allowed)

	// Third request should be rejected (burst exhausted)
	allowed, err = limiter.Allow(ctx, "global")
	require.NoError(t, err)
	require.False(t, allowed, "request after burst exhaustion should be rejected")
}

func TestRateLimiter_TokensRefill(t *testing.T) {
	client := setupRedisClient(t)
	log := zap.NewNop()

	config := RateLimitConfig{
		KeyPrefix:         "test:ratelimit:refill",
		RequestsPerSecond: 10.0, // 10 tokens per second
		BurstSize:         2,
	}

	// Clean up
	ctx := context.Background()
	key := config.KeyPrefix + ":global"
	_ = client.Do(ctx, client.B().Del().Key(key).Build()).Error()

	limiter := NewRateLimiter(client, config, log)

	// Exhaust the burst
	limiter.Allow(ctx, "global")
	limiter.Allow(ctx, "global")

	// Should be rejected immediately
	allowed, _ := limiter.Allow(ctx, "global")
	require.False(t, allowed)

	// Wait for tokens to refill (100ms = 1 token at 10/s)
	time.Sleep(150 * time.Millisecond)

	// Should be allowed now
	allowed, err := limiter.Allow(ctx, "global")
	require.NoError(t, err)
	require.True(t, allowed, "request should be allowed after refill")
}

func TestRateLimiter_Metrics(t *testing.T) {
	client := setupRedisClient(t)
	log := zap.NewNop()

	config := RateLimitConfig{
		KeyPrefix:         "test:ratelimit:metrics",
		RequestsPerSecond: 1.0,
		BurstSize:         3,
	}

	// Clean up
	ctx := context.Background()
	key := config.KeyPrefix + ":global"
	_ = client.Do(ctx, client.B().Del().Key(key).Build()).Error()

	limiter := NewRateLimiter(client, config, log)

	// Make some requests
	limiter.Allow(ctx, "global")
	limiter.Allow(ctx, "global")
	limiter.Allow(ctx, "global")
	limiter.Allow(ctx, "global") // Rejected (burst = 3)

	metrics := limiter.GetMetrics()
	require.Equal(t, int64(4), metrics.RequestsTotal)
	require.Equal(t, int64(3), metrics.RequestsAllowed)
	require.Equal(t, int64(1), metrics.RequestsRejected)
}

func TestRateLimitMiddleware_Disabled(t *testing.T) {
	// When disabled, middleware should be a no-op
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// nil client, disabled = true -> passthrough
	middleware := RateLimitWithConfig(zap.NewNop(), nil, false, RateLimitConfig{})
	wrapped := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestRateLimitMiddleware_Enabled(t *testing.T) {
	client := setupRedisClient(t)
	log := zap.NewNop()

	config := RateLimitConfig{
		KeyPrefix:         "test:ratelimit:middleware",
		RequestsPerSecond: 1.0,
		BurstSize:         2,
	}

	// Clean up
	ctx := context.Background()
	key := config.KeyPrefix + ":global"
	_ = client.Do(ctx, client.B().Del().Key(key).Build()).Error()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	middleware := RateLimitWithConfig(log, client, true, config)
	wrapped := middleware(handler)

	// First two requests should succeed
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
	}

	// Third request should be rate limited
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.Contains(t, rec.Body.String(), "rate limit exceeded")
	require.Equal(t, "1", rec.Header().Get("Retry-After"))
}

func TestRateLimitMiddleware_FailOpen(t *testing.T) {
	// When Redis is unavailable, the middleware should fail open
	// (allow requests through rather than blocking)

	log := zap.NewNop()
	config := RateLimitConfig{
		KeyPrefix:         "test:ratelimit:failopen",
		RequestsPerSecond: 1.0,
		BurstSize:         1,
	}

	// Create a broken client that will fail
	brokenClient, err := rueidis.NewClient(rueidis.ClientOption{
		InitAddress: []string{"localhost:19999"}, // Non-existent port
	})
	if err != nil {
		t.Skip("could not create broken client")
	}
	defer brokenClient.Close()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := RateLimitWithConfig(log, brokenClient, true, config)
	wrapped := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	// Should still get OK because middleware fails open
	wrapped.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}
