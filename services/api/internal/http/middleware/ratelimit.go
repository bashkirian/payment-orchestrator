package middleware

import (
	"net/http"

	"github.com/redis/rueidis"
	"go.uber.org/zap"
)

// RateLimit returns a middleware that enforces per-IP rate limiting using Redis.
// When enabled is false (or client is nil) the middleware is a no-op passthrough —
// useful for local development and testing without Redis.
//
// TODO: implement sliding-window / token-bucket logic once Redis is required.
func RateLimit(log *zap.Logger, client rueidis.Client, enabled bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if !enabled || client == nil {
			return next
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Stub: rate limiting is not yet enforced.
			// Replace this block with actual Redis-backed logic (e.g. INCR + EXPIRE).
			log.Debug("rate-limit middleware (stub)", zap.String("remote_addr", r.RemoteAddr))
			next.ServeHTTP(w, r)
		})
	}
}
