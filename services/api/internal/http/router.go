package http

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

func NewRouter(log *zap.Logger) http.Handler {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.Recoverer)
	router.Use(accessLog(log))

	router.Get("/health", func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(`{"status":"ok"}`))
	})

	return router
}

func accessLog(log *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			start := time.Now()
			wrapped := middleware.NewWrapResponseWriter(writer, request.ProtoMajor)

			next.ServeHTTP(wrapped, request)

			log.Info(
				"http_request",
				zap.String("request_id", middleware.GetReqID(request.Context())),
				zap.String("method", request.Method),
				zap.String("path", request.URL.Path),
				zap.String("remote_addr", request.RemoteAddr),
				zap.String("user_agent", request.UserAgent()),
				zap.Int("status", wrapped.Status()),
				zap.Int("bytes", wrapped.BytesWritten()),
				zap.Duration("duration", time.Since(start)),
			)
		})
	}
}