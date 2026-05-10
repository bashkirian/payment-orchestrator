// Package mock provides a test-only in-process provider client.
// It must not be imported from production code.
package mock

import (
	"context"
	"fmt"

	"github.com/bashkirian/fintech-project/services/orchestrator/internal/domain"
)

// Provider is a deterministic in-process implementation of provider.Client
// for use in tests. SendPayout always succeeds and returns a predictable
// external_id. CancelPayout always succeeds (cancel is supported).
type Provider struct {
	// SendErr, if non-nil, is returned by SendPayout instead of success.
	SendErr error
	// CancelErr, if non-nil, is returned by CancelPayout instead of success.
	CancelErr error
}

func (p *Provider) SendPayout(_ context.Context, payout domain.Payout) (string, error) {
	if p.SendErr != nil {
		return "", p.SendErr
	}
	return fmt.Sprintf("mock-ext-%s", payout.ID), nil
}

func (p *Provider) CancelPayout(_ context.Context, _ domain.Payout) error {
	return p.CancelErr
}
