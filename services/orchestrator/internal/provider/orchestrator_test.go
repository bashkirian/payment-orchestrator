package provider_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bashkirian/fintech-project/services/orchestrator/internal/domain"
	"github.com/bashkirian/fintech-project/services/orchestrator/internal/provider"
	"github.com/bashkirian/fintech-project/services/orchestrator/internal/provider/mock"
)

func TestOrchestrator_SendPayoutWithFallback_Success(t *testing.T) {
	reg := provider.NewRegistry()
	reg.RegisterWithMeta(domain.RailCard, &mock.Provider{}, domain.ProviderMeta{
		Provider: domain.ProviderStripe,
		IsActive: true,
	})

	tracker := provider.NewSuccessTracker()
	router := provider.NewRouter(reg, tracker, 10)
	log := zap.NewNop()
	orch := provider.NewOrchestrator(reg, router, tracker, log, provider.RoutingPriority)

	payout := domain.Payout{
		ID:          uuid.New(),
		Rail:        domain.RailCard,
		AmountCents: 1000,
	}

	result := orch.SendPayoutWithFallback(context.Background(), payout)

	assert.True(t, result.Success)
	assert.NotEmpty(t, result.ExternalID)
	assert.Equal(t, domain.ProviderStripe, result.UsedProvider)
	assert.Len(t, result.TriedProviders, 1)

	// Verify success was recorded
	success, fail, ok := tracker.GetStats("card", "stripe")
	require.True(t, ok)
	assert.Equal(t, int64(1), success)
	assert.Equal(t, int64(0), fail)
}

func TestOrchestrator_SendPayoutWithFallback_FallbackOnFailure(t *testing.T) {
	reg := provider.NewRegistry()

	// First provider fails
	failingProvider := &mock.Provider{SendErr: &provider.RetryableError{Retryable: true, Err: errors.New("timeout")}}
	reg.RegisterWithMeta(domain.RailCard, failingProvider, domain.ProviderMeta{
		Provider: domain.ProviderStripe,
		IsActive: true,
	})

	// Second provider succeeds
	reg.RegisterWithMeta(domain.RailCard, &mock.Provider{}, domain.ProviderMeta{
		Provider: domain.ProviderMockCard,
		IsActive: true,
	})

	tracker := provider.NewSuccessTracker()
	router := provider.NewRouter(reg, tracker, 10)
	log := zap.NewNop()
	orch := provider.NewOrchestrator(reg, router, tracker, log, provider.RoutingPriority)

	payout := domain.Payout{
		ID:          uuid.New(),
		Rail:        domain.RailCard,
		AmountCents: 1000,
	}

	result := orch.SendPayoutWithFallback(context.Background(), payout)

	assert.True(t, result.Success)
	assert.Equal(t, domain.ProviderMockCard, result.UsedProvider)
	assert.Len(t, result.TriedProviders, 2) // Tried stripe, then mock_card

	// Verify failures/successes were recorded
	stripeSuccess, stripeFail, _ := tracker.GetStats("card", "stripe")
	assert.Equal(t, int64(0), stripeSuccess)
	assert.Equal(t, int64(1), stripeFail)

	mockSuccess, mockFail, _ := tracker.GetStats("card", "mock_card")
	assert.Equal(t, int64(1), mockSuccess)
	assert.Equal(t, int64(0), mockFail)
}

func TestOrchestrator_SendPayoutWithFallback_TerminalErrorStopsFallback(t *testing.T) {
	reg := provider.NewRegistry()

	// First provider returns terminal error (not retryable)
	terminalErr := &provider.RetryableError{Retryable: false, Err: errors.New("card declined")}
	reg.RegisterWithMeta(domain.RailCard, &mock.Provider{SendErr: terminalErr}, domain.ProviderMeta{
		Provider: domain.ProviderStripe,
		IsActive: true,
	})

	// Second provider would succeed, but shouldn't be tried
	reg.RegisterWithMeta(domain.RailCard, &mock.Provider{}, domain.ProviderMeta{
		Provider: domain.ProviderMockCard,
		IsActive: true,
	})

	tracker := provider.NewSuccessTracker()
	router := provider.NewRouter(reg, tracker, 10)
	log := zap.NewNop()
	orch := provider.NewOrchestrator(reg, router, tracker, log, provider.RoutingPriority)

	payout := domain.Payout{
		ID:          uuid.New(),
		Rail:        domain.RailCard,
		AmountCents: 1000,
	}

	result := orch.SendPayoutWithFallback(context.Background(), payout)

	assert.False(t, result.Success)
	assert.Len(t, result.TriedProviders, 1) // Only tried stripe
	assert.Equal(t, domain.ProviderStripe, result.TriedProviders[0])

	// Verify second provider was NOT tried
	_, _, mockOk := tracker.GetStats("card", "mock_card")
	assert.False(t, mockOk)
}

func TestOrchestrator_SendPayoutWithFallback_AllProvidersFail(t *testing.T) {
	reg := provider.NewRegistry()

	err := &provider.RetryableError{Retryable: true, Err: errors.New("timeout")}
	reg.RegisterWithMeta(domain.RailCard, &mock.Provider{SendErr: err}, domain.ProviderMeta{
		Provider: domain.ProviderStripe,
		IsActive: true,
	})
	reg.RegisterWithMeta(domain.RailCard, &mock.Provider{SendErr: err}, domain.ProviderMeta{
		Provider: domain.ProviderMockCard,
		IsActive: true,
	})

	tracker := provider.NewSuccessTracker()
	router := provider.NewRouter(reg, tracker, 10)
	log := zap.NewNop()
	orch := provider.NewOrchestrator(reg, router, tracker, log, provider.RoutingPriority)

	payout := domain.Payout{
		ID:          uuid.New(),
		Rail:        domain.RailCard,
		AmountCents: 1000,
	}

	result := orch.SendPayoutWithFallback(context.Background(), payout)

	assert.False(t, result.Success)
	assert.Empty(t, result.ExternalID)
	assert.Len(t, result.TriedProviders, 2)

	// Verify both failures recorded
	stripeSuccess, stripeFail, _ := tracker.GetStats("card", "stripe")
	assert.Equal(t, int64(0), stripeSuccess)
	assert.Equal(t, int64(1), stripeFail)

	mockSuccess, mockFail, _ := tracker.GetStats("card", "mock_card")
	assert.Equal(t, int64(0), mockSuccess)
	assert.Equal(t, int64(1), mockFail)
}

func TestOrchestrator_SendPayoutWithFallback_NoProviders(t *testing.T) {
	reg := provider.NewRegistry()
	tracker := provider.NewSuccessTracker()
	router := provider.NewRouter(reg, tracker, 10)
	log := zap.NewNop()
	orch := provider.NewOrchestrator(reg, router, tracker, log, provider.RoutingPriority)

	payout := domain.Payout{
		ID:          uuid.New(),
		Rail:        domain.RailCard,
		AmountCents: 1000,
	}

	result := orch.SendPayoutWithFallback(context.Background(), payout)

	assert.False(t, result.Success)
	assert.Empty(t, result.TriedProviders)
}

func TestOrchestrator_CancelPayout(t *testing.T) {
	reg := provider.NewRegistry()
	mockProvider := &mock.Provider{}
	reg.RegisterWithMeta(domain.RailCard, mockProvider, domain.ProviderMeta{
		Provider: domain.ProviderStripe,
		IsActive: true,
	})

	tracker := provider.NewSuccessTracker()
	router := provider.NewRouter(reg, tracker, 10)
	log := zap.NewNop()
	orch := provider.NewOrchestrator(reg, router, tracker, log, provider.RoutingPriority)

	externalID := "pi_test123"
	payout := domain.Payout{
		ID:          uuid.New(),
		Rail:        domain.RailCard,
		Provider:    domain.ProviderStripe,
		ExternalID:  &externalID,
		AmountCents: 1000,
	}

	err := orch.CancelPayout(context.Background(), payout)
	assert.NoError(t, err)
}

func TestOrchestrator_CancelPayout_ProviderNotFound(t *testing.T) {
	reg := provider.NewRegistry()
	tracker := provider.NewSuccessTracker()
	router := provider.NewRouter(reg, tracker, 10)
	log := zap.NewNop()
	orch := provider.NewOrchestrator(reg, router, tracker, log, provider.RoutingPriority)

	payout := domain.Payout{
		ID:          uuid.New(),
		Rail:        domain.RailCard,
		Provider:    domain.ProviderStripe, // Not registered
		AmountCents: 1000,
	}

	err := orch.CancelPayout(context.Background(), payout)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no client registered for provider")
}

func TestOrchestrator_SendPayoutWithFallback_InactiveProviderSkipped(t *testing.T) {
	reg := provider.NewRegistry()

	// First provider is inactive
	reg.RegisterWithMeta(domain.RailCard, &mock.Provider{}, domain.ProviderMeta{
		Provider: domain.ProviderStripe,
		IsActive: false,
	})

	// Second provider is active
	reg.RegisterWithMeta(domain.RailCard, &mock.Provider{}, domain.ProviderMeta{
		Provider: domain.ProviderMockCard,
		IsActive: true,
	})

	tracker := provider.NewSuccessTracker()
	router := provider.NewRouter(reg, tracker, 10)
	log := zap.NewNop()
	orch := provider.NewOrchestrator(reg, router, tracker, log, provider.RoutingPriority)

	payout := domain.Payout{
		ID:          uuid.New(),
		Rail:        domain.RailCard,
		AmountCents: 1000,
	}

	result := orch.SendPayoutWithFallback(context.Background(), payout)

	assert.True(t, result.Success)
	assert.Equal(t, domain.ProviderMockCard, result.UsedProvider)
	assert.Len(t, result.TriedProviders, 1) // Only mock_card was tried

	// Verify stripe was not tried (inactive)
	_, _, stripeOk := tracker.GetStats("card", "stripe")
	assert.False(t, stripeOk)
}
