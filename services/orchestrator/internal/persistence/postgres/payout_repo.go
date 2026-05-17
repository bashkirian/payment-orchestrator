package postgres

import (
	"context"
	"errors"

	"github.com/bashkirian/fintech-project/services/orchestrator/internal/domain"
	"github.com/bashkirian/fintech-project/services/orchestrator/internal/persistence/sqlcgen"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrNotFound = errors.New("not found")

type PayoutRepo struct {
	q sqlcgen.Querier
}

func NewPayoutRepo(q sqlcgen.Querier) *PayoutRepo {
	return &PayoutRepo{q: q}
}

func (r *PayoutRepo) CreatePayout(ctx context.Context, params domain.CreatePayoutParams) (domain.Payout, error) {
	row, err := r.q.CreatePayout(ctx, sqlcgen.CreatePayoutParams{
		State:       string(params.State),
		AmountCents: params.AmountCents,
		Currency:    params.Currency,
		Rail:        string(params.Rail),
		Provider:    string(params.Provider),
		ExternalID:  params.ExternalID,
	})
	if err != nil {
		return domain.Payout{}, err
	}
	return toDomainPayout(row), nil
}

func (r *PayoutRepo) GetPayout(ctx context.Context, id uuid.UUID) (domain.Payout, error) {
	row, err := r.q.GetPayout(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Payout{}, ErrNotFound
		}
		return domain.Payout{}, err
	}
	return toDomainPayout(row), nil
}

func (r *PayoutRepo) UpdatePayoutState(ctx context.Context, id uuid.UUID, params domain.UpdatePayoutParams) (domain.Payout, error) {
	row, err := r.q.UpdatePayoutState(ctx, sqlcgen.UpdatePayoutStateParams{
		ID:         id,
		State:      string(params.State),
		ExternalID: params.ExternalID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Payout{}, ErrNotFound
		}
		return domain.Payout{}, err
	}
	return toDomainPayout(row), nil
}

func (r *PayoutRepo) CancelPayout(ctx context.Context, id uuid.UUID, cancelableStates []domain.PayoutState) (domain.Payout, error) {
	states := make([]string, len(cancelableStates))
	for i, s := range cancelableStates {
		states[i] = string(s)
	}
	row, err := r.q.CancelPayoutIfCancelable(ctx, sqlcgen.CancelPayoutIfCancelableParams{
		ID:               id,
		CancelableStates: states,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Payout{}, ErrNotFound
		}
		return domain.Payout{}, err
	}
	return toDomainPayout(row), nil
}

func (r *PayoutRepo) FindByExternalID(ctx context.Context, externalID string) (domain.Payout, error) {
	row, err := r.q.FindPayoutByExternalID(ctx, &externalID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Payout{}, ErrNotFound
		}
		return domain.Payout{}, err
	}
	return toDomainPayout(row), nil
}

type IdempotencyRepo struct {
	q sqlcgen.Querier
}

func NewIdempotencyRepo(q sqlcgen.Querier) *IdempotencyRepo {
	return &IdempotencyRepo{q: q}
}

// TryInsertIdempotencyKey inserts a new idempotency key atomically.
// Returns (key, true, nil) on insert, (existingKey, false, nil) if already present.
func (r *IdempotencyRepo) TryInsertIdempotencyKey(ctx context.Context, key, requestHash string, payoutID uuid.UUID) (domain.IdempotencyKey, bool, error) {
	row, err := r.q.TryInsertIdempotencyKey(ctx, sqlcgen.TryInsertIdempotencyKeyParams{
		Key:         key,
		RequestHash: requestHash,
		PayoutID:    payoutID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Key already exists; fetch the existing record.
			existing, getErr := r.q.GetIdempotencyKey(ctx, key)
			if getErr != nil {
				return domain.IdempotencyKey{}, false, getErr
			}
			return toDomainIdempotencyKey(existing), false, nil
		}
		return domain.IdempotencyKey{}, false, err
	}
	return toDomainIdempotencyKey(row), true, nil
}

func (r *IdempotencyRepo) GetIdempotencyKey(ctx context.Context, key string) (domain.IdempotencyKey, error) {
	row, err := r.q.GetIdempotencyKey(ctx, key)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.IdempotencyKey{}, ErrNotFound
		}
		return domain.IdempotencyKey{}, err
	}
	return toDomainIdempotencyKey(row), nil
}

func toDomainPayout(p sqlcgen.Payout) domain.Payout {
	return domain.Payout{
		ID:          p.ID,
		State:       domain.PayoutState(p.State),
		AmountCents: p.AmountCents,
		Currency:    p.Currency,
		Rail:        domain.Rail(p.Rail),
		Provider:    domain.Provider(p.Provider),
		ExternalID:  p.ExternalID,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
}

func toDomainIdempotencyKey(k sqlcgen.IdempotencyKey) domain.IdempotencyKey {
	return domain.IdempotencyKey{
		Key:         k.Key,
		RequestHash: k.RequestHash,
		PayoutID:    k.PayoutID,
		CreatedAt:   k.CreatedAt,
	}
}
