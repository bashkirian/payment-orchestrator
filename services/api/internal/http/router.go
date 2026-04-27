package http

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"

	apigrpc "github.com/bashkirian/fintech-project/services/api/internal/grpc"
	orchestratorv1 "github.com/bashkirian/fintech-project/libs/genproto/orchestrator/v1"
)

func NewRouter(log *zap.Logger, orchestrator *apigrpc.OrchestratorClient) http.Handler {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.Recoverer)
	router.Use(accessLog(log))

	router.Get("/health", func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(`{"status":"ok"}`))
	})

	router.Post("/v1/payouts", createPayoutHandler(log, orchestrator))

	return router
}

type createPayoutRequest struct {
	PayoutID string `json:"payout_id"`
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
	Provider string `json:"provider"`
}

func createPayoutHandler(log *zap.Logger, orchestrator *apigrpc.OrchestratorClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body createPayoutRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}

		resp, err := orchestrator.Payout.CreatePayout(r.Context(), &orchestratorv1.CreatePayoutRequest{
			PayoutId: body.PayoutID,
			Amount:   body.Amount,
			Currency: body.Currency,
			Provider: body.Provider,
		})
		if err != nil {
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