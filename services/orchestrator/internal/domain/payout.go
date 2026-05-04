package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type PayoutState string

const (
	PayoutStatePending    PayoutState = "pending"
	PayoutStateProcessing PayoutState = "processing"
	PayoutStateCompleted  PayoutState = "completed"
	PayoutStateFailed     PayoutState = "failed"
)

type Rail string

const (
	RailCard   Rail = "card"
	RailCrypto Rail = "crypto"
)

type Provider string

const (
	ProviderStripe    Provider = "stripe"
	ProviderCryptoSim Provider = "crypto_sim"
)

type Payout struct {
	ID          uuid.UUID
	State       PayoutState
	AmountCents int64
	Currency    string
	Rail        Rail
	Provider    Provider
	ExternalID  *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type IdempotencyKey struct {
	Key         string
	RequestHash string
	PayoutID    uuid.UUID
	CreatedAt   time.Time
}

type CreatePayoutParams struct {
	State       PayoutState
	AmountCents int64
	Currency    string
	Rail        Rail
	Provider    Provider
	ExternalID  *string
}

type PayoutRepository interface {
	CreatePayout(ctx context.Context, params CreatePayoutParams) (Payout, error)
	GetPayout(ctx context.Context, id uuid.UUID) (Payout, error)
}

// IdempotencyRepository provides atomic idempotency key management.
// TryInsertIdempotencyKey returns (key, true, nil) on successful insert,
// or (existingKey, false, nil) if the key already exists.
type IdempotencyRepository interface {
	TryInsertIdempotencyKey(ctx context.Context, key, requestHash string, payoutID uuid.UUID) (IdempotencyKey, bool, error)
	GetIdempotencyKey(ctx context.Context, key string) (IdempotencyKey, error)
}
