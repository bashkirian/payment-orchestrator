package provider

import (
	"fmt"

	"github.com/bashkirian/fintech-project/services/orchestrator/internal/domain"
)

// ProviderWithMeta associates a provider client with its routing metadata.
type ProviderWithMeta struct {
	Client Client
	Meta   domain.ProviderMeta
}

// Registry maps payment rails to their provider clients with routing metadata.
// Multiple providers can be registered per rail. Order of registration defines
// priority: first registered = tried first (like Hyperswitch's Priority routing).
type Registry struct {
	providers map[domain.Rail][]ProviderWithMeta
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{providers: make(map[domain.Rail][]ProviderWithMeta)}
}

// Register associates a rail with a provider client using default metadata.
func (r *Registry) Register(rail domain.Rail, client Client) {
	r.RegisterWithMeta(rail, client, domain.ProviderMeta{IsActive: true})
}

// RegisterWithMeta associates a rail with a provider client and explicit metadata.
// Order of registration defines priority: first = highest priority.
func (r *Registry) RegisterWithMeta(rail domain.Rail, client Client, meta domain.ProviderMeta) {
	entry := ProviderWithMeta{Client: client, Meta: meta}
	r.providers[rail] = append(r.providers[rail], entry)
}

// Get returns the first active provider client for the given rail.
// Returns an error if no provider is registered or all are inactive.
func (r *Registry) Get(rail domain.Rail) (Client, error) {
	providers := r.GetProviders(rail)
	if len(providers) == 0 {
		return nil, fmt.Errorf("provider: no active client registered for rail %q", rail)
	}
	return providers[0].Client, nil
}

// GetProviders returns all providers for a rail in registration order.
// Only active providers are included.
func (r *Registry) GetProviders(rail domain.Rail) []ProviderWithMeta {
	entries, ok := r.providers[rail]
	if !ok {
		return nil
	}

	result := make([]ProviderWithMeta, 0, len(entries))
	for _, e := range entries {
		if e.Meta.IsActive {
			result = append(result, e)
		}
	}
	return result
}

// SetActive toggles a provider's active status at runtime.
func (r *Registry) SetActive(rail domain.Rail, provider domain.Provider, active bool) bool {
	entries, ok := r.providers[rail]
	if !ok {
		return false
	}

	for i, e := range entries {
		if e.Meta.Provider == provider {
			r.providers[rail][i].Meta.IsActive = active
			return true
		}
	}
	return false
}
