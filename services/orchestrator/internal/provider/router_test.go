package provider_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bashkirian/fintech-project/services/orchestrator/internal/domain"
	"github.com/bashkirian/fintech-project/services/orchestrator/internal/provider"
	"github.com/bashkirian/fintech-project/services/orchestrator/internal/provider/mock"
)

func TestRouter_SelectProvider_Priority(t *testing.T) {
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
	router := provider.NewRouter(reg, tracker, 10)

	client, prov, err := router.SelectProvider(domain.RailCard, provider.RoutingPriority)
	require.NoError(t, err)
	assert.NotNil(t, client)
	assert.Equal(t, domain.ProviderStripe, prov) // First in list = highest priority
}

func TestRouter_SelectProvider_Weighted(t *testing.T) {
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
	router := provider.NewRouter(reg, tracker, 10)

	// Run multiple times to verify randomness doesn't panic
	for i := 0; i < 100; i++ {
		client, prov, err := router.SelectProvider(domain.RailCard, provider.RoutingWeighted)
		require.NoError(t, err)
		assert.NotNil(t, client)
		assert.Contains(t, []domain.Provider{domain.ProviderStripe, domain.ProviderMockCard}, prov)
	}
}

func TestRouter_SelectProvider_SuccessBased_NoSamples(t *testing.T) {
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
	router := provider.NewRouter(reg, tracker, 10) // min samples = 10

	// No samples yet, should return first provider (priority)
	client, prov, err := router.SelectProvider(domain.RailCard, provider.RoutingSuccessBased)
	require.NoError(t, err)
	assert.NotNil(t, client)
	assert.Equal(t, domain.ProviderStripe, prov)
}

func TestRouter_SelectProvider_SuccessBased_WithSamples(t *testing.T) {
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

	router := provider.NewRouter(reg, tracker, 10)

	client, prov, err := router.SelectProvider(domain.RailCard, provider.RoutingSuccessBased)
	require.NoError(t, err)
	assert.NotNil(t, client)
	assert.Equal(t, domain.ProviderMockCard, prov) // Higher success rate
}

func TestRouter_SelectProvider_SuccessBased_InsufficientSamples(t *testing.T) {
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

	// Only 5 samples (< min 10)
	for i := 0; i < 5; i++ {
		tracker.RecordSuccess("card", "mock_card")
	}

	router := provider.NewRouter(reg, tracker, 10)

	// Should fall back to priority (first provider)
	client, prov, err := router.SelectProvider(domain.RailCard, provider.RoutingSuccessBased)
	require.NoError(t, err)
	assert.NotNil(t, client)
	assert.Equal(t, domain.ProviderStripe, prov)
}

func TestRouter_SelectProvider_NoProviders(t *testing.T) {
	reg := provider.NewRegistry()
	tracker := provider.NewSuccessTracker()
	router := provider.NewRouter(reg, tracker, 10)

	_, _, err := router.SelectProvider(domain.RailCard, provider.RoutingPriority)
	assert.ErrorIs(t, err, provider.ErrNoProviders)
}

func TestRouter_SelectProvider_DefaultAlgorithm(t *testing.T) {
	reg := provider.NewRegistry()
	reg.RegisterWithMeta(domain.RailCard, &mock.Provider{}, domain.ProviderMeta{
		Provider: domain.ProviderStripe,
		IsActive: true,
	})

	tracker := provider.NewSuccessTracker()
	router := provider.NewRouter(reg, tracker, 10)

	// Unknown algorithm should default to priority
	client, prov, err := router.SelectProvider(domain.RailCard, provider.RoutingAlgorithm("unknown"))
	require.NoError(t, err)
	assert.NotNil(t, client)
	assert.Equal(t, domain.ProviderStripe, prov)
}

func TestRouter_SelectProvider_InactiveProviderSkipped(t *testing.T) {
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
	router := provider.NewRouter(reg, tracker, 10)

	client, prov, err := router.SelectProvider(domain.RailCard, provider.RoutingPriority)
	require.NoError(t, err)
	assert.NotNil(t, client)
	assert.Equal(t, domain.ProviderMockCard, prov) // Stripe was filtered out
}
