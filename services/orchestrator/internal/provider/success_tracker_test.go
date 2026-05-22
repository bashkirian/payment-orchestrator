package provider_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bashkirian/fintech-project/services/orchestrator/internal/provider"
)

func TestSuccessTracker_RecordSuccess(t *testing.T) {
	tracker := provider.NewSuccessTracker()

	tracker.RecordSuccess("card", "stripe")

	success, fail, ok := tracker.GetStats("card", "stripe")
	assert.True(t, ok)
	assert.Equal(t, int64(1), success)
	assert.Equal(t, int64(0), fail)
}

func TestSuccessTracker_RecordFailure(t *testing.T) {
	tracker := provider.NewSuccessTracker()

	tracker.RecordFailure("card", "stripe")

	success, fail, ok := tracker.GetStats("card", "stripe")
	assert.True(t, ok)
	assert.Equal(t, int64(0), success)
	assert.Equal(t, int64(1), fail)
}

func TestSuccessTracker_GetSuccessRate_NoData(t *testing.T) {
	tracker := provider.NewSuccessTracker()

	// No data should return default rate
	rate := tracker.GetSuccessRate("card", "stripe")
	assert.Equal(t, provider.DefaultSuccessRate, rate)
}

func TestSuccessTracker_GetSuccessRate_WithSamples(t *testing.T) {
	tracker := provider.NewSuccessTracker()

	// 7 successes, 3 failures = 70% success rate
	for i := 0; i < 7; i++ {
		tracker.RecordSuccess("card", "stripe")
	}
	for i := 0; i < 3; i++ {
		tracker.RecordFailure("card", "stripe")
	}

	rate := tracker.GetSuccessRate("card", "stripe")
	assert.InDelta(t, 0.7, rate, 0.001)
}

func TestSuccessTracker_GetSuccessRate_AllSuccesses(t *testing.T) {
	tracker := provider.NewSuccessTracker()

	for i := 0; i < 10; i++ {
		tracker.RecordSuccess("card", "stripe")
	}

	rate := tracker.GetSuccessRate("card", "stripe")
	assert.Equal(t, 1.0, rate)
}

func TestSuccessTracker_GetSuccessRate_AllFailures(t *testing.T) {
	tracker := provider.NewSuccessTracker()

	for i := 0; i < 10; i++ {
		tracker.RecordFailure("card", "stripe")
	}

	rate := tracker.GetSuccessRate("card", "stripe")
	assert.Equal(t, 0.0, rate)
}

func TestSuccessTracker_MultipleProviders(t *testing.T) {
	tracker := provider.NewSuccessTracker()

	// stripe: 80% success (8/10)
	for i := 0; i < 8; i++ {
		tracker.RecordSuccess("card", "stripe")
	}
	for i := 0; i < 2; i++ {
		tracker.RecordFailure("card", "stripe")
	}

	// adyen: 60% success (6/10)
	for i := 0; i < 6; i++ {
		tracker.RecordSuccess("card", "adyen")
	}
	for i := 0; i < 4; i++ {
		tracker.RecordFailure("card", "adyen")
	}

	stripeRate := tracker.GetSuccessRate("card", "stripe")
	adyenRate := tracker.GetSuccessRate("card", "adyen")

	assert.InDelta(t, 0.8, stripeRate, 0.001)
	assert.InDelta(t, 0.6, adyenRate, 0.001)
}

func TestSuccessTracker_GetStats_NotFound(t *testing.T) {
	tracker := provider.NewSuccessTracker()

	success, fail, ok := tracker.GetStats("card", "unknown")
	assert.False(t, ok)
	assert.Equal(t, int64(0), success)
	assert.Equal(t, int64(0), fail)
}
