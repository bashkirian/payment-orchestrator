// router_payout_test.go contains unit tests for the /v1/payouts HTTP endpoint.
//
// These tests mock the gRPC client to test the HTTP layer independently.
// For end-to-end integration tests, see the orchestrator's gRPC tests.
package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	orchestratorv1 "github.com/bashkirian/fintech-project/libs/genproto/orchestrator/v1"
)

// mockPayoutClient implements PayoutClient for testing
type mockPayoutClient struct {
	createPayoutFunc func(ctx context.Context, in *orchestratorv1.CreatePayoutRequest, opts ...grpc.CallOption) (*orchestratorv1.CreatePayoutResponse, error)
	getPayoutFunc    func(ctx context.Context, in *orchestratorv1.GetPayoutRequest, opts ...grpc.CallOption) (*orchestratorv1.GetPayoutResponse, error)
	cancelPayoutFunc func(ctx context.Context, in *orchestratorv1.CancelPayoutRequest, opts ...grpc.CallOption) (*orchestratorv1.CancelPayoutResponse, error)
}

func (m *mockPayoutClient) CreatePayout(ctx context.Context, in *orchestratorv1.CreatePayoutRequest, opts ...grpc.CallOption) (*orchestratorv1.CreatePayoutResponse, error) {
	if m.createPayoutFunc != nil {
		return m.createPayoutFunc(ctx, in, opts...)
	}
	return &orchestratorv1.CreatePayoutResponse{PayoutId: "test-payout-id", Status: "created"}, nil
}

func (m *mockPayoutClient) GetPayout(ctx context.Context, in *orchestratorv1.GetPayoutRequest, opts ...grpc.CallOption) (*orchestratorv1.GetPayoutResponse, error) {
	if m.getPayoutFunc != nil {
		return m.getPayoutFunc(ctx, in, opts...)
	}
	return &orchestratorv1.GetPayoutResponse{
		PayoutId: in.GetPayoutId(),
		Status:   "created",
		Amount:   5000,
		Currency: "USD",
		Rail:     "card",
		Provider: "stripe",
	}, nil
}

func (m *mockPayoutClient) CancelPayout(ctx context.Context, in *orchestratorv1.CancelPayoutRequest, opts ...grpc.CallOption) (*orchestratorv1.CancelPayoutResponse, error) {
	if m.cancelPayoutFunc != nil {
		return m.cancelPayoutFunc(ctx, in, opts...)
	}
	return &orchestratorv1.CancelPayoutResponse{Success: true}, nil
}

func TestCreatePayout_Success(t *testing.T) {
	mockClient := &mockPayoutClient{
		createPayoutFunc: func(ctx context.Context, in *orchestratorv1.CreatePayoutRequest, opts ...grpc.CallOption) (*orchestratorv1.CreatePayoutResponse, error) {
			// Verify idempotency key and request hash are passed
			assert.Equal(t, "test-idempotency-key", in.GetIdempotencyKey())
			assert.NotEmpty(t, in.GetRequestHash(), "request hash should be computed")
			assert.Equal(t, int64(5000), in.GetAmount())
			assert.Equal(t, "USD", in.GetCurrency())
			assert.Equal(t, "card", in.GetRail())
			return &orchestratorv1.CreatePayoutResponse{
				PayoutId: "payout-123",
				Status:   "created",
			}, nil
		},
	}

	router := NewRouterWithClient(zap.NewNop(), mockClient, nil, false)

	body := `{"amount": 5000, "currency": "USD", "rail": "card"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/payouts", bytes.NewBufferString(body))
	req.Header.Set("Idempotency-Key", "test-idempotency-key")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusAccepted, rec.Code)

	var resp map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "payout-123", resp["payout_id"])
	assert.Equal(t, "created", resp["status"])
}

func TestCreatePayout_MissingIdempotencyKey(t *testing.T) {
	mockClient := &mockPayoutClient{}
	router := NewRouterWithClient(zap.NewNop(), mockClient, nil, false)

	body := `{"amount": 5000, "currency": "USD", "rail": "card"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/payouts", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "Idempotency-Key header is required")
}

func TestCreatePayout_InvalidRequestBody(t *testing.T) {
	mockClient := &mockPayoutClient{}
	router := NewRouterWithClient(zap.NewNop(), mockClient, nil, false)

	tests := []struct {
		name   string
		body   string
		errMsg string
	}{
		{
			name:   "invalid JSON",
			body:   `{invalid json}`,
			errMsg: "invalid request body",
		},
		{
			name:   "negative amount",
			body:   `{"amount": -100, "currency": "USD", "rail": "card"}`,
			errMsg: "amount must be positive",
		},
		{
			name:   "zero amount",
			body:   `{"amount": 0, "currency": "USD", "rail": "card"}`,
			errMsg: "amount must be positive",
		},
		{
			name:   "missing rail",
			body:   `{"amount": 100, "currency": "USD"}`,
			errMsg: "rail is required",
		},
		{
			name:   "empty rail",
			body:   `{"amount": 100, "currency": "USD", "rail": ""}`,
			errMsg: "rail is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/payouts", bytes.NewBufferString(tt.body))
			req.Header.Set("Idempotency-Key", "test-key")
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Contains(t, rec.Body.String(), tt.errMsg)
		})
	}
}

func TestCreatePayout_IdempotentReplay(t *testing.T) {
	callCount := 0
	mockClient := &mockPayoutClient{
		createPayoutFunc: func(ctx context.Context, in *orchestratorv1.CreatePayoutRequest, opts ...grpc.CallOption) (*orchestratorv1.CreatePayoutResponse, error) {
			callCount++
			// Same response for same idempotency key
			return &orchestratorv1.CreatePayoutResponse{
				PayoutId: "payout-replay-123",
				Status:   "created",
			}, nil
		},
	}

	router := NewRouterWithClient(zap.NewNop(), mockClient, nil, false)

	body := `{"amount": 10000, "currency": "EUR", "rail": "crypto"}`

	// First call
	req1 := httptest.NewRequest(http.MethodPost, "/v1/payouts", bytes.NewBufferString(body))
	req1.Header.Set("Idempotency-Key", "same-key-replay")
	req1.Header.Set("Content-Type", "application/json")
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req1)
	require.Equal(t, http.StatusAccepted, rec1.Code)

	// Second call with same key - should return same payout ID
	req2 := httptest.NewRequest(http.MethodPost, "/v1/payouts", bytes.NewBufferString(body))
	req2.Header.Set("Idempotency-Key", "same-key-replay")
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)

	assert.Equal(t, http.StatusAccepted, rec2.Code)

	var resp1, resp2 map[string]string
	require.NoError(t, json.Unmarshal(rec1.Body.Bytes(), &resp1))
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &resp2))
	assert.Equal(t, resp1["payout_id"], resp2["payout_id"], "same payout ID on replay")
	assert.Equal(t, 2, callCount, "gRPC should be called twice (orchestrator handles idempotency)")
}

func TestCreatePayout_ConflictDifferentHash(t *testing.T) {
	mockClient := &mockPayoutClient{
		createPayoutFunc: func(ctx context.Context, in *orchestratorv1.CreatePayoutRequest, opts ...grpc.CallOption) (*orchestratorv1.CreatePayoutResponse, error) {
			// Simulate the orchestrator returning AlreadyExists for key reuse with different hash
			return nil, status.Error(codes.AlreadyExists, "idempotency key reused with different request")
		},
	}

	router := NewRouterWithClient(zap.NewNop(), mockClient, nil, false)

	body := `{"amount": 5000, "currency": "USD", "rail": "card"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/payouts", bytes.NewBufferString(body))
	req.Header.Set("Idempotency-Key", "key-conflict")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "idempotency key reused")
}

func TestCreatePayout_UpstreamError(t *testing.T) {
	mockClient := &mockPayoutClient{
		createPayoutFunc: func(ctx context.Context, in *orchestratorv1.CreatePayoutRequest, opts ...grpc.CallOption) (*orchestratorv1.CreatePayoutResponse, error) {
			return nil, status.Error(codes.Internal, "database connection failed")
		},
	}

	router := NewRouterWithClient(zap.NewNop(), mockClient, nil, false)

	body := `{"amount": 5000, "currency": "USD", "rail": "card"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/payouts", bytes.NewBufferString(body))
	req.Header.Set("Idempotency-Key", "test-key")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Contains(t, rec.Body.String(), "upstream error")
}

func TestCanonicalHash_Consistency(t *testing.T) {
	// Verify that the same input always produces the same hash
	hash1 := canonicalHash(5000, "USD", "card")
	hash2 := canonicalHash(5000, "USD", "card")
	assert.Equal(t, hash1, hash2, "same input should produce same hash")

	// Verify different inputs produce different hashes
	hash3 := canonicalHash(5000, "USD", "crypto")
	assert.NotEqual(t, hash1, hash3, "different rail should produce different hash")

	hash4 := canonicalHash(5000, "EUR", "card")
	assert.NotEqual(t, hash1, hash4, "different currency should produce different hash")

	hash5 := canonicalHash(10000, "USD", "card")
	assert.NotEqual(t, hash1, hash5, "different amount should produce different hash")
}

func TestCanonicalHash_Format(t *testing.T) {
	// Verify hash is 64 characters (SHA-256 hex encoding)
	hash := canonicalHash(12345, "USD", "card")
	assert.Len(t, hash, 64, "SHA-256 hex should be 64 characters")
	assert.Regexp(t, "^[a-f0-9]+$", hash, "hash should be lowercase hex")
}

func TestCreatePayout_RequestHashComputedCorrectly(t *testing.T) {
	// Test that different request bodies produce different hashes
	var receivedHashes []string

	mockClient := &mockPayoutClient{
		createPayoutFunc: func(ctx context.Context, in *orchestratorv1.CreatePayoutRequest, opts ...grpc.CallOption) (*orchestratorv1.CreatePayoutResponse, error) {
			receivedHashes = append(receivedHashes, in.GetRequestHash())
			return &orchestratorv1.CreatePayoutResponse{PayoutId: "id", Status: "created"}, nil
		},
	}

	router := NewRouterWithClient(zap.NewNop(), mockClient, nil, false)

	// Different amounts should produce different hashes
	requests := []struct {
		amount   int64
		currency string
		rail     string
	}{
		{5000, "USD", "card"},
		{10000, "USD", "card"},
		{5000, "EUR", "card"},
		{5000, "USD", "crypto"},
	}

	for _, r := range requests {
		body := bytes.NewBufferString("")
		require.NoError(t, json.NewEncoder(body).Encode(map[string]interface{}{
			"amount":   r.amount,
			"currency": r.currency,
			"rail":     r.rail,
		}))

		req := httptest.NewRequest(http.MethodPost, "/v1/payouts", body)
		req.Header.Set("Idempotency-Key", "key-"+r.rail+"-"+r.currency)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusAccepted, rec.Code)
	}

	// All hashes should be unique
	uniqueHashes := make(map[string]bool)
	for _, h := range receivedHashes {
		assert.False(t, uniqueHashes[h], "hash should be unique: %s", h)
		uniqueHashes[h] = true
	}
}

func TestCreatePayout_ContentTypeNotRequired(t *testing.T) {
	// The handler should work without Content-Type header (JSON decoder is lenient)
	mockClient := &mockPayoutClient{
		createPayoutFunc: func(ctx context.Context, in *orchestratorv1.CreatePayoutRequest, opts ...grpc.CallOption) (*orchestratorv1.CreatePayoutResponse, error) {
			return &orchestratorv1.CreatePayoutResponse{PayoutId: "id", Status: "created"}, nil
		},
	}

	router := NewRouterWithClient(zap.NewNop(), mockClient, nil, false)

	body := `{"amount": 5000, "currency": "USD", "rail": "card"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/payouts", bytes.NewBufferString(body))
	req.Header.Set("Idempotency-Key", "test-key")
	// No Content-Type header
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusAccepted, rec.Code)
}

func TestCreatePayout_SameBodySameHash(t *testing.T) {
	// Verify that identical request bodies produce identical hashes
	var hash1, hash2 string

	mockClient := &mockPayoutClient{
		createPayoutFunc: func(ctx context.Context, in *orchestratorv1.CreatePayoutRequest, opts ...grpc.CallOption) (*orchestratorv1.CreatePayoutResponse, error) {
			if hash1 == "" {
				hash1 = in.GetRequestHash()
			} else {
				hash2 = in.GetRequestHash()
			}
			return &orchestratorv1.CreatePayoutResponse{PayoutId: "id", Status: "created"}, nil
		},
	}

	router := NewRouterWithClient(zap.NewNop(), mockClient, nil, false)

	// Two identical requests (different idempotency keys)
	body := `{"amount": 5000, "currency": "USD", "rail": "card"}`

	req1 := httptest.NewRequest(http.MethodPost, "/v1/payouts", bytes.NewBufferString(body))
	req1.Header.Set("Idempotency-Key", "key-1")
	req1.Header.Set("Content-Type", "application/json")
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req1)
	require.Equal(t, http.StatusAccepted, rec1.Code)

	req2 := httptest.NewRequest(http.MethodPost, "/v1/payouts", bytes.NewBufferString(body))
	req2.Header.Set("Idempotency-Key", "key-2")
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusAccepted, rec2.Code)

	assert.Equal(t, hash1, hash2, "identical request bodies should produce same hash")
}

func TestCreatePayout_MissingCurrency(t *testing.T) {
	mockClient := &mockPayoutClient{
		createPayoutFunc: func(ctx context.Context, in *orchestratorv1.CreatePayoutRequest, opts ...grpc.CallOption) (*orchestratorv1.CreatePayoutResponse, error) {
			return &orchestratorv1.CreatePayoutResponse{PayoutId: "id", Status: "created"}, nil
		},
	}

	router := NewRouterWithClient(zap.NewNop(), mockClient, nil, false)

	// Currency is optional - handler should pass empty string to orchestrator
	body := `{"amount": 5000, "rail": "card"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/payouts", bytes.NewBufferString(body))
	req.Header.Set("Idempotency-Key", "test-key-no-currency")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusAccepted, rec.Code)
}

// ── GET /v1/payouts/{id} ──────────────────────────────────────────────────────

func TestGetPayout_Success(t *testing.T) {
	payoutID := "550e8400-e29b-41d4-a716-446655440000"

	mockClient := &mockPayoutClient{
		getPayoutFunc: func(ctx context.Context, in *orchestratorv1.GetPayoutRequest, opts ...grpc.CallOption) (*orchestratorv1.GetPayoutResponse, error) {
			assert.Equal(t, payoutID, in.GetPayoutId())
			return &orchestratorv1.GetPayoutResponse{
				PayoutId: payoutID,
				Status:   "created",
				Amount:   7500,
				Currency: "USD",
				Rail:     "card",
				Provider: "stripe",
			}, nil
		},
	}

	router := NewRouterWithClient(zap.NewNop(), mockClient, nil, false)
	req := httptest.NewRequest(http.MethodGet, "/v1/payouts/"+payoutID, nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, payoutID, resp["payout_id"])
	assert.Equal(t, "created", resp["status"])
	assert.Equal(t, float64(7500), resp["amount"])
	assert.Equal(t, "USD", resp["currency"])
	assert.Equal(t, "card", resp["rail"])
	assert.Equal(t, "stripe", resp["provider"])
}

func TestGetPayout_WithExternalID(t *testing.T) {
	payoutID := "550e8400-e29b-41d4-a716-446655440001"

	mockClient := &mockPayoutClient{
		getPayoutFunc: func(ctx context.Context, in *orchestratorv1.GetPayoutRequest, opts ...grpc.CallOption) (*orchestratorv1.GetPayoutResponse, error) {
			return &orchestratorv1.GetPayoutResponse{
				PayoutId:   payoutID,
				Status:     "completed",
				Amount:     10000,
				Currency:   "EUR",
				Rail:       "crypto",
				Provider:   "crypto_sim",
				ExternalId: "ext-ref-xyz",
			}, nil
		},
	}

	router := NewRouterWithClient(zap.NewNop(), mockClient, nil, false)
	req := httptest.NewRequest(http.MethodGet, "/v1/payouts/"+payoutID, nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "ext-ref-xyz", resp["external_id"])
	assert.Equal(t, "completed", resp["status"])
}

func TestGetPayout_ExternalIDOmittedWhenEmpty(t *testing.T) {
	mockClient := &mockPayoutClient{
		getPayoutFunc: func(ctx context.Context, in *orchestratorv1.GetPayoutRequest, opts ...grpc.CallOption) (*orchestratorv1.GetPayoutResponse, error) {
			return &orchestratorv1.GetPayoutResponse{
				PayoutId: in.GetPayoutId(),
				Status:   "created",
				Amount:   5000,
				Currency: "USD",
				Rail:     "card",
				Provider: "stripe",
				// ExternalId intentionally empty
			}, nil
		},
	}

	router := NewRouterWithClient(zap.NewNop(), mockClient, nil, false)
	req := httptest.NewRequest(http.MethodGet, "/v1/payouts/some-id", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	_, hasExternalID := resp["external_id"]
	assert.False(t, hasExternalID, "external_id should be omitted when empty")
}

func TestGetPayout_NotFound(t *testing.T) {
	mockClient := &mockPayoutClient{
		getPayoutFunc: func(ctx context.Context, in *orchestratorv1.GetPayoutRequest, opts ...grpc.CallOption) (*orchestratorv1.GetPayoutResponse, error) {
			return nil, status.Error(codes.NotFound, "payout not found")
		},
	}

	router := NewRouterWithClient(zap.NewNop(), mockClient, nil, false)
	req := httptest.NewRequest(http.MethodGet, "/v1/payouts/nonexistent-id", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "payout not found")
}

func TestGetPayout_InvalidUUID(t *testing.T) {
	mockClient := &mockPayoutClient{
		getPayoutFunc: func(ctx context.Context, in *orchestratorv1.GetPayoutRequest, opts ...grpc.CallOption) (*orchestratorv1.GetPayoutResponse, error) {
			return nil, status.Error(codes.InvalidArgument, "payout_id must be a valid UUID")
		},
	}

	router := NewRouterWithClient(zap.NewNop(), mockClient, nil, false)
	req := httptest.NewRequest(http.MethodGet, "/v1/payouts/not-a-uuid", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "payout_id must be a valid UUID")
}

func TestGetPayout_UpstreamError(t *testing.T) {
	mockClient := &mockPayoutClient{
		getPayoutFunc: func(ctx context.Context, in *orchestratorv1.GetPayoutRequest, opts ...grpc.CallOption) (*orchestratorv1.GetPayoutResponse, error) {
			return nil, status.Error(codes.Internal, "database unavailable")
		},
	}

	router := NewRouterWithClient(zap.NewNop(), mockClient, nil, false)
	req := httptest.NewRequest(http.MethodGet, "/v1/payouts/some-id", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Contains(t, rec.Body.String(), "upstream error")
}

func TestGetPayout_PassesIDToGRPC(t *testing.T) {
	var capturedID string

	mockClient := &mockPayoutClient{
		getPayoutFunc: func(ctx context.Context, in *orchestratorv1.GetPayoutRequest, opts ...grpc.CallOption) (*orchestratorv1.GetPayoutResponse, error) {
			capturedID = in.GetPayoutId()
			return &orchestratorv1.GetPayoutResponse{PayoutId: in.GetPayoutId(), Status: "created"}, nil
		},
	}

	router := NewRouterWithClient(zap.NewNop(), mockClient, nil, false)
	req := httptest.NewRequest(http.MethodGet, "/v1/payouts/abc-123-def", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "abc-123-def", capturedID, "URL path ID must be forwarded to gRPC as-is")
}

// TestGetPayout_AfterCreate is an end-to-end HTTP-layer test:
// POST creates a payout, GET fetches it by the returned ID.
func TestGetPayout_AfterCreate(t *testing.T) {
	const fixedID = "550e8400-e29b-41d4-a716-446655440099"

	store := map[string]*orchestratorv1.GetPayoutResponse{}

	mockClient := &mockPayoutClient{
		createPayoutFunc: func(ctx context.Context, in *orchestratorv1.CreatePayoutRequest, opts ...grpc.CallOption) (*orchestratorv1.CreatePayoutResponse, error) {
			store[fixedID] = &orchestratorv1.GetPayoutResponse{
				PayoutId: fixedID,
				Status:   "created",
				Amount:   in.GetAmount(),
				Currency: in.GetCurrency(),
				Rail:     in.GetRail(),
				Provider: "stripe",
			}
			return &orchestratorv1.CreatePayoutResponse{PayoutId: fixedID, Status: "created"}, nil
		},
		getPayoutFunc: func(ctx context.Context, in *orchestratorv1.GetPayoutRequest, opts ...grpc.CallOption) (*orchestratorv1.GetPayoutResponse, error) {
			p, ok := store[in.GetPayoutId()]
			if !ok {
				return nil, status.Error(codes.NotFound, "payout not found")
			}
			return p, nil
		},
	}

	router := NewRouterWithClient(zap.NewNop(), mockClient, nil, false)

	// Step 1: create
	createBody := `{"amount":9900,"currency":"GBP","rail":"card"}`
	createReq := httptest.NewRequest(http.MethodPost, "/v1/payouts", bytes.NewBufferString(createBody))
	createReq.Header.Set("Idempotency-Key", "e2e-key-1")
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	router.ServeHTTP(createRec, createReq)
	require.Equal(t, http.StatusAccepted, createRec.Code)

	var createResp map[string]string
	require.NoError(t, json.Unmarshal(createRec.Body.Bytes(), &createResp))
	payoutID := createResp["payout_id"]
	require.NotEmpty(t, payoutID)

	// Step 2: fetch by ID
	getReq := httptest.NewRequest(http.MethodGet, "/v1/payouts/"+payoutID, nil)
	getRec := httptest.NewRecorder()
	router.ServeHTTP(getRec, getReq)
	require.Equal(t, http.StatusOK, getRec.Code)

	var getResp map[string]interface{}
	require.NoError(t, json.Unmarshal(getRec.Body.Bytes(), &getResp))
	assert.Equal(t, payoutID, getResp["payout_id"])
	assert.Equal(t, "created", getResp["status"])
	assert.Equal(t, float64(9900), getResp["amount"])
	assert.Equal(t, "GBP", getResp["currency"])
	assert.Equal(t, "card", getResp["rail"])
}

// ── POST /v1/payouts/{id}/cancel ─────────────────────────────────────────────

func TestCancelPayout_Success(t *testing.T) {
	payoutID := "550e8400-e29b-41d4-a716-446655440000"

	mockClient := &mockPayoutClient{
		cancelPayoutFunc: func(ctx context.Context, in *orchestratorv1.CancelPayoutRequest, opts ...grpc.CallOption) (*orchestratorv1.CancelPayoutResponse, error) {
			assert.Equal(t, payoutID, in.GetPayoutId())
			return &orchestratorv1.CancelPayoutResponse{Success: true}, nil
		},
	}

	router := NewRouterWithClient(zap.NewNop(), mockClient, nil, false)
	req := httptest.NewRequest(http.MethodPost, "/v1/payouts/"+payoutID+"/cancel", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]bool
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.True(t, resp["success"])
}

func TestCancelPayout_NotFound(t *testing.T) {
	mockClient := &mockPayoutClient{
		cancelPayoutFunc: func(ctx context.Context, in *orchestratorv1.CancelPayoutRequest, opts ...grpc.CallOption) (*orchestratorv1.CancelPayoutResponse, error) {
			return nil, status.Error(codes.NotFound, "payout not found")
		},
	}

	router := NewRouterWithClient(zap.NewNop(), mockClient, nil, false)
	req := httptest.NewRequest(http.MethodPost, "/v1/payouts/nonexistent/cancel", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "payout not found")
}

func TestCancelPayout_InvalidUUID(t *testing.T) {
	mockClient := &mockPayoutClient{
		cancelPayoutFunc: func(ctx context.Context, in *orchestratorv1.CancelPayoutRequest, opts ...grpc.CallOption) (*orchestratorv1.CancelPayoutResponse, error) {
			return nil, status.Error(codes.InvalidArgument, "payout_id must be a valid UUID")
		},
	}

	router := NewRouterWithClient(zap.NewNop(), mockClient, nil, false)
	req := httptest.NewRequest(http.MethodPost, "/v1/payouts/not-a-uuid/cancel", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "payout_id must be a valid UUID")
}

func TestCancelPayout_WrongState(t *testing.T) {
	mockClient := &mockPayoutClient{
		cancelPayoutFunc: func(ctx context.Context, in *orchestratorv1.CancelPayoutRequest, opts ...grpc.CallOption) (*orchestratorv1.CancelPayoutResponse, error) {
			return nil, status.Error(codes.FailedPrecondition, `payout cannot be canceled in state "processing"`)
		},
	}

	router := NewRouterWithClient(zap.NewNop(), mockClient, nil, false)
	req := httptest.NewRequest(http.MethodPost, "/v1/payouts/some-id/cancel", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "cannot be canceled")
}

func TestCancelPayout_UpstreamError(t *testing.T) {
	mockClient := &mockPayoutClient{
		cancelPayoutFunc: func(ctx context.Context, in *orchestratorv1.CancelPayoutRequest, opts ...grpc.CallOption) (*orchestratorv1.CancelPayoutResponse, error) {
			return nil, status.Error(codes.Internal, "db error")
		},
	}

	router := NewRouterWithClient(zap.NewNop(), mockClient, nil, false)
	req := httptest.NewRequest(http.MethodPost, "/v1/payouts/some-id/cancel", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Contains(t, rec.Body.String(), "upstream error")
}

func TestCancelPayout_PassesIDToGRPC(t *testing.T) {
	var capturedID string

	mockClient := &mockPayoutClient{
		cancelPayoutFunc: func(ctx context.Context, in *orchestratorv1.CancelPayoutRequest, opts ...grpc.CallOption) (*orchestratorv1.CancelPayoutResponse, error) {
			capturedID = in.GetPayoutId()
			return &orchestratorv1.CancelPayoutResponse{Success: true}, nil
		},
	}

	router := NewRouterWithClient(zap.NewNop(), mockClient, nil, false)
	req := httptest.NewRequest(http.MethodPost, "/v1/payouts/my-payout-id/cancel", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "my-payout-id", capturedID)
}
