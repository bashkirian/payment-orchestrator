package worker

import (
	"context"
	"fmt"

	"github.com/bashkirian/fintech-project/services/orchestrator/internal/domain"
	"github.com/bashkirian/fintech-project/services/orchestrator/internal/provider"
	"github.com/bashkirian/fintech-project/services/orchestrator/internal/queue"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// RetryWorker processes retry messages from Kafka and attempts payout delivery.
type RetryWorker struct {
	consumer    queue.Consumer
	orchestrator Orchestrator
	repo        PayoutRepository
	log         *zap.Logger
}

// Orchestrator interface for retry worker (to avoid circular dependency).
type Orchestrator interface {
	ProcessRetry(ctx context.Context, payout domain.Payout, retryType queue.RetryType, provider domain.Provider) (provider.SendPayoutResult, error)
}

// PayoutRepository interface for retry worker.
type PayoutRepository interface {
	GetPayout(ctx context.Context, id uuid.UUID) (domain.Payout, error)
	UpdatePayoutRetryState(ctx context.Context, id uuid.UUID, params domain.UpdatePayoutRetryParams) (domain.Payout, error)
}

// NewRetryWorker creates a new retry worker.
func NewRetryWorker(
	consumer queue.Consumer,
	orchestrator Orchestrator,
	repo PayoutRepository,
	log *zap.Logger,
) *RetryWorker {
	return &RetryWorker{
		consumer:     consumer,
		orchestrator: orchestrator,
		repo:         repo,
		log:          log,
	}
}

// Start begins consuming and processing retry messages.
func (w *RetryWorker) Start(ctx context.Context) error {
	w.log.Info("starting retry worker")

	err := w.consumer.Consume(ctx, w.handleMessage)
	if err != nil && err != context.Canceled {
		return fmt.Errorf("consume retry messages: %w", err)
	}

	return nil
}

// handleMessage processes a single retry message.
func (w *RetryWorker) handleMessage(ctx context.Context, msg queue.RetryMessage) error {
	w.log.Info("processing retry message",
		zap.String("payout_id", msg.PayoutID),
		zap.String("retry_type", string(msg.RetryType)),
		zap.Int("attempt", msg.Attempt),
		zap.String("provider", string(msg.Provider)),
	)

	payoutID, err := uuid.Parse(msg.PayoutID)
	if err != nil {
		w.log.Error("invalid payout ID", zap.String("payout_id", msg.PayoutID), zap.Error(err))
		return nil // Don't retry invalid messages
	}

	// Fetch current payout state
	payout, err := w.repo.GetPayout(ctx, payoutID)
	if err != nil {
		return fmt.Errorf("get payout %s: %w", msg.PayoutID, err)
	}

	// Check if payout is still in a retryable state
	if !isRetryableState(payout.State) {
		w.log.Info("payout no longer retryable, skipping",
			zap.String("payout_id", msg.PayoutID),
			zap.String("state", string(payout.State)),
		)
		return nil
	}

	// Process the retry through orchestrator
	result, err := w.orchestrator.ProcessRetry(ctx, payout, msg.RetryType, msg.Provider)
	if err != nil {
		return fmt.Errorf("process retry: %w", err)
	}

	w.log.Info("retry processed",
		zap.String("payout_id", msg.PayoutID),
		zap.Bool("success", result.Success),
		zap.String("provider", string(result.UsedProvider)),
	)

	return nil
}

// isRetryableState checks if the payout can be retried.
func isRetryableState(state domain.PayoutState) bool {
	switch state {
	case domain.PayoutStateRetrying, domain.PayoutStatePending, domain.PayoutStateProcessing:
		return true
	default:
		return false
	}
}
