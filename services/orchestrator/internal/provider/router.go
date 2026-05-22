package provider

import (
	"math/rand"

	"github.com/bashkirian/fintech-project/services/orchestrator/internal/domain"
)

// RoutingAlgorithm defines how to select among multiple providers for a rail.
type RoutingAlgorithm string

const (
	// RoutingPriority returns the first active provider (order in config).
	RoutingPriority RoutingAlgorithm = "priority"
	// RoutingWeighted randomly selects a provider based on weight.
	// Note: weights are equal for now; can be extended to use config weights.
	RoutingWeighted RoutingAlgorithm = "weighted"
	// RoutingSuccessBased picks the provider with the highest success rate.
	// Requires minimum samples before using real data.
	RoutingSuccessBased RoutingAlgorithm = "success_based"
)

// Router selects providers based on routing algorithms.
type Router struct {
	registry       *Registry
	successTracker *SuccessTracker
	minSamples     int // Minimum transactions before using success rate data
}

// NewRouter creates a new Router with the given registry and success tracker.
func NewRouter(registry *Registry, tracker *SuccessTracker, minSamples int) *Router {
	return &Router{
		registry:       registry,
		successTracker: tracker,
		minSamples:     minSamples,
	}
}

// SelectProvider selects a provider based on the given algorithm.
// Returns the provider client, provider identifier, and any error.
func (r *Router) SelectProvider(rail domain.Rail, algo RoutingAlgorithm) (Client, domain.Provider, error) {
	providers := r.registry.GetProviders(rail)
	if len(providers) == 0 {
		return nil, "", ErrNoProviders
	}

	switch algo {
	case RoutingPriority:
		return r.selectByPriority(providers)
	case RoutingWeighted:
		return r.selectByWeight(providers)
	case RoutingSuccessBased:
		return r.selectBySuccessRate(rail, providers)
	default:
		// Default to priority
		return r.selectByPriority(providers)
	}
}

// selectByPriority returns the first provider in the list.
func (r *Router) selectByPriority(providers []ProviderWithMeta) (Client, domain.Provider, error) {
	return providers[0].Client, providers[0].Meta.Provider, nil
}

// selectByWeight randomly selects a provider with equal weights.
func (r *Router) selectByWeight(providers []ProviderWithMeta) (Client, domain.Provider, error) {
	idx := rand.Intn(len(providers))
	return providers[idx].Client, providers[idx].Meta.Provider, nil
}

// selectBySuccessRate picks the provider with the highest success rate.
// Falls back to priority order for providers with insufficient samples.
func (r *Router) selectBySuccessRate(rail domain.Rail, providers []ProviderWithMeta) (Client, domain.Provider, error) {
	var bestProvider *ProviderWithMeta
	var bestRate float64 = -1

	for i := range providers {
		p := &providers[i]
		rate := r.successTracker.GetSuccessRate(string(rail), string(p.Meta.Provider))

		// Check if we have enough samples for this provider
		success, fail, _ := r.successTracker.GetStats(string(rail), string(p.Meta.Provider))
		total := success + fail

		// If provider has insufficient samples, use priority ordering for it
		if total < int64(r.minSamples) {
			// If no provider has enough samples yet, return first (priority)
			if bestRate < 0 {
				bestProvider = p
				bestRate = rate
			}
			continue
		}

		// Provider has enough samples - compare success rates
		if rate > bestRate {
			bestRate = rate
			bestProvider = p
		}
	}

	if bestProvider == nil {
		bestProvider = &providers[0]
	}

	return bestProvider.Client, bestProvider.Meta.Provider, nil
}

// ErrNoProviders is returned when no providers are available for a rail.
var ErrNoProviders = &NoProvidersError{}

// NoProvidersError indicates no providers are available.
type NoProvidersError struct{}

func (e *NoProvidersError) Error() string {
	return "no providers available for rail"
}
