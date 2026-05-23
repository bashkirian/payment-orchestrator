package provider

import (
	"context"
	"errors"

	"github.com/bashkirian/fintech-project/services/orchestrator/internal/domain"
	"go.uber.org/zap"
)

// Orchestrator handles payout execution with automatic fallback on failure.
type Orchestrator struct {
	registry       *Registry
	router         *Router
	successTracker *SuccessTracker
	log            *zap.Logger
	routingAlgo    RoutingAlgorithm
}

// NewOrchestrator creates a new Orchestrator with the given routing algorithm.
func NewOrchestrator(
	registry *Registry,
	router *Router,
	successTracker *SuccessTracker,
	log *zap.Logger,
	routingAlgo RoutingAlgorithm,
) *Orchestrator {
	return &Orchestrator{
		registry:       registry,
		router:         router,
		successTracker: successTracker,
		log:            log,
		routingAlgo:    routingAlgo,
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

	providers := o.registry.GetProviders(payout.Rail)
	if len(providers) == 0 {
		o.log.Error("no providers available for rail",
			zap.String("rail", string(payout.Rail)),
			zap.String("payout_id", payout.ID.String()),
		)
		return result
	}

	var lastErr error
	for _, p := range providers {
		providerName := p.Meta.Provider
		result.TriedProviders = append(result.TriedProviders, providerName)

		o.log.Info("attempting payout with provider",
			zap.String("payout_id", payout.ID.String()),
			zap.String("provider", string(providerName)),
			zap.String("rail", string(payout.Rail)),
		)

		extID, err := p.Client.SendPayout(ctx, payout)
		if err == nil {
			// Success
			o.successTracker.RecordSuccess(string(payout.Rail), string(providerName))
			o.log.Info("payout succeeded",
				zap.String("payout_id", payout.ID.String()),
				zap.String("provider", string(providerName)),
				zap.String("external_id", extID),
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
	client, err := o.registry.GetByProvider(payout.Provider)
	if err != nil {
		return err
	}

	return client.CancelPayout(ctx, payout)
}

// providerNames converts a slice of providers to strings for logging.
func providerNames(providers []domain.Provider) []string {
	names := make([]string, len(providers))
	for i, p := range providers {
		names[i] = string(p)
	}
	return names
}
