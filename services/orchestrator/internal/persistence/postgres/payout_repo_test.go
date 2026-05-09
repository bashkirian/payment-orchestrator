package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/bashkirian/fintech-project/services/orchestrator/internal/domain"
	"github.com/bashkirian/fintech-project/services/orchestrator/internal/persistence/postgres"
	"github.com/bashkirian/fintech-project/services/orchestrator/internal/persistence/sqlcgen"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockQuerier is an in-memory mock that implements sqlcgen.Querier.
type mockQuerier struct {
	payouts         map[uuid.UUID]sqlcgen.Payout
	idempotencyKeys map[string]sqlcgen.IdempotencyKey
}

func newMockQuerier() *mockQuerier {
	return &mockQuerier{
		payouts:         make(map[uuid.UUID]sqlcgen.Payout),
		idempotencyKeys: make(map[string]sqlcgen.IdempotencyKey),
	}
}

func (m *mockQuerier) CreatePayout(_ context.Context, arg sqlcgen.CreatePayoutParams) (sqlcgen.Payout, error) {
	p := sqlcgen.Payout{
		ID:          uuid.New(),
		State:       arg.State,
		AmountCents: arg.AmountCents,
		Currency:    arg.Currency,
		Rail:        arg.Rail,
		Provider:    arg.Provider,
		ExternalID:  arg.ExternalID,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	m.payouts[p.ID] = p
	return p, nil
}

func (m *mockQuerier) GetPayout(_ context.Context, id uuid.UUID) (sqlcgen.Payout, error) {
	p, ok := m.payouts[id]
	if !ok {
		return sqlcgen.Payout{}, pgx.ErrNoRows
	}
	return p, nil
}

func (m *mockQuerier) TryInsertIdempotencyKey(_ context.Context, arg sqlcgen.TryInsertIdempotencyKeyParams) (sqlcgen.IdempotencyKey, error) {
	if _, exists := m.idempotencyKeys[arg.Key]; exists {
		return sqlcgen.IdempotencyKey{}, pgx.ErrNoRows
	}
	k := sqlcgen.IdempotencyKey{
		Key:         arg.Key,
		RequestHash: arg.RequestHash,
		PayoutID:    arg.PayoutID,
		CreatedAt:   time.Now(),
	}
	m.idempotencyKeys[k.Key] = k
	return k, nil
}

func (m *mockQuerier) GetIdempotencyKey(_ context.Context, key string) (sqlcgen.IdempotencyKey, error) {
	k, ok := m.idempotencyKeys[key]
	if !ok {
		return sqlcgen.IdempotencyKey{}, pgx.ErrNoRows
	}
	return k, nil
}

func (m *mockQuerier) UpdatePayoutState(_ context.Context, arg sqlcgen.UpdatePayoutStateParams) (sqlcgen.Payout, error) {
	p, ok := m.payouts[arg.ID]
	if !ok {
		return sqlcgen.Payout{}, pgx.ErrNoRows
	}
	p.State = arg.State
	p.ExternalID = arg.ExternalID
	p.UpdatedAt = time.Now()
	m.payouts[arg.ID] = p
	return p, nil
}

func (m *mockQuerier) CancelPayoutIfCancelable(_ context.Context, arg sqlcgen.CancelPayoutIfCancelableParams) (sqlcgen.Payout, error) {
	p, ok := m.payouts[arg.ID]
	if !ok {
		return sqlcgen.Payout{}, pgx.ErrNoRows
	}
	for _, s := range arg.CancelableStates {
		if p.State == s {
			p.State = "canceled"
			p.UpdatedAt = time.Now()
			m.payouts[arg.ID] = p
			return p, nil
		}
	}
	// State not in cancelable list
	return sqlcgen.Payout{}, pgx.ErrNoRows
}

// --- PayoutRepo tests ---

func TestPayoutRepo_CreatePayout(t *testing.T) {
	q := newMockQuerier()
	repo := postgres.NewPayoutRepo(q)
	ctx := context.Background()

	params := domain.CreatePayoutParams{
		State:       domain.PayoutStatePending,
		AmountCents: 5000,
		Currency:    "USD",
		Rail:        domain.RailCard,
		Provider:    domain.ProviderStripe,
	}

	payout, err := repo.CreatePayout(ctx, params)
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, payout.ID)
	assert.Equal(t, domain.PayoutStatePending, payout.State)
	assert.Equal(t, int64(5000), payout.AmountCents)
	assert.Equal(t, "USD", payout.Currency)
	assert.Equal(t, domain.RailCard, payout.Rail)
	assert.Equal(t, domain.ProviderStripe, payout.Provider)
	assert.Nil(t, payout.ExternalID)
}

func TestPayoutRepo_GetPayout_Found(t *testing.T) {
	q := newMockQuerier()
	repo := postgres.NewPayoutRepo(q)
	ctx := context.Background()

	created, err := repo.CreatePayout(ctx, domain.CreatePayoutParams{
		State:       domain.PayoutStateProcessing,
		AmountCents: 1000,
		Currency:    "EUR",
		Rail:        domain.RailCrypto,
		Provider:    domain.ProviderCryptoSim,
	})
	require.NoError(t, err)

	got, err := repo.GetPayout(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, got.ID)
	assert.Equal(t, domain.PayoutStateProcessing, got.State)
}

func TestPayoutRepo_GetPayout_NotFound(t *testing.T) {
	q := newMockQuerier()
	repo := postgres.NewPayoutRepo(q)

	_, err := repo.GetPayout(context.Background(), uuid.New())
	assert.ErrorIs(t, err, postgres.ErrNotFound)
}

// --- IdempotencyRepo tests ---

func TestIdempotencyRepo_TryInsert_NewKey(t *testing.T) {
	q := newMockQuerier()
	repo := postgres.NewIdempotencyRepo(q)
	ctx := context.Background()

	payoutID := uuid.New()
	key, inserted, err := repo.TryInsertIdempotencyKey(ctx, "key-abc", "hash-1", payoutID)
	require.NoError(t, err)
	assert.True(t, inserted)
	assert.Equal(t, "key-abc", key.Key)
	assert.Equal(t, "hash-1", key.RequestHash)
	assert.Equal(t, payoutID, key.PayoutID)
}

func TestIdempotencyRepo_TryInsert_ConflictReturnsExisting(t *testing.T) {
	q := newMockQuerier()
	repo := postgres.NewIdempotencyRepo(q)
	ctx := context.Background()

	payoutID := uuid.New()
	_, _, err := repo.TryInsertIdempotencyKey(ctx, "key-dup", "hash-orig", payoutID)
	require.NoError(t, err)

	existing, inserted, err := repo.TryInsertIdempotencyKey(ctx, "key-dup", "hash-new", uuid.New())
	require.NoError(t, err)
	assert.False(t, inserted)
	assert.Equal(t, "key-dup", existing.Key)
	assert.Equal(t, "hash-orig", existing.RequestHash)
	assert.Equal(t, payoutID, existing.PayoutID)
}

func TestIdempotencyRepo_GetIdempotencyKey_NotFound(t *testing.T) {
	q := newMockQuerier()
	repo := postgres.NewIdempotencyRepo(q)

	_, err := repo.GetIdempotencyKey(context.Background(), "missing")
	assert.ErrorIs(t, err, postgres.ErrNotFound)
}

// --- CancelPayout repo tests ---

func TestPayoutRepo_CancelPayout_Created(t *testing.T) {
	q := newMockQuerier()
	repo := postgres.NewPayoutRepo(q)
	ctx := context.Background()

	created, err := repo.CreatePayout(ctx, domain.CreatePayoutParams{
		State: domain.PayoutStateCreated, AmountCents: 5000, Currency: "USD",
		Rail: domain.RailCard, Provider: domain.ProviderStripe,
	})
	require.NoError(t, err)

	cancelable := []domain.PayoutState{domain.PayoutStateCreated, domain.PayoutStateQueued}
	result, err := repo.CancelPayout(ctx, created.ID, cancelable)
	require.NoError(t, err)
	assert.Equal(t, domain.PayoutStateCanceled, result.State)
	assert.Equal(t, created.ID, result.ID)
}

func TestPayoutRepo_CancelPayout_WrongState(t *testing.T) {
	q := newMockQuerier()
	repo := postgres.NewPayoutRepo(q)
	ctx := context.Background()

	created, err := repo.CreatePayout(ctx, domain.CreatePayoutParams{
		State: domain.PayoutStateProcessing, AmountCents: 5000, Currency: "USD",
		Rail: domain.RailCard, Provider: domain.ProviderStripe,
	})
	require.NoError(t, err)

	cancelable := []domain.PayoutState{domain.PayoutStateCreated, domain.PayoutStateQueued}
	_, err = repo.CancelPayout(ctx, created.ID, cancelable)
	assert.ErrorIs(t, err, postgres.ErrNotFound)
}

func TestPayoutRepo_CancelPayout_NotFound(t *testing.T) {
	q := newMockQuerier()
	repo := postgres.NewPayoutRepo(q)

	cancelable := []domain.PayoutState{domain.PayoutStateCreated, domain.PayoutStateQueued}
	_, err := repo.CancelPayout(context.Background(), uuid.New(), cancelable)
	assert.ErrorIs(t, err, postgres.ErrNotFound)
}
