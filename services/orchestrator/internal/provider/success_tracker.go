package provider

import (
	"sync"
	"time"
)

// SuccessTracker tracks success/failure rates per (rail, provider) combination.
// It uses an in-memory rolling window and is thread-safe.
type SuccessTracker struct {
	mu    sync.RWMutex
	stats map[string]*ProviderStats // key: "rail:provider"
}

// ProviderStats holds the success/failure counts and metadata for a provider.
type ProviderStats struct {
	SuccessCount int64
	FailCount    int64
	LastUpdated  time.Time
}

// NewSuccessTracker creates a new empty SuccessTracker.
func NewSuccessTracker() *SuccessTracker {
	return &SuccessTracker{
		stats: make(map[string]*ProviderStats),
	}
}

// RecordSuccess increments the success count for the given rail and provider.
func (t *SuccessTracker) RecordSuccess(rail, provider string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	key := rail + ":" + provider
	if t.stats[key] == nil {
		t.stats[key] = &ProviderStats{}
	}
	t.stats[key].SuccessCount++
	t.stats[key].LastUpdated = time.Now()
}

// RecordFailure increments the failure count for the given rail and provider.
func (t *SuccessTracker) RecordFailure(rail, provider string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	key := rail + ":" + provider
	if t.stats[key] == nil {
		t.stats[key] = &ProviderStats{}
	}
	t.stats[key].FailCount++
	t.stats[key].LastUpdated = time.Now()
}

// GetSuccessRate returns the success rate (0.0 to 1.0) for the given rail and provider.
// Returns a default rate for providers with no data (optimistic assumption for new providers).
func (t *SuccessTracker) GetSuccessRate(rail, provider string) float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()

	key := rail + ":" + provider
	stats, ok := t.stats[key]
	if !ok {
		return DefaultSuccessRate // Optimistic default for new providers
	}

	total := stats.SuccessCount + stats.FailCount
	if total == 0 {
		return DefaultSuccessRate
	}

	return float64(stats.SuccessCount) / float64(total)
}

// GetStats returns the current stats for a provider (for testing/observability).
func (t *SuccessTracker) GetStats(rail, provider string) (success, fail int64, ok bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	key := rail + ":" + provider
	stats, ok := t.stats[key]
	if !ok {
		return 0, 0, false
	}
	return stats.SuccessCount, stats.FailCount, true
}

// DefaultSuccessRate is the assumed success rate for providers with no data.
// Optimistic value encourages trying new providers.
const DefaultSuccessRate = 0.95
