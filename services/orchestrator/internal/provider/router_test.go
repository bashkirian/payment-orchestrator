package provider_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bashkirian/fintech-project/services/orchestrator/internal/domain"
	"github.com/bashkirian/fintech-project/services/orchestrator/internal/provider"
	"github.com/bashkirian/fintech-project/services/orchestrator/internal/provider/mock"
)

func TestRouter_SelectProviders_Priority(t *testing.T) {
	reg := provider.NewRegistry()
	reg.RegisterWithMeta(domain.RailCard, &mock.Provider{}, domain.ProviderMeta{
		Provider: domain.ProviderStripe,
		IsActive: true,
	})
	reg.RegisterWithMeta(domain.RailCard, &mock.Provider{}, domain.ProviderMeta{
		Provider: domain.ProviderMockCard,
		IsActive: true,
	})

	tracker := provider.NewSuccessTracker()
	router := provider.NewRouter(reg, tracker, 10, zap.NewNop())

	providers, err := router.SelectProviders(domain.RailCard, provider.RoutingPriority)
	require.NoError(t, err)
	assert.Len(t, providers, 2)
	assert.Equal(t, domain.ProviderStripe, providers[0].Meta.Provider)   // First in list
	assert.Equal(t, domain.ProviderMockCard, providers[1].Meta.Provider) // Second in list
}

func TestRouter_SelectProviders_Weighted(t *testing.T) {
	reg := provider.NewRegistry()
	reg.RegisterWithMeta(domain.RailCard, &mock.Provider{}, domain.ProviderMeta{
		Provider: domain.ProviderStripe,
		IsActive: true,
		Weight:   70,
	})
	reg.RegisterWithMeta(domain.RailCard, &mock.Provider{}, domain.ProviderMeta{
		Provider: domain.ProviderMockCard,
		IsActive: true,
		Weight:   30,
	})

	tracker := provider.NewSuccessTracker()
	router := provider.NewRouter(reg, tracker, 10, zap.NewNop())

	// Run multiple times to verify weighted selection works
	// Count how many times each provider is selected first
	stripeFirst := 0
	mockFirst := 0
	for i := 0; i < 1000; i++ {
		providers, err := router.SelectProviders(domain.RailCard, provider.RoutingWeighted)
		require.NoError(t, err)
		assert.Len(t, providers, 2)
		if providers[0].Meta.Provider == domain.ProviderStripe {
			stripeFirst++
		} else {
			mockFirst++
		}
	}

	// With 70/30 weights, stripe should be first ~70% of the time
	// Allow some variance: 65-75%
	assert.Greater(t, stripeFirst, 600, "stripe should be selected first ~70%% of the time (got %d/1000)", stripeFirst)
	assert.Less(t, stripeFirst, 800, "stripe should be selected first ~70%% of the time (got %d/1000)", stripeFirst)
}

func TestRouter_SelectProviders_Weighted_RemainderDistribution(t *testing.T) {
	reg := provider.NewRegistry()
	reg.RegisterWithMeta(domain.RailCard, &mock.Provider{}, domain.ProviderMeta{
		Provider: domain.ProviderStripe,
		IsActive: true,
		Weight:   30, // 30% explicit
	})
	reg.RegisterWithMeta(domain.RailCard, &mock.Provider{}, domain.ProviderMeta{
		Provider: domain.ProviderMockCard,
		IsActive: true,
		// No weight - should get 70% (remainder)
	})

	tracker := provider.NewSuccessTracker()
	router := provider.NewRouter(reg, tracker, 10, zap.NewNop())

	// Run multiple times to verify remainder distribution
	stripeFirst := 0
	mockFirst := 0
	for i := 0; i < 1000; i++ {
		providers, err := router.SelectProviders(domain.RailCard, provider.RoutingWeighted)
		require.NoError(t, err)
		if providers[0].Meta.Provider == domain.ProviderStripe {
			stripeFirst++
		} else {
			mockFirst++
		}
	}

	// stripe: 30%, mock_card: 70% (remainder)
	assert.Greater(t, stripeFirst, 200, "stripe should be selected first ~30%% (got %d/1000)", stripeFirst)
	assert.Less(t, stripeFirst, 400, "stripe should be selected first ~30%% (got %d/1000)", stripeFirst)
}

func TestRouter_SelectProviders_Weighted_NoExplicitWeights(t *testing.T) {
	reg := provider.NewRegistry()
	reg.RegisterWithMeta(domain.RailCard, &mock.Provider{}, domain.ProviderMeta{
		Provider: domain.ProviderStripe,
		IsActive: true,
		// No weight
	})
	reg.RegisterWithMeta(domain.RailCard, &mock.Provider{}, domain.ProviderMeta{
		Provider: domain.ProviderMockCard,
		IsActive: true,
		// No weight
	})

	tracker := provider.NewSuccessTracker()
	router := provider.NewRouter(reg, tracker, 10, zap.NewNop())

	// Should be 50/50 distribution
	stripeFirst := 0
	mockFirst := 0
	for i := 0; i < 1000; i++ {
		providers, err := router.SelectProviders(domain.RailCard, provider.RoutingWeighted)
		require.NoError(t, err)
		if providers[0].Meta.Provider == domain.ProviderStripe {
			stripeFirst++
		} else {
			mockFirst++
		}
	}

	// Should be roughly 50/50
	assert.Greater(t, stripeFirst, 400, "should be ~50%% (got %d/1000)", stripeFirst)
	assert.Less(t, stripeFirst, 600, "should be ~50%% (got %d/1000)", stripeFirst)
}

func TestRouter_SelectProviders_SuccessBased_NoSamples(t *testing.T) {
	reg := provider.NewRegistry()
	reg.RegisterWithMeta(domain.RailCard, &mock.Provider{}, domain.ProviderMeta{
		Provider: domain.ProviderStripe,
		IsActive: true,
	})
	reg.RegisterWithMeta(domain.RailCard, &mock.Provider{}, domain.ProviderMeta{
		Provider: domain.ProviderMockCard,
		IsActive: true,
	})

	tracker := provider.NewSuccessTracker()
	router := provider.NewRouter(reg, tracker, 10, zap.NewNop()) // min samples = 10

	// No samples yet, should return providers in original order (priority)
	providers, err := router.SelectProviders(domain.RailCard, provider.RoutingSuccessBased)
	require.NoError(t, err)
	assert.Len(t, providers, 2)
	assert.Equal(t, domain.ProviderStripe, providers[0].Meta.Provider)
}

func TestRouter_SelectProviders_SuccessBased_WithSamples(t *testing.T) {
	reg := provider.NewRegistry()
	reg.RegisterWithMeta(domain.RailCard, &mock.Provider{}, domain.ProviderMeta{
		Provider: domain.ProviderStripe,
		IsActive: true,
	})
	reg.RegisterWithMeta(domain.RailCard, &mock.Provider{}, domain.ProviderMeta{
		Provider: domain.ProviderMockCard,
		IsActive: true,
	})

	tracker := provider.NewSuccessTracker()

	// stripe: 50% success rate (5/10)
	for i := 0; i < 5; i++ {
		tracker.RecordSuccess("card", "stripe")
	}
	for i := 0; i < 5; i++ {
		tracker.RecordFailure("card", "stripe")
	}

	// mock_card: 90% success rate (9/10)
	for i := 0; i < 9; i++ {
		tracker.RecordSuccess("card", "mock_card")
	}
	tracker.RecordFailure("card", "mock_card")

	router := provider.NewRouter(reg, tracker, 10, zap.NewNop())

	providers, err := router.SelectProviders(domain.RailCard, provider.RoutingSuccessBased)
	require.NoError(t, err)
	assert.Len(t, providers, 2)
	// mock_card has higher success rate, should be first
	assert.Equal(t, domain.ProviderMockCard, providers[0].Meta.Provider)
	assert.Equal(t, domain.ProviderStripe, providers[1].Meta.Provider)
}

func TestRouter_SelectProviders_SuccessBased_InsufficientSamples(t *testing.T) {
	reg := provider.NewRegistry()
	reg.RegisterWithMeta(domain.RailCard, &mock.Provider{}, domain.ProviderMeta{
		Provider: domain.ProviderStripe,
		IsActive: true,
	})
	reg.RegisterWithMeta(domain.RailCard, &mock.Provider{}, domain.ProviderMeta{
		Provider: domain.ProviderMockCard,
		IsActive: true,
	})

	tracker := provider.NewSuccessTracker()

	// Only 5 samples for mock_card (< min 10)
	for i := 0; i < 5; i++ {
		tracker.RecordSuccess("card", "mock_card")
	}

	router := provider.NewRouter(reg, tracker, 10, zap.NewNop())

	// Should fall back to priority order (stripe first)
	providers, err := router.SelectProviders(domain.RailCard, provider.RoutingSuccessBased)
	require.NoError(t, err)
	assert.Equal(t, domain.ProviderStripe, providers[0].Meta.Provider)
}

func TestRouter_SelectProviders_NoProviders(t *testing.T) {
	reg := provider.NewRegistry()
	tracker := provider.NewSuccessTracker()
	router := provider.NewRouter(reg, tracker, 10, zap.NewNop())

	_, err := router.SelectProviders(domain.RailCard, provider.RoutingPriority)
	assert.ErrorIs(t, err, provider.ErrNoProviders)
}

func TestRouter_SelectProviders_DefaultAlgorithm(t *testing.T) {
	reg := provider.NewRegistry()
	reg.RegisterWithMeta(domain.RailCard, &mock.Provider{}, domain.ProviderMeta{
		Provider: domain.ProviderStripe,
		IsActive: true,
	})

	tracker := provider.NewSuccessTracker()
	router := provider.NewRouter(reg, tracker, 10, zap.NewNop())

	// Unknown algorithm should default to priority
	providers, err := router.SelectProviders(domain.RailCard, provider.RoutingAlgorithm("unknown"))
	require.NoError(t, err)
	assert.Len(t, providers, 1)
	assert.Equal(t, domain.ProviderStripe, providers[0].Meta.Provider)
}

func TestRouter_SelectProviders_InactiveProviderSkipped(t *testing.T) {
	reg := provider.NewRegistry()
	reg.RegisterWithMeta(domain.RailCard, &mock.Provider{}, domain.ProviderMeta{
		Provider: domain.ProviderStripe,
		IsActive: false, // inactive
	})
	reg.RegisterWithMeta(domain.RailCard, &mock.Provider{}, domain.ProviderMeta{
		Provider: domain.ProviderMockCard,
		IsActive: true,
	})

	tracker := provider.NewSuccessTracker()
	router := provider.NewRouter(reg, tracker, 10, zap.NewNop())

	providers, err := router.SelectProviders(domain.RailCard, provider.RoutingPriority)
	require.NoError(t, err)
	assert.Len(t, providers, 1)
	assert.Equal(t, domain.ProviderMockCard, providers[0].Meta.Provider) // Stripe was filtered out
}

func TestRouter_SelectProviders_Weighted_AllProvidersReturned(t *testing.T) {
	reg := provider.NewRegistry()
	reg.RegisterWithMeta(domain.RailCard, &mock.Provider{}, domain.ProviderMeta{
		Provider: domain.ProviderStripe,
		IsActive: true,
		Weight:   70,
	})
	reg.RegisterWithMeta(domain.RailCard, &mock.Provider{}, domain.ProviderMeta{
		Provider: domain.ProviderMockCard,
		IsActive: true,
		Weight:   30,
	})

	tracker := provider.NewSuccessTracker()
	router := provider.NewRouter(reg, tracker, 10, zap.NewNop())

	providers, err := router.SelectProviders(domain.RailCard, provider.RoutingWeighted)
	require.NoError(t, err)
	assert.Len(t, providers, 2, "all providers should be returned for fallback")
	// Both providers should be present, just in different order
	providerNames := []domain.Provider{providers[0].Meta.Provider, providers[1].Meta.Provider}
	assert.Contains(t, providerNames, domain.ProviderStripe)
	assert.Contains(t, providerNames, domain.ProviderMockCard)
}
