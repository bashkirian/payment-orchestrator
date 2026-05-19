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

	"github.com/bashkirian/fintech-project/libs/errors"
	orchestratorv1 "github.com/bashkirian/fintech-project/libs/genproto/orchestrator/v1"
	"github.com/bashkirian/fintech-project/libs/grpcutil"
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
		requestID := middleware.GetReqID(r.Context())
		ctx := grpcutil.SetRequestID(r.Context(), requestID)

		idempotencyKey := r.Header.Get("Idempotency-Key")
		if idempotencyKey == "" {
			writeError(w, http.StatusBadRequest, errors.CodeInvalidArgument, "Idempotency-Key header is required")
			return
		}

		var body createPayoutRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, errors.CodeInvalidArgument, "invalid request body")
			return
		}
		if body.Amount <= 0 {
			writeError(w, http.StatusBadRequest, errors.CodeInvalidArgument, "amount must be positive")
			return
		}
		if body.Rail == "" {
			writeError(w, http.StatusBadRequest, errors.CodeInvalidArgument, "rail is required")
			return
		}

		requestHash := canonicalHash(body.Amount, body.Currency, body.Rail)

		resp, err := client.CreatePayout(ctx, &orchestratorv1.CreatePayoutRequest{
			IdempotencyKey: idempotencyKey,
			RequestHash:    requestHash,
			Amount:         body.Amount,
			Currency:       body.Currency,
			Rail:           body.Rail,
		})
		if err != nil {
			if st, ok := status.FromError(err); ok && st.Code() == codes.AlreadyExists {
				writeError(w, http.StatusConflict, errors.CodeIdempotencyConflict, "idempotency key reused with different request")
				return
			}
			log.Error("CreatePayout failed", zap.String("request_id", requestID), zap.Error(err))
			writeError(w, http.StatusBadGateway, errors.CodeInternal, "upstream error")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", requestID)
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
		requestID := middleware.GetReqID(r.Context())
		ctx := grpcutil.SetRequestID(r.Context(), requestID)

		id := chi.URLParam(r, "id")
		if id == "" {
			writeError(w, http.StatusBadRequest, errors.CodeInvalidArgument, "payout id is required")
			return
		}

		resp, err := client.GetPayout(ctx, &orchestratorv1.GetPayoutRequest{
			PayoutId: id,
		})
		if err != nil {
			if st, ok := status.FromError(err); ok {
				switch st.Code() {
				case codes.NotFound:
					writeError(w, http.StatusNotFound, errors.CodeNotFound, "payout not found")
					return
				case codes.InvalidArgument:
					writeError(w, http.StatusBadRequest, errors.CodeInvalidUUID, st.Message())
					return
				}
			}
			log.Error("GetPayout failed", zap.String("request_id", requestID), zap.Error(err))
			writeError(w, http.StatusBadGateway, errors.CodeInternal, "upstream error")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", requestID)
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
		requestID := middleware.GetReqID(r.Context())
		ctx := grpcutil.SetRequestID(r.Context(), requestID)

		id := chi.URLParam(r, "id")
		if id == "" {
			writeError(w, http.StatusBadRequest, errors.CodeInvalidArgument, "payout id is required")
			return
		}

		resp, err := client.CancelPayout(ctx, &orchestratorv1.CancelPayoutRequest{
			PayoutId: id,
		})
		if err != nil {
			if st, ok := status.FromError(err); ok {
				switch st.Code() {
				case codes.NotFound:
					writeError(w, http.StatusNotFound, errors.CodeNotFound, "payout not found")
					return
				case codes.InvalidArgument:
					writeError(w, http.StatusBadRequest, errors.CodeInvalidUUID, st.Message())
					return
				case codes.FailedPrecondition:
					writeError(w, http.StatusConflict, errors.CodeInvalidState, st.Message())
					return
				}
			}
			log.Error("CancelPayout failed", zap.String("request_id", requestID), zap.Error(err))
			writeError(w, http.StatusBadGateway, errors.CodeInternal, "upstream error")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", requestID)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]bool{"success": resp.GetSuccess()})
	}
}

// writeError writes a structured JSON error response with code and message.
func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"code":    code,
		"message": message,
	})
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
