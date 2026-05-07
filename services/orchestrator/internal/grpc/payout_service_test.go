// payout_service_test.go contains integration tests for the payout gRPC service.
//
// These tests use testcontainers to spin up a PostgreSQL container, so Docker must be
// running and available. Run with: go test -v ./services/orchestrator/internal/grpc/...
//
// Test coverage:
//   - Creating new payouts with fresh idempotency keys
//   - Idempotent replay: same key + same hash returns same payout
//   - Conflict detection: same key + different hash returns 409 AlreadyExists
//   - Validation: missing idempotency key / request hash
//   - Concurrent requests with same key only create one payout
//   - Different keys create different payouts
//   - Rail-to-provider mapping
//   - Idempotency key persistence verification
package grpc_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bashkirian/fintech-project/services/orchestrator/internal/domain"
	"github.com/bashkirian/fintech-project/services/orchestrator/internal/persistence/postgres"
	"github.com/bashkirian/fintech-project/services/orchestrator/internal/persistence/sqlcgen"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	postgres_tc "github.com/testcontainers/testcontainers-go/modules/postgres"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	orchestratorv1 "github.com/bashkirian/fintech-project/libs/genproto/orchestrator/v1"
)

// errIdempotencyKeyExists mirrors the private sentinel in payout_service.go
var errIdempotencyKeyExists = errors.New("idempotency key already exists")

func setupTestDB(t *testing.T) (ctx context.Context, pool *pgxpool.Pool, cleanup func()) {
	ctx = context.Background()

	pgContainer, err := postgres_tc.Run(ctx, "postgres:17-alpine",
		postgres_tc.WithDatabase("testdb"),
		postgres_tc.WithUsername("testuser"),
		postgres_tc.WithPassword("testpass"),
	)
	require.NoError(t, err, "failed to start postgres container")

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err, "failed to get connection string")

	// Run migrations manually
	migrationSQL := `
	CREATE TABLE payouts (
	    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
	    state        text        NOT NULL CHECK (state IN ('created', 'pending', 'processing', 'completed', 'succeeded', 'failed')),
	    amount_cents bigint      NOT NULL CHECK (amount_cents > 0),
	    currency     text        NOT NULL,
	    rail         text        NOT NULL CHECK (rail IN ('card', 'crypto')),
	    provider     text        NOT NULL CHECK (provider IN ('stripe', 'crypto_sim')),
	    external_id  text        NULL UNIQUE,
	    created_at   timestamptz NOT NULL DEFAULT now(),
	    updated_at   timestamptz NOT NULL DEFAULT now()
	);

	CREATE INDEX idx_payouts_state      ON payouts (state);
	CREATE INDEX idx_payouts_created_at ON payouts (created_at);

	CREATE TABLE idempotency_keys (
	    key          text        PRIMARY KEY,
	    request_hash text        NOT NULL,
	    payout_id    uuid        NOT NULL,
	    created_at   timestamptz NOT NULL DEFAULT now()
	);

	CREATE INDEX idx_idempotency_keys_created_at ON idempotency_keys (created_at);
	`

	// Connect and run migrations
	pool, err = pgxpool.New(ctx, connStr)
	require.NoError(t, err, "failed to connect to database")

	_, err = pool.Exec(ctx, migrationSQL)
	require.NoError(t, err, "failed to run migrations")

	cleanup = func() {
		pool.Close()
		if err := testcontainers.TerminateContainer(pgContainer); err != nil {
			t.Logf("failed to terminate container: %v", err)
		}
	}

	return ctx, pool, cleanup
}

// payoutTestServer holds test dependencies
type payoutTestServer struct {
	pool *pgxpool.Pool
}

func newTestServer(t *testing.T, pool *pgxpool.Pool) *payoutTestServer {
	return &payoutTestServer{
		pool: pool,
	}
}

// createPayout simulates the CreatePayout gRPC method logic
func (s *payoutTestServer) createPayout(
	ctx context.Context,
	idempotencyKey, requestHash string,
	amount int64, currency, rail string,
) (*orchestratorv1.CreatePayoutResponse, error) {
	if idempotencyKey == "" {
		return nil, status.Error(codes.InvalidArgument, "idempotency_key is required")
	}
	if requestHash == "" {
		return nil, status.Error(codes.InvalidArgument, "request_hash is required")
	}

	var result struct {
		newPayout   domain.Payout
		existingKey domain.IdempotencyKey
		inserted    bool
	}

	txErr := postgres.RunInTx(ctx, s.pool, func(ctx context.Context, q sqlcgen.Querier) error {
		r := domain.Rail(rail)
		provider := railToProvider(r)

		payout, err := postgres.NewPayoutRepo(q).CreatePayout(ctx, domain.CreatePayoutParams{
			State:       domain.PayoutStateCreated,
			AmountCents: amount,
			Currency:    currency,
			Rail:        r,
			Provider:    provider,
		})
		if err != nil {
			return err
		}

		key, inserted, err := postgres.NewIdempotencyRepo(q).TryInsertIdempotencyKey(
			ctx, idempotencyKey, requestHash, payout.ID,
		)
		if err != nil {
			return err
		}

		result.newPayout = payout
		result.existingKey = key
		result.inserted = inserted
		if !inserted {
			return errIdempotencyKeyExists
		}
		return nil
	})

	switch {
	case txErr == nil:
		return &orchestratorv1.CreatePayoutResponse{
			PayoutId: result.newPayout.ID.String(),
			Status:   string(result.newPayout.State),
		}, nil

	case errors.Is(txErr, errIdempotencyKeyExists):
		if result.existingKey.RequestHash != requestHash {
			return nil, status.Error(codes.AlreadyExists, "idempotency key reused with different request")
		}
		existing, err := postgres.NewPayoutRepo(sqlcgen.New(s.pool)).GetPayout(ctx, result.existingKey.PayoutID)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "fetch existing payout: %v", err)
		}
		return &orchestratorv1.CreatePayoutResponse{
			PayoutId: existing.ID.String(),
			Status:   string(existing.State),
		}, nil

	default:
		return nil, status.Errorf(codes.Internal, "create payout: %v", txErr)
	}
}

func railToProvider(rail domain.Rail) domain.Provider {
	switch rail {
	case domain.RailCrypto:
		return domain.ProviderCryptoSim
	default:
		return domain.ProviderStripe
	}
}

// getPayout replicates the GetPayout gRPC business logic for integration testing.
func (s *payoutTestServer) getPayout(
	ctx context.Context,
	payoutID string,
) (*orchestratorv1.GetPayoutResponse, error) {
	if payoutID == "" {
		return nil, status.Error(codes.InvalidArgument, "payout_id is required")
	}

	id, err := uuid.Parse(payoutID)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "payout_id must be a valid UUID")
	}

	payout, err := postgres.NewPayoutRepo(sqlcgen.New(s.pool)).GetPayout(ctx, id)
	if err != nil {
		if errors.Is(err, postgres.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "payout %s not found", payoutID)
		}
		return nil, status.Errorf(codes.Internal, "get payout: %v", err)
	}

	resp := &orchestratorv1.GetPayoutResponse{
		PayoutId: payout.ID.String(),
		Status:   string(payout.State),
		Amount:   payout.AmountCents,
		Currency: payout.Currency,
		Rail:     string(payout.Rail),
		Provider: string(payout.Provider),
	}
	if payout.ExternalID != nil {
		resp.ExternalId = *payout.ExternalID
	}
	return resp, nil
}

// TestCreatePayout_NewPayout tests creating a new payout with a fresh idempotency key
func TestCreatePayout_NewPayout(t *testing.T) {
	ctx, pool, cleanup := setupTestDB(t)
	defer cleanup()

	server := newTestServer(t, pool)

	resp, err := server.createPayout(ctx, "key-new-1", "hash-abc-123", 5000, "USD", "card")

	require.NoError(t, err)
	assert.NotEmpty(t, resp.PayoutId)
	assert.Equal(t, "created", resp.Status)

	// Verify payout was created
	payoutID, err := uuid.Parse(resp.PayoutId)
	require.NoError(t, err)

	payout, err := postgres.NewPayoutRepo(sqlcgen.New(pool)).GetPayout(ctx, payoutID)
	require.NoError(t, err)
	assert.Equal(t, int64(5000), payout.AmountCents)
	assert.Equal(t, "USD", payout.Currency)
	assert.Equal(t, domain.RailCard, payout.Rail)
	assert.Equal(t, domain.ProviderStripe, payout.Provider)
}

// TestCreatePayout_IdempotentReplay tests that calling with the same idempotency key
// and request hash returns the same payout
func TestCreatePayout_IdempotentReplay(t *testing.T) {
	ctx, pool, cleanup := setupTestDB(t)
	defer cleanup()

	server := newTestServer(t, pool)

	// First call
	resp1, err := server.createPayout(ctx, "key-replay-1", "hash-same", 10000, "EUR", "crypto")
	require.NoError(t, err)
	require.NotEmpty(t, resp1.PayoutId)

	// Second call with same key and hash - should return same payout
	resp2, err := server.createPayout(ctx, "key-replay-1", "hash-same", 10000, "EUR", "crypto")
	require.NoError(t, err)
	assert.Equal(t, resp1.PayoutId, resp2.PayoutId, "same payout ID should be returned")
	assert.Equal(t, resp1.Status, resp2.Status, "same status should be returned")

	// Verify only one payout exists in the database
	rows, err := pool.Query(ctx, "SELECT COUNT(*) FROM payouts WHERE amount_cents = 10000")
	require.NoError(t, err)
	defer rows.Close()

	var count int64
	require.True(t, rows.Next())
	require.NoError(t, rows.Scan(&count))
	assert.Equal(t, int64(1), count, "only one payout should exist in the database")

	// Verify only one idempotency key exists
	keyRows, err := pool.Query(ctx, "SELECT COUNT(*) FROM idempotency_keys WHERE key = 'key-replay-1'")
	require.NoError(t, err)
	defer keyRows.Close()

	var keyCount int64
	require.True(t, keyRows.Next())
	require.NoError(t, keyRows.Scan(&keyCount))
	assert.Equal(t, int64(1), keyCount, "only one idempotency key should exist")
}

// TestCreatePayout_ConflictingRequestHash tests that using the same idempotency key
// with a different request hash returns AlreadyExists error
func TestCreatePayout_ConflictingRequestHash(t *testing.T) {
	ctx, pool, cleanup := setupTestDB(t)
	defer cleanup()

	server := newTestServer(t, pool)

	// First call
	resp1, err := server.createPayout(ctx, "key-conflict-1", "hash-original", 5000, "USD", "card")
	require.NoError(t, err)
	require.NotEmpty(t, resp1.PayoutId)

	// Second call with same key but different hash - should fail
	_, err = server.createPayout(ctx, "key-conflict-1", "hash-different", 9999, "GBP", "crypto")
	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.AlreadyExists, st.Code())
	assert.Contains(t, st.Message(), "idempotency key reused")

	// Verify original payout still exists and is unchanged
	payoutID, err := uuid.Parse(resp1.PayoutId)
	require.NoError(t, err)

	payout, err := postgres.NewPayoutRepo(sqlcgen.New(pool)).GetPayout(ctx, payoutID)
	require.NoError(t, err)
	assert.Equal(t, int64(5000), payout.AmountCents)
	assert.Equal(t, "USD", payout.Currency)
	assert.Equal(t, domain.RailCard, payout.Rail)
}

// TestCreatePayout_MissingIdempotencyKey tests validation
func TestCreatePayout_MissingIdempotencyKey(t *testing.T) {
	ctx, pool, cleanup := setupTestDB(t)
	defer cleanup()

	server := newTestServer(t, pool)

	_, err := server.createPayout(ctx, "", "hash-123", 5000, "USD", "card")
	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "idempotency_key is required")
}

// TestCreatePayout_MissingRequestHash tests validation
func TestCreatePayout_MissingRequestHash(t *testing.T) {
	ctx, pool, cleanup := setupTestDB(t)
	defer cleanup()

	server := newTestServer(t, pool)

	_, err := server.createPayout(ctx, "key-no-hash", "", 5000, "USD", "card")
	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "request_hash is required")
}

// TestCreatePayout_MultipleConcurrentRequests tests that concurrent requests
// with the same idempotency key only create one payout
func TestCreatePayout_MultipleConcurrentRequests(t *testing.T) {
	ctx, pool, cleanup := setupTestDB(t)
	defer cleanup()

	server := newTestServer(t, pool)

	const numGoroutines = 10
	results := make(chan string, numGoroutines)
	errors := make(chan error, numGoroutines)

	// Launch concurrent requests
	for i := 0; i < numGoroutines; i++ {
		go func() {
			resp, err := server.createPayout(ctx, "key-concurrent-1", "hash-concurrent", 2500, "USD", "card")
			if err != nil {
				errors <- err
			} else {
				results <- resp.PayoutId
			}
		}()
	}

	// Collect results
	payoutIDs := make(map[string]int)
	var errs []error
	for i := 0; i < numGoroutines; i++ {
		select {
		case id := <-results:
			payoutIDs[id]++
		case err := <-errors:
			errs = append(errs, err)
		case <-time.After(10 * time.Second):
			t.Fatal("timeout waiting for goroutines")
		}
	}

	require.Empty(t, errs, "no goroutine should have returned an error")

	// All successful responses should have the same payout ID
	assert.Len(t, payoutIDs, 1, "all successful requests should return same payout ID")
	for id, count := range payoutIDs {
		assert.NotEmpty(t, id)
		t.Logf("Payout ID %s returned %d times", id, count)
	}

	// Verify only one payout exists in database
	rows, err := pool.Query(ctx, "SELECT COUNT(*) FROM payouts WHERE amount_cents = 2500")
	require.NoError(t, err)
	defer rows.Close()

	var count int64
	require.True(t, rows.Next())
	require.NoError(t, rows.Scan(&count))
	assert.Equal(t, int64(1), count, "only one payout should exist in database")
}

// TestCreatePayout_DifferentKeysCreateDifferentPayouts tests that different
// idempotency keys create different payouts
func TestCreatePayout_DifferentKeysCreateDifferentPayouts(t *testing.T) {
	ctx, pool, cleanup := setupTestDB(t)
	defer cleanup()

	server := newTestServer(t, pool)

	resp1, err := server.createPayout(ctx, "key-diff-1", "hash-1", 1000, "USD", "card")
	require.NoError(t, err)

	resp2, err := server.createPayout(ctx, "key-diff-2", "hash-2", 1000, "USD", "card")
	require.NoError(t, err)

	assert.NotEqual(t, resp1.PayoutId, resp2.PayoutId, "different keys should create different payouts")

	// Verify both payouts exist
	rows, err := pool.Query(ctx, "SELECT COUNT(*) FROM payouts WHERE amount_cents = 1000")
	require.NoError(t, err)
	defer rows.Close()

	var count int64
	require.True(t, rows.Next())
	require.NoError(t, rows.Scan(&count))
	assert.Equal(t, int64(2), count, "two payouts should exist")
}

// TestCreatePayout_RailToProviderMapping tests provider mapping
func TestCreatePayout_RailToProviderMapping(t *testing.T) {
	tests := []struct {
		name     string
		rail     string
		provider domain.Provider
	}{
		{"card maps to stripe", "card", domain.ProviderStripe},
		{"crypto maps to crypto_sim", "crypto", domain.ProviderCryptoSim},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, pool, cleanup := setupTestDB(t)
			defer cleanup()

			server := newTestServer(t, pool)

			resp, err := server.createPayout(ctx, "key-rail-"+tt.rail, "hash-"+tt.rail, 100, "USD", tt.rail)
			require.NoError(t, err)

			payoutID, err := uuid.Parse(resp.PayoutId)
			require.NoError(t, err)

			payout, err := postgres.NewPayoutRepo(sqlcgen.New(pool)).GetPayout(ctx, payoutID)
			require.NoError(t, err)
			assert.Equal(t, tt.provider, payout.Provider)
		})
	}
}

// TestIdempotencyKeyPersistence verifies that idempotency keys are correctly stored
func TestIdempotencyKeyPersistence(t *testing.T) {
	ctx, pool, cleanup := setupTestDB(t)
	defer cleanup()

	server := newTestServer(t, pool)

	resp, err := server.createPayout(ctx, "key-persist-1", "hash-persist", 7500, "GBP", "crypto")
	require.NoError(t, err)

	payoutID, err := uuid.Parse(resp.PayoutId)
	require.NoError(t, err)

	// Verify idempotency key was stored correctly
	key, err := postgres.NewIdempotencyRepo(sqlcgen.New(pool)).GetIdempotencyKey(ctx, "key-persist-1")
	require.NoError(t, err)
	assert.Equal(t, "key-persist-1", key.Key)
	assert.Equal(t, "hash-persist", key.RequestHash)
	assert.Equal(t, payoutID, key.PayoutID)
	assert.WithinDuration(t, time.Now(), key.CreatedAt, 5*time.Second)
}

// ── GetPayout integration tests ───────────────────────────────────────────────

func TestGetPayout_Success_Integration(t *testing.T) {
	ctx, pool, cleanup := setupTestDB(t)
	defer cleanup()

	server := newTestServer(t, pool)

	createResp, err := server.createPayout(ctx, "get-key-1", "get-hash-1", 5000, "USD", "card")
	require.NoError(t, err)

	ctx2, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	resp, err := server.getPayout(ctx2, createResp.PayoutId)
	require.NoError(t, err)

	assert.Equal(t, createResp.PayoutId, resp.GetPayoutId())
	assert.Equal(t, "created", resp.GetStatus())
	assert.Equal(t, int64(5000), resp.GetAmount())
	assert.Equal(t, "USD", resp.GetCurrency())
	assert.Equal(t, "card", resp.GetRail())
	assert.Equal(t, "stripe", resp.GetProvider())
	assert.Empty(t, resp.GetExternalId(), "external_id should be empty for new payout")
}

func TestGetPayout_FieldsMatchCreate_Integration(t *testing.T) {
	ctx, pool, cleanup := setupTestDB(t)
	defer cleanup()

	server := newTestServer(t, pool)

	cases := []struct {
		name     string
		amount   int64
		currency string
		rail     string
		provider string
	}{
		{"card/stripe", 9900, "GBP", "card", "stripe"},
		{"crypto/crypto_sim", 15000, "USD", "crypto", "crypto_sim"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			createResp, err := server.createPayout(ctx, "get-fields-"+tc.rail, "hash-"+tc.rail, tc.amount, tc.currency, tc.rail)
			require.NoError(t, err)

			ctx2, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()

			resp, err := server.getPayout(ctx2, createResp.PayoutId)
			require.NoError(t, err)
			assert.Equal(t, tc.amount, resp.GetAmount())
			assert.Equal(t, tc.currency, resp.GetCurrency())
			assert.Equal(t, tc.rail, resp.GetRail())
			assert.Equal(t, tc.provider, resp.GetProvider())
		})
	}
}

func TestGetPayout_NotFound_Integration(t *testing.T) {
	ctx, pool, cleanup := setupTestDB(t)
	defer cleanup()

	server := newTestServer(t, pool)
	nonExistentID := uuid.New().String()

	ctx2, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	_, err := server.getPayout(ctx2, nonExistentID)
	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	assert.Contains(t, st.Message(), nonExistentID)
}

func TestGetPayout_InvalidUUID_Integration(t *testing.T) {
	ctx, pool, cleanup := setupTestDB(t)
	defer cleanup()

	server := newTestServer(t, pool)

	ctx2, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	_, err := server.getPayout(ctx2, "not-a-uuid")
	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "payout_id must be a valid UUID")
}

func TestGetPayout_EmptyID_Integration(t *testing.T) {
	ctx, pool, cleanup := setupTestDB(t)
	defer cleanup()

	server := newTestServer(t, pool)

	ctx2, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	_, err := server.getPayout(ctx2, "")
	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "payout_id is required")
}

func TestGetPayout_AfterIdempotentReplay_Integration(t *testing.T) {
	ctx, pool, cleanup := setupTestDB(t)
	defer cleanup()

	server := newTestServer(t, pool)

	const key = "get-idempotent-1"
	const hash = "get-hash-idem-1"

	resp1, err := server.createPayout(ctx, key, hash, 3000, "EUR", "card")
	require.NoError(t, err)

	resp2, err := server.createPayout(ctx, key, hash, 3000, "EUR", "card")
	require.NoError(t, err)
	require.Equal(t, resp1.PayoutId, resp2.PayoutId, "replay must return same payout_id")

	ctx2, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	getResp, err := server.getPayout(ctx2, resp1.PayoutId)
	require.NoError(t, err)
	assert.Equal(t, resp1.PayoutId, getResp.GetPayoutId())
	assert.Equal(t, int64(3000), getResp.GetAmount())
}
