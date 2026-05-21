package provider_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bashkirian/fintech-project/services/orchestrator/internal/domain"
	"github.com/bashkirian/fintech-project/services/orchestrator/internal/provider"
	"github.com/bashkirian/fintech-project/services/orchestrator/internal/provider/mock"
)

func TestRegistry_GetRegisteredRail(t *testing.T) {
	reg := provider.NewRegistry()
	reg.Register(domain.RailCard, &mock.Provider{})

	client, err := reg.Get(domain.RailCard)
	require.NoError(t, err)
	assert.NotNil(t, client)
}

func TestRegistry_GetUnregisteredRail(t *testing.T) {
	reg := provider.NewRegistry()

	_, err := reg.Get(domain.RailCrypto)
	assert.ErrorContains(t, err, "no active client registered for rail")
}

func TestRegistry_GetProviders_ReturnsActiveOnly(t *testing.T) {
	reg := provider.NewRegistry()
	reg.RegisterWithMeta(domain.RailCard, &mock.Provider{}, domain.ProviderMeta{
		Provider: domain.ProviderStripe,
		IsActive: true,
	})
	reg.RegisterWithMeta(domain.RailCard, &mock.Provider{}, domain.ProviderMeta{
		Provider: domain.ProviderMockCard,
		IsActive: false,
	})

	providers := reg.GetProviders(domain.RailCard)
	require.Len(t, providers, 1)
	assert.Equal(t, domain.ProviderStripe, providers[0].Meta.Provider)
}

func TestRegistry_GetProviders_OrderedByRegistration(t *testing.T) {
	reg := provider.NewRegistry()
	reg.RegisterWithMeta(domain.RailCard, &mock.Provider{}, domain.ProviderMeta{
		Provider: domain.ProviderStripe,
		IsActive: true,
	})
	reg.RegisterWithMeta(domain.RailCard, &mock.Provider{}, domain.ProviderMeta{
		Provider: domain.Provider("adyen"),
		IsActive: true,
	})
	reg.RegisterWithMeta(domain.RailCard, &mock.Provider{}, domain.ProviderMeta{
		Provider: domain.ProviderMockCard,
		IsActive: true,
	})

	providers := reg.GetProviders(domain.RailCard)
	require.Len(t, providers, 3)

	// Order should match registration order
	assert.Equal(t, domain.ProviderStripe, providers[0].Meta.Provider)
	assert.Equal(t, domain.Provider("adyen"), providers[1].Meta.Provider)
	assert.Equal(t, domain.ProviderMockCard, providers[2].Meta.Provider)
}

func TestRegistry_SetActive(t *testing.T) {
	reg := provider.NewRegistry()
	reg.RegisterWithMeta(domain.RailCard, &mock.Provider{}, domain.ProviderMeta{
		Provider: domain.ProviderStripe,
		IsActive: true,
	})
	reg.RegisterWithMeta(domain.RailCard, &mock.Provider{}, domain.ProviderMeta{
		Provider: domain.ProviderMockCard,
		IsActive: true,
	})

	// Disable Stripe
	ok := reg.SetActive(domain.RailCard, domain.ProviderStripe, false)
	assert.True(t, ok)

	providers := reg.GetProviders(domain.RailCard)
	require.Len(t, providers, 1)
	assert.Equal(t, domain.ProviderMockCard, providers[0].Meta.Provider)

	// Enable Stripe back
	ok = reg.SetActive(domain.RailCard, domain.ProviderStripe, true)
	assert.True(t, ok)

	providers = reg.GetProviders(domain.RailCard)
	require.Len(t, providers, 2)
}

func TestRegistry_SetActive_UnknownProvider(t *testing.T) {
	reg := provider.NewRegistry()
	reg.RegisterWithMeta(domain.RailCard, &mock.Provider{}, domain.ProviderMeta{
		Provider: domain.ProviderStripe,
		IsActive: true,
	})

	ok := reg.SetActive(domain.RailCard, domain.Provider("unknown"), false)
	assert.False(t, ok)
}

func TestMockProvider_SendPayout_Success(t *testing.T) {
	p := &mock.Provider{}
	payout := domain.Payout{ID: uuid.MustParse("00000000-0000-0000-0000-000000000001")}

	extID, err := p.SendPayout(context.Background(), payout)
	require.NoError(t, err)
	assert.Equal(t, "mock-ext-00000000-0000-0000-0000-000000000001", extID)
}

func TestMockProvider_SendPayout_Error(t *testing.T) {
	p := &mock.Provider{SendErr: assert.AnError}

	_, err := p.SendPayout(context.Background(), domain.Payout{})
	assert.ErrorIs(t, err, assert.AnError)
}

func TestMockProvider_CancelPayout_Supported(t *testing.T) {
	p := &mock.Provider{}

	err := p.CancelPayout(context.Background(), domain.Payout{})
	assert.NoError(t, err)
}

func TestMockProvider_CancelPayout_Unsupported(t *testing.T) {
	p := &mock.Provider{CancelErr: provider.ErrCancelNotSupported}

	err := p.CancelPayout(context.Background(), domain.Payout{})
	assert.ErrorIs(t, err, provider.ErrCancelNotSupported)
}

func TestRegistry_ProviderRoundtrip(t *testing.T) {
	reg := provider.NewRegistry()
	reg.RegisterWithMeta(domain.RailCard, &mock.Provider{}, domain.ProviderMeta{
		Provider: domain.ProviderStripe,
		IsActive: true,
	})
	reg.RegisterWithMeta(domain.RailCrypto, &mock.Provider{CancelErr: provider.ErrCancelNotSupported}, domain.ProviderMeta{
		Provider: domain.ProviderCryptoSim,
		IsActive: true,
	})

	cardClient, err := reg.Get(domain.RailCard)
	require.NoError(t, err)

	cryptoClient, err := reg.Get(domain.RailCrypto)
	require.NoError(t, err)

	payout := domain.Payout{ID: uuid.New()}

	_, sendErr := cardClient.SendPayout(context.Background(), payout)
	assert.NoError(t, sendErr)

	cancelErr := cryptoClient.CancelPayout(context.Background(), payout)
	assert.ErrorIs(t, cancelErr, provider.ErrCancelNotSupported)
}
