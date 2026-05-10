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
	assert.ErrorContains(t, err, "no client registered for rail")
}

func TestRegistry_DuplicateRegisterPanics(t *testing.T) {
	reg := provider.NewRegistry()
	reg.Register(domain.RailCard, &mock.Provider{})

	assert.Panics(t, func() {
		reg.Register(domain.RailCard, &mock.Provider{})
	})
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
	reg.Register(domain.RailCard, &mock.Provider{})
	reg.Register(domain.RailCrypto, &mock.Provider{CancelErr: provider.ErrCancelNotSupported})

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
