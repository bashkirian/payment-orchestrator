package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	stripe "github.com/stripe/stripe-go/v82"
	"go.uber.org/zap"

	orchestratorv1 "github.com/bashkirian/fintech-project/libs/genproto/orchestrator/v1"
	"github.com/bashkirian/fintech-project/services/webhook/internal/dedup"
	"github.com/bashkirian/fintech-project/services/webhook/internal/domain"
	"github.com/bashkirian/fintech-project/services/webhook/internal/grpc"
	"github.com/bashkirian/fintech-project/services/webhook/internal/stripeadapter"
)

// StripeWebhookHandler handles incoming Stripe webhook events.
type StripeWebhookHandler struct {
	parser        *stripeadapter.EventParser
	dedup         dedup.Deduplicator
	orchestrator  grpc.PayoutServiceClient
	webhookSecret string
	log           *zap.Logger
}

// NewStripeWebhookHandler creates a new Stripe webhook handler.
func NewStripeWebhookHandler(
	parser *stripeadapter.EventParser,
	dedup dedup.Deduplicator,
	orchestrator grpc.PayoutServiceClient,
	webhookSecret string,
	log *zap.Logger,
) *StripeWebhookHandler {
	return &StripeWebhookHandler{
		parser:        parser,
		dedup:         dedup,
		orchestrator:  orchestrator,
		webhookSecret: webhookSecret,
		log:           log,
	}
}

// ServeHTTP handles POST /v1/webhooks/stripe.
func (h *StripeWebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Read the raw body
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		h.log.Error("read body", zap.Error(err))
		h.writeError(w, http.StatusBadRequest, "failed to read body")
		return
	}
	defer r.Body.Close()

	// Get the Stripe signature header
	signature := r.Header.Get("Stripe-Signature")
	if signature == "" {
		h.log.Warn("missing stripe signature")
		h.writeError(w, http.StatusBadRequest, "missing stripe signature")
		return
	}

	// Verify the webhook signature and parse the event
	event, err := stripe.ConstructEvent(payload, signature, h.webhookSecret)
	if err != nil {
		h.log.Warn("invalid stripe signature", zap.Error(err))
		h.writeError(w, http.StatusBadRequest, "invalid signature")
		return
	}

	// Parse into normalized event
	providerEvent, err := h.parser.Parse(&event)
	if err != nil {
		h.log.Warn("unsupported event type",
			zap.String("type", string(event.Type)),
			zap.Error(err),
		)
		// Return 200 for unsupported events (acknowledge but ignore)
		h.writeOK(w)
		return
	}

	h.log.Info("received stripe event",
		zap.String("event_id", providerEvent.EventID),
		zap.String("external_id", providerEvent.ExternalID),
		zap.String("type", string(providerEvent.Type)),
	)

	// Deduplicate using Redis
	isDuplicate, err := h.dedup.IsProcessed(r.Context(), providerEvent.EventID)
	if err != nil {
		h.log.Error("dedup check failed", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "dedup check failed")
		return
	}

	if isDuplicate {
		h.log.Info("duplicate event, skipping", zap.String("event_id", providerEvent.EventID))
		h.writeOK(w)
		return
	}

	// Forward to orchestrator
	if err := h.forwardToOrchestrator(r.Context(), providerEvent); err != nil {
		h.log.Error("forward to orchestrator failed", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "forward failed")
		return
	}

	h.log.Info("event processed",
		zap.String("event_id", providerEvent.EventID),
		zap.String("external_id", providerEvent.ExternalID),
	)
	h.writeOK(w)
}

// forwardToOrchestrator calls the orchestrator's ApplyProviderUpdate RPC.
func (h *StripeWebhookHandler) forwardToOrchestrator(ctx context.Context, event domain.ProviderEvent) error {
	// Map our event type to provider status
	providerStatus := string(event.Type)

	_, err := h.orchestrator.ApplyProviderUpdate(ctx, &orchestratorv1.ApplyProviderUpdateRequest{
		PayoutId:          event.ExternalID,
		ProviderStatus:    providerStatus,
		ProviderReference: event.EventID,
	})
	if err != nil {
		return fmt.Errorf("apply provider update: %w", err)
	}

	return nil
}

func (h *StripeWebhookHandler) writeOK(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (h *StripeWebhookHandler) writeError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
