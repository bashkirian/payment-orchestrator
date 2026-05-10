package provider

import (
	"context"
	"errors"

	"github.com/bashkirian/fintech-project/services/orchestrator/internal/domain"
)

// ErrCancelNotSupported is returned by Client.CancelPayout when the provider
// does not support cancellation (e.g. an irreversible crypto rail).
var ErrCancelNotSupported = errors.New("provider does not support cancel")

// Client is the abstraction every payment provider must implement.
// SendPayout submits a payout to the provider and returns the provider's
// reference ID (external_id). CancelPayout attempts to cancel a previously
// submitted payout; it returns ErrCancelNotSupported if the provider cannot
// cancel after submission.
type Client interface {
	SendPayout(ctx context.Context, payout domain.Payout) (externalID string, err error)
	CancelPayout(ctx context.Context, payout domain.Payout) error
}
