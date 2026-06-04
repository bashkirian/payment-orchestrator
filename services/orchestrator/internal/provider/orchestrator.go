package provider

import (
	"context"
	"errors"

	"github.com/bashkirian/fintech-project/services/orchestrator/internal/config"
	"github.com/bashkirian/fintech-project/services/orchestrator/internal/domain"
	"github.com/bashkirian/fintech-project/services/orchestrator/internal/queue"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Orchestrator handles payout execution with automatic fallback on failure.
type Orchestrator struct {
	registry       *Registry
	router         *Router
	successTracker *SuccessTracker
	publisher      queue.Publisher
	repo           PayoutRepository
	log            *zap.Logger
	routingAlgo    RoutingAlgorithm
	retryCfg       config.RetryConfig
}

// PayoutRepository interface for orchestrator.
type PayoutRepository interface {
	GetPayout(ctx context.Context, id uuid.UUID) (domain.Payout, error)
	UpdatePayoutRetryState(ctx context.Context, id uuid.UUID, params domain.UpdatePayoutRetryParams) (domain.Payout, error)
}

// NewOrchestrator creates a new Orchestrator with the given routing algorithm.
func NewOrchestrator(
	registry *Registry,
	router *Router,
	successTracker *SuccessTracker,
	publisher queue.Publisher,
	repo PayoutRepository,
	log *zap.Logger,
	routingAlgo RoutingAlgorithm,
	retryCfg config.RetryConfig,
) *Orchestrator {
	return &Orchestrator{
		registry:       registry,
		router:         router,
		successTracker: successTracker,
		publisher:      publisher,
		repo:           repo,
		log:            log,
		routingAlgo:    routingAlgo,
		retryCfg:       retryCfg,
	}
}

// SendPayoutResult contains the result of a payout attempt with fallback.
type SendPayoutResult struct {
	ExternalID     string
	UsedProvider   domain.Provider
	TriedProviders []domain.Provider
	Success        bool
}

// SendPayoutWithFallback attempts to send a payout using the configured routing algorithm.
// On retryable failure, it falls back to the next provider in order.
// Returns the result including which provider was used and which providers were tried.
func (o *Orchestrator) SendPayoutWithFallback(
	ctx context.Context,
	payout domain.Payout,
) SendPayoutResult {
	result := SendPayoutResult{
		TriedProviders: make([]domain.Provider, 0),
	}

	o.log.Info("SendPayoutWithFallback: starting payout routing",
		zap.String("payout_id", payout.ID.String()),
		zap.String("rail", string(payout.Rail)),
		zap.Int64("amount_cents", payout.AmountCents),
		zap.String("currency", payout.Currency),
		zap.String("routing_algorithm", o.routingAlgo.String()),
	)

	providers, err := o.router.SelectProviders(payout.Rail, o.routingAlgo)
	if err != nil {
		o.log.Error("no providers available for rail",
			zap.String("rail", string(payout.Rail)),
			zap.String("payout_id", payout.ID.String()),
		)
		return result
	}

	o.log.Info("providers selected for payout",
		zap.String("payout_id", payout.ID.String()),
		zap.Strings("providers", providerNamesFromMeta(providers)),
		zap.Int("count", len(providers)),
	)

	var lastErr error
	for i, p := range providers {
		providerName := p.Meta.Provider
		result.TriedProviders = append(result.TriedProviders, providerName)

		o.log.Info("attempting payout with provider",
			zap.String("payout_id", payout.ID.String()),
			zap.String("provider", string(providerName)),
			zap.String("rail", string(payout.Rail)),
			zap.Int("attempt", i+1),
			zap.Int("total_providers", len(providers)),
		)

		extID, err := p.Client.SendPayout(ctx, payout)
		if err == nil {
			// Success
			o.successTracker.RecordSuccess(string(payout.Rail), string(providerName))
			o.log.Info("payout succeeded",
				zap.String("payout_id", payout.ID.String()),
				zap.String("provider", string(providerName)),
				zap.String("external_id", extID),
				zap.Int("attempts", i+1),
			)
			result.ExternalID = extID
			result.UsedProvider = providerName
			result.Success = true
			return result
		}

		// Failure - record it
		o.successTracker.RecordFailure(string(payout.Rail), string(providerName))
		lastErr = err

		o.log.Warn("provider failed",
			zap.String("payout_id", payout.ID.String()),
			zap.String("provider", string(providerName)),
			zap.Error(err),
		)

		// Check if error is terminal (don't try next provider)
		if !o.isRetryable(err) {
			o.log.Warn("terminal error, stopping fallback",
				zap.String("payout_id", payout.ID.String()),
				zap.String("provider", string(providerName)),
				zap.Error(err),
			)
			break
		}

		// Log fallback decision
		if i < len(providers)-1 {
			o.log.Info("falling back to next provider",
				zap.String("payout_id", payout.ID.String()),
				zap.String("failed_provider", string(providerName)),
				zap.String("next_provider", string(providers[i+1].Meta.Provider)),
			)
		}
	}

	o.log.Error("all providers failed",
		zap.String("payout_id", payout.ID.String()),
		zap.Strings("tried_providers", providerNames(result.TriedProviders)),
		zap.Error(lastErr),
	)

	return result
}

// isRetryable returns true if the error is transient and we should try the next provider.
// Terminal errors (card decline, fraud, invalid request) should not be retried.
func (o *Orchestrator) isRetryable(err error) bool {
	var re *RetryableError
	if errors.As(err, &re) {
		return re.Retryable
	}
	// Default: treat unknown errors as retryable
	return true
}

// CancelPayout cancels a payout with the provider that was used to create it.
func (o *Orchestrator) CancelPayout(ctx context.Context, payout domain.Payout) error {
	o.log.Info("CancelPayout: starting cancellation",
		zap.String("payout_id", payout.ID.String()),
		zap.String("provider", string(payout.Provider)),
		zap.Stringp("external_id", payout.ExternalID),
	)

	client, err := o.registry.GetByProvider(payout.Provider)
	if err != nil {
		o.log.Error("CancelPayout: provider not found",
			zap.String("payout_id", payout.ID.String()),
			zap.String("provider", string(payout.Provider)),
			zap.Error(err),
		)
		return err
	}

	if err := client.CancelPayout(ctx, payout); err != nil {
		o.log.Error("CancelPayout: provider cancel failed",
			zap.String("payout_id", payout.ID.String()),
			zap.String("provider", string(payout.Provider)),
			zap.Error(err),
		)
		return err
	}

	o.log.Info("CancelPayout: cancellation successful",
		zap.String("payout_id", payout.ID.String()),
		zap.String("provider", string(payout.Provider)),
	)
	return nil
}

// providerNames converts a slice of providers to strings for logging.
func providerNames(providers []domain.Provider) []string {
	names := make([]string, len(providers))
	for i, p := range providers {
		names[i] = string(p)
	}
	return names
}

// providerNamesFromMeta extracts provider names from ProviderWithMeta slice.
func providerNamesFromMeta(providers []ProviderWithMeta) []string {
	names := make([]string, len(providers))
	for i, p := range providers {
		names[i] = string(p.Meta.Provider)
	}
	return names
}

// ProcessRetry handles a retry message from the queue.
// It implements the hybrid retry strategy:
// - Provider retry: retry same provider (short delay)
// - Global retry: fresh provider selection (longer delay)
func (o *Orchestrator) ProcessRetry(
	ctx context.Context,
	payout domain.Payout,
	retryType queue.RetryType,
	provider domain.Provider,
) (SendPayoutResult, error) {
	o.log.Info("ProcessRetry: processing retry",
		zap.String("payout_id", payout.ID.String()),
		zap.String("retry_type", string(retryType)),
		zap.String("provider", string(provider)),
		zap.Int("global_retry_count", payout.GlobalRetryCount),
		zap.Int("provider_retry_count", payout.ProviderRetryCount),
	)

	var result SendPayoutResult

	switch retryType {
	case queue.RetryTypeProvider:
		result = o.retryWithProvider(ctx, payout, provider)
	case queue.RetryTypeGlobal:
		result = o.retryWithFreshSelection(ctx, payout)
	default:
		return result, errors.New("unknown retry type")
	}

	// Update payout state based on result
	if result.Success {
		// Update to success state (the caller should handle this via UpdatePayoutState)
		o.log.Info("ProcessRetry: retry succeeded",
			zap.String("payout_id", payout.ID.String()),
			zap.String("provider", string(result.UsedProvider)),
		)
	} else {
		// Queue next retry or mark as failed
		o.handleRetryFailure(ctx, payout, result, retryType, provider)
	}

	return result, nil
}

// retryWithProvider retries the payout with the same provider.
func (o *Orchestrator) retryWithProvider(ctx context.Context, payout domain.Payout, providerName domain.Provider) SendPayoutResult {
	result := SendPayoutResult{TriedProviders: []domain.Provider{providerName}}

	client, err := o.registry.GetByProvider(providerName)
	if err != nil {
		o.log.Error("retryWithProvider: provider not found",
			zap.String("payout_id", payout.ID.String()),
			zap.String("provider", string(providerName)),
			zap.Error(err),
		)
		return result
	}

	extID, err := client.SendPayout(ctx, payout)
	if err == nil {
		o.successTracker.RecordSuccess(string(payout.Rail), string(providerName))
		result.ExternalID = extID
		result.UsedProvider = providerName
		result.Success = true
		return result
	}

	o.successTracker.RecordFailure(string(payout.Rail), string(providerName))
	o.log.Warn("retryWithProvider: provider failed",
		zap.String("payout_id", payout.ID.String()),
		zap.String("provider", string(providerName)),
		zap.Error(err),
	)

	return result
}

// retryWithFreshSelection starts fresh provider selection.
func (o *Orchestrator) retryWithFreshSelection(ctx context.Context, payout domain.Payout) SendPayoutResult {
	// Reset provider retry count since we're starting fresh
	payout.ProviderRetryCount = 0

	// Use existing SendPayoutWithFallback logic
	return o.SendPayoutWithFallback(ctx, payout)
}

// handleRetryFailure handles the failure of a retry attempt.
// It decides whether to queue another retry or send to DLQ.
func (o *Orchestrator) handleRetryFailure(
	ctx context.Context,
	payout domain.Payout,
	result SendPayoutResult,
	retryType queue.RetryType,
	provider domain.Provider,
) {
	switch retryType {
	case queue.RetryTypeProvider:
		// Check if we can retry the provider again
		if payout.ProviderRetryCount < o.retryCfg.Provider.MaxAttempts {
			// Queue another provider retry
			o.queueProviderRetry(ctx, payout, provider)
		} else {
			// Provider retries exhausted, try global retry
			o.queueGlobalRetry(ctx, payout)
		}

	case queue.RetryTypeGlobal:
		// Check if we can do another global retry
		if payout.GlobalRetryCount < o.retryCfg.Global.MaxAttempts {
			// Queue another global retry
			o.queueGlobalRetry(ctx, payout)
		} else {
			// All retries exhausted, send to DLQ
			o.sendToDLQ(ctx, payout)
		}
	}
}

// queueProviderRetry queues a provider-level retry.
func (o *Orchestrator) queueProviderRetry(ctx context.Context, payout domain.Payout, provider domain.Provider) {
	msg := queue.RetryMessage{
		PayoutID:     payout.ID.String(),
		Attempt:      payout.ProviderRetryCount + 1,
		MaxAttempts:  o.retryCfg.Provider.MaxAttempts,
		RetryType:    queue.RetryTypeProvider,
		Provider:     provider,
	}

	if err := o.publisher.PublishProviderRetry(ctx, msg); err != nil {
		o.log.Error("queueProviderRetry: failed to publish",
			zap.String("payout_id", payout.ID.String()),
			zap.Error(err),
		)
	}
}

// queueGlobalRetry queues a global retry.
func (o *Orchestrator) queueGlobalRetry(ctx context.Context, payout domain.Payout) {
	msg := queue.RetryMessage{
		PayoutID:    payout.ID.String(),
		Attempt:     payout.GlobalRetryCount + 1,
		MaxAttempts: o.retryCfg.Global.MaxAttempts,
		RetryType:   queue.RetryTypeGlobal,
	}

	if err := o.publisher.PublishGlobalRetry(ctx, msg); err != nil {
		o.log.Error("queueGlobalRetry: failed to publish",
			zap.String("payout_id", payout.ID.String()),
			zap.Error(err),
		)
	}
}

// sendToDLQ sends the payout to the dead letter queue.
func (o *Orchestrator) sendToDLQ(ctx context.Context, payout domain.Payout) {
	msg := queue.RetryMessage{
		PayoutID: payout.ID.String(),
		Attempt:  payout.GlobalRetryCount,
	}

	if err := o.publisher.PublishDLQ(ctx, msg); err != nil {
		o.log.Error("sendToDLQ: failed to publish",
			zap.String("payout_id", payout.ID.String()),
			zap.Error(err),
		)
	}
}
