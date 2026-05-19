package http

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/redis/rueidis"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	orchestratorv1 "github.com/bashkirian/fintech-project/libs/genproto/orchestrator/v1"
	apiconfig "github.com/bashkirian/fintech-project/services/api/internal/config"
	apigrpc "github.com/bashkirian/fintech-project/services/api/internal/grpc"
	apimiddleware "github.com/bashkirian/fintech-project/services/api/internal/http/middleware"
)

// PayoutClient is an interface for the payout gRPC client.
// This allows mocking in tests while the real implementation uses *apigrpc.OrchestratorClient.
type PayoutClient interface {
	CreatePayout(ctx context.Context, in *orchestratorv1.CreatePayoutRequest, opts ...grpc.CallOption) (*orchestratorv1.CreatePayoutResponse, error)
	GetPayout(ctx context.Context, in *orchestratorv1.GetPayoutRequest, opts ...grpc.CallOption) (*orchestratorv1.GetPayoutResponse, error)
	CancelPayout(ctx context.Context, in *orchestratorv1.CancelPayoutRequest, opts ...grpc.CallOption) (*orchestratorv1.CancelPayoutResponse, error)
}

// NewRouter creates a router with rate limiting from config.
func NewRouter(log *zap.Logger, orchestrator *apigrpc.OrchestratorClient, redis rueidis.Client, cfg apiconfig.Config) http.Handler {
	return NewRouterWithClient(log, orchestrator.Payout, redis, cfg)
}

// NewRouterWithClient creates a router with a PayoutClient interface for testing.
func NewRouterWithClient(log *zap.Logger, client PayoutClient, redis rueidis.Client, cfg apiconfig.Config) http.Handler {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.Recoverer)
	router.Use(accessLog(log))

	// Configure rate limiter
	rateLimitConfig := apimiddleware.RateLimitConfig{
		KeyPrefix:         "ratelimit:global",
		RequestsPerSecond: cfg.RateLimitRequestsPerSecond,
		BurstSize:         cfg.RateLimitBurstSize,
	}
	router.Use(apimiddleware.RateLimitWithConfig(log, redis, cfg.RateLimitEnabled, rateLimitConfig))

	router.Get("/health", func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(`{"status":"ok"}`))
	})

	router.Post("/v1/payouts", createPayoutHandlerWithClient(log, client))
	router.Get("/v1/payouts/{id}", getPayoutHandler(log, client))
	router.Post("/v1/payouts/{id}/cancel", cancelPayoutHandler(log, client))

	return router
}

type createPayoutRequest struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
	Rail     string `json:"rail"`
}

// canonicalHash returns a stable SHA-256 hex string over the request fields.
func canonicalHash(amount int64, currency, rail string) string {
	// Manual canonical form avoids map-iteration order issues.
	canonical := fmt.Sprintf(`{"amount":%d,"currency":%q,"rail":%q}`, amount, currency, rail)
	sum := sha256.Sum256([]byte(canonical))
	return fmt.Sprintf("%x", sum)
}

func createPayoutHandlerWithClient(log *zap.Logger, client PayoutClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idempotencyKey := r.Header.Get("Idempotency-Key")
		if idempotencyKey == "" {
			http.Error(w, `{"error":"Idempotency-Key header is required"}`, http.StatusBadRequest)
			return
		}

		var body createPayoutRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}
		if body.Amount <= 0 {
			http.Error(w, `{"error":"amount must be positive"}`, http.StatusBadRequest)
			return
		}
		if body.Rail == "" {
			http.Error(w, `{"error":"rail is required"}`, http.StatusBadRequest)
			return
		}

		requestHash := canonicalHash(body.Amount, body.Currency, body.Rail)

		resp, err := client.CreatePayout(r.Context(), &orchestratorv1.CreatePayoutRequest{
			IdempotencyKey: idempotencyKey,
			RequestHash:    requestHash,
			Amount:         body.Amount,
			Currency:       body.Currency,
			Rail:           body.Rail,
		})
		if err != nil {
			if st, ok := status.FromError(err); ok && st.Code() == codes.AlreadyExists {
				http.Error(w, `{"error":"idempotency key reused with different request"}`, http.StatusConflict)
				return
			}
			log.Error("CreatePayout failed", zap.Error(err))
			http.Error(w, `{"error":"upstream error"}`, http.StatusBadGateway)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"payout_id": resp.GetPayoutId(),
			"status":    resp.GetStatus(),
		})
	}
}

type getPayoutResponse struct {
	PayoutID   string `json:"payout_id"`
	Status     string `json:"status"`
	Amount     int64  `json:"amount"`
	Currency   string `json:"currency"`
	Rail       string `json:"rail"`
	Provider   string `json:"provider"`
	ExternalID string `json:"external_id,omitempty"`
}

func getPayoutHandler(log *zap.Logger, client PayoutClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if id == "" {
			http.Error(w, `{"error":"payout id is required"}`, http.StatusBadRequest)
			return
		}

		resp, err := client.GetPayout(r.Context(), &orchestratorv1.GetPayoutRequest{
			PayoutId: id,
		})
		if err != nil {
			if st, ok := status.FromError(err); ok {
				switch st.Code() {
				case codes.NotFound:
					http.Error(w, `{"error":"payout not found"}`, http.StatusNotFound)
					return
				case codes.InvalidArgument:
					http.Error(w, `{"error":"`+st.Message()+`"}`, http.StatusBadRequest)
					return
				}
			}
			log.Error("GetPayout failed", zap.Error(err))
			http.Error(w, `{"error":"upstream error"}`, http.StatusBadGateway)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(getPayoutResponse{
			PayoutID:   resp.GetPayoutId(),
			Status:     resp.GetStatus(),
			Amount:     resp.GetAmount(),
			Currency:   resp.GetCurrency(),
			Rail:       resp.GetRail(),
			Provider:   resp.GetProvider(),
			ExternalID: resp.GetExternalId(),
		})
	}
}

func cancelPayoutHandler(log *zap.Logger, client PayoutClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if id == "" {
			http.Error(w, `{"error":"payout id is required"}`, http.StatusBadRequest)
			return
		}

		resp, err := client.CancelPayout(r.Context(), &orchestratorv1.CancelPayoutRequest{
			PayoutId: id,
		})
		if err != nil {
			if st, ok := status.FromError(err); ok {
				switch st.Code() {
				case codes.NotFound:
					http.Error(w, `{"error":"payout not found"}`, http.StatusNotFound)
					return
				case codes.InvalidArgument:
					http.Error(w, `{"error":"`+st.Message()+`"}`, http.StatusBadRequest)
					return
				case codes.FailedPrecondition:
					http.Error(w, `{"error":"`+st.Message()+`"}`, http.StatusConflict)
					return
				}
			}
			log.Error("CancelPayout failed", zap.Error(err))
			http.Error(w, `{"error":"upstream error"}`, http.StatusBadGateway)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]bool{"success": resp.GetSuccess()})
	}
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
