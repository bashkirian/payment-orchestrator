package provider

import (
	"fmt"

	"github.com/bashkirian/fintech-project/services/orchestrator/internal/domain"
)

// Registry maps payment rails to their provider clients.
type Registry struct {
	clients map[domain.Rail]Client
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{clients: make(map[domain.Rail]Client)}
}

// Register associates a rail with a provider client.
// Panics if the same rail is registered twice — this is a programming error
// that should be caught at startup, not at runtime.
func (r *Registry) Register(rail domain.Rail, client Client) {
	if _, exists := r.clients[rail]; exists {
		panic(fmt.Sprintf("provider: rail %q already registered", rail))
	}
	r.clients[rail] = client
}

// Get returns the provider client for the given rail.
// Returns an error if no provider is registered for that rail.
func (r *Registry) Get(rail domain.Rail) (Client, error) {
	c, ok := r.clients[rail]
	if !ok {
		return nil, fmt.Errorf("provider: no client registered for rail %q", rail)
	}
	return c, nil
}
