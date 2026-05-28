package provider

import (
	"math/rand"

	"github.com/bashkirian/fintech-project/services/orchestrator/internal/domain"
	"go.uber.org/zap"
)

// RoutingAlgorithm defines how to select among multiple providers for a rail.
type RoutingAlgorithm string

func (a RoutingAlgorithm) String() string {
	return string(a)
}

const (
	// RoutingPriority returns providers in config order (first = highest priority).
	RoutingPriority RoutingAlgorithm = "priority"
	// RoutingWeighted randomly selects a provider based on weight,
	// then returns all providers with selected first (for fallback).
	RoutingWeighted RoutingAlgorithm = "weighted"
	// RoutingSuccessBased picks the provider with the highest success rate.
	// Requires minimum samples before using real data.
	// Returns all providers sorted by success rate (best first).
	RoutingSuccessBased RoutingAlgorithm = "success_based"
)

// Router selects providers based on routing algorithms.
type Router struct {
	registry       *Registry
	successTracker *SuccessTracker
	minSamples     int         // Minimum transactions before using success rate data
	log            *zap.Logger // Logger for routing decisions
}

// NewRouter creates a new Router with the given registry and success tracker.
func NewRouter(registry *Registry, tracker *SuccessTracker, minSamples int, log *zap.Logger) *Router {
	return &Router{
		registry:       registry,
		successTracker: tracker,
		minSamples:     minSamples,
		log:            log,
	}
}

// SelectProviders returns an ordered list of providers based on the routing algorithm.
// The first provider is tried first, subsequent providers are fallback options.
func (r *Router) SelectProviders(rail domain.Rail, algo RoutingAlgorithm) ([]ProviderWithMeta, error) {
	providers := r.registry.GetProviders(rail)
	if len(providers) == 0 {
		r.log.Warn("SelectProviders: no providers for rail",
			zap.String("rail", string(rail)),
		)
		return nil, ErrNoProviders
	}

	r.log.Info("SelectProviders: selecting providers",
		zap.String("rail", string(rail)),
		zap.String("algorithm", string(algo)),
		zap.Int("available_providers", len(providers)),
	)

	var result []ProviderWithMeta
	switch algo {
	case RoutingWeighted:
		result = r.selectByWeight(rail, providers)
	case RoutingSuccessBased:
		result = r.selectBySuccessRate(rail, providers)
	default:
		// RoutingPriority - return as-is (config order)
		result = providers
	}

	r.log.Info("SelectProviders: providers ordered",
		zap.String("rail", string(rail)),
		zap.String("algorithm", string(algo)),
		zap.Strings("ordered_providers", providerNamesFromMeta(result)),
	)
	return result, nil
}

// selectByWeight randomly selects a provider based on weight, puts it first.
// Providers without weight share the remaining percentage equally.
func (r *Router) selectByWeight(rail domain.Rail, providers []ProviderWithMeta) []ProviderWithMeta {
	if len(providers) == 0 {
		return providers
	}

	// Calculate effective weights
	weights := calculateEffectiveWeights(providers)

	// Weighted random selection
	totalWeight := 0
	for _, w := range weights {
		totalWeight += w
	}

	idx := weightedRandomIndex(weights, totalWeight)

	r.log.Info("selectByWeight: weighted selection",
		zap.String("rail", string(rail)),
		zap.Int("selected_index", idx),
		zap.String("selected_provider", string(providers[idx].Meta.Provider)),
		zap.Ints("weights", weights),
		zap.Int("total_weight", totalWeight),
	)

	// Move selected provider to front
	result := make([]ProviderWithMeta, len(providers))
	result[0] = providers[idx]
	j := 1
	for i, p := range providers {
		if i != idx {
			result[j] = p
			j++
		}
	}
	return result
}

// calculateEffectiveWeights computes weights for all providers.
// If some providers have explicit weights, those without weights share the remainder.
// If no provider has explicit weights, all get equal weight.
func calculateEffectiveWeights(providers []ProviderWithMeta) []int {
	weights := make([]int, len(providers))
	totalExplicit := 0
	explicitCount := 0

	for i, p := range providers {
		if p.Meta.Weight > 0 {
			weights[i] = p.Meta.Weight
			totalExplicit += p.Meta.Weight
			explicitCount++
		}
	}

	// If no explicit weights, use equal distribution
	if explicitCount == 0 {
		for i := range weights {
			weights[i] = 100 / len(providers)
		}
		// Give remainder to first provider
		weights[0] += 100 % len(providers)
		return weights
	}

	// If all have explicit weights, use as-is
	if explicitCount == len(providers) {
		return weights
	}

	// Distribute remainder among providers without explicit weight
	remainder := 100 - totalExplicit
	if remainder <= 0 {
		// Already at or over 100%, give minimum weight to unset providers
		for i, p := range providers {
			if p.Meta.Weight == 0 {
				weights[i] = 1
			}
		}
		return weights
	}

	unsetCount := len(providers) - explicitCount
	unsetWeight := remainder / unsetCount
	for i, p := range providers {
		if p.Meta.Weight == 0 {
			weights[i] = unsetWeight
		}
	}
	// Give remainder to first unset provider
	remainderRemainder := remainder % unsetCount
	if remainderRemainder > 0 {
		for i, p := range providers {
			if p.Meta.Weight == 0 {
				weights[i] += remainderRemainder
				break
			}
		}
	}

	return weights
}

// weightedRandomIndex selects an index based on weights.
func weightedRandomIndex(weights []int, totalWeight int) int {
	if totalWeight <= 0 {
		return rand.Intn(len(weights))
	}

	r := rand.Intn(totalWeight)
	sum := 0
	for i, w := range weights {
		sum += w
		if r < sum {
			return i
		}
	}
	return len(weights) - 1
}

// selectBySuccessRate returns providers sorted by success rate (best first).
// Providers without enough samples keep their original order.
func (r *Router) selectBySuccessRate(rail domain.Rail, providers []ProviderWithMeta) []ProviderWithMeta {
	if len(providers) == 0 {
		return providers
	}

	// Create a copy to sort
	result := make([]ProviderWithMeta, len(providers))
	copy(result, providers)

	// Log current stats for each provider
	for _, p := range providers {
		succ, fail, ok := r.successTracker.GetStats(string(rail), string(p.Meta.Provider))
		if ok {
			rate := r.successTracker.GetSuccessRate(string(rail), string(p.Meta.Provider))
			r.log.Info("selectBySuccessRate: provider stats",
				zap.String("rail", string(rail)),
				zap.String("provider", string(p.Meta.Provider)),
				zap.Int64("successes", succ),
				zap.Int64("failures", fail),
				zap.Float64("success_rate", rate),
				zap.Bool("has_enough_samples", (succ+fail) >= int64(r.minSamples)),
			)
		} else {
			r.log.Info("selectBySuccessRate: provider stats",
				zap.String("rail", string(rail)),
				zap.String("provider", string(p.Meta.Provider)),
				zap.Bool("has_stats", false),
			)
		}
	}

	// Sort by success rate (descending), with providers having enough samples first
	// Use stable sort to preserve order for providers without enough samples
	for i := 0; i < len(result)-1; i++ {
		for j := i + 1; j < len(result); j++ {
			// Check if both have enough samples
			succI, failI, okI := r.successTracker.GetStats(string(rail), string(result[i].Meta.Provider))
			succJ, failJ, okJ := r.successTracker.GetStats(string(rail), string(result[j].Meta.Provider))

			hasEnoughI := okI && (succI+failI) >= int64(r.minSamples)
			hasEnoughJ := okJ && (succJ+failJ) >= int64(r.minSamples)

			// Provider with enough samples comes before one without
			if hasEnoughI && !hasEnoughJ {
				continue // i is already before j, correct order
			}
			if !hasEnoughI && hasEnoughJ {
				result[i], result[j] = result[j], result[i]
				continue
			}

			// Both have enough samples - compare rates (higher rate first)
			if hasEnoughI && hasEnoughJ {
				rateI := r.successTracker.GetSuccessRate(string(rail), string(result[i].Meta.Provider))
				rateJ := r.successTracker.GetSuccessRate(string(rail), string(result[j].Meta.Provider))
				if rateJ > rateI {
					result[i], result[j] = result[j], result[i]
				}
			}
			// If both don't have enough samples, keep original order (do nothing)
		}
	}

	r.log.Info("selectBySuccessRate: providers ordered by success rate",
		zap.String("rail", string(rail)),
		zap.Strings("ordered_providers", providerNamesFromMeta(result)),
	)

	return result
}

// ErrNoProviders is returned when no providers are available for a rail.
var ErrNoProviders = &NoProvidersError{}

// NoProvidersError indicates no providers are available.
type NoProvidersError struct{}

func (e *NoProvidersError) Error() string {
	return "no providers available for rail"
}
