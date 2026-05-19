package http

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	apiconfig "github.com/bashkirian/fintech-project/services/api/internal/config"
)

func TestRequestIDPropagation(t *testing.T) {
	mockClient := &mockPayoutClient{}
	cfg := apiconfig.Config{RateLimitEnabled: false}
	router := NewRouterWithClient(zap.NewNop(), mockClient, nil, cfg)

	body := `{"amount": 5000, "currency": "USD", "rail": "card"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/payouts", bytes.NewBufferString(body))
	req.Header.Set("Idempotency-Key", "test-request-id-key")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)

	// Verify X-Request-Id header is set in response
	requestID := rec.Header().Get("X-Request-Id")
	assert.NotEmpty(t, requestID, "X-Request-Id header should be set")
}

func TestErrorResponseFormat(t *testing.T) {
	mockClient := &mockPayoutClient{}
	cfg := apiconfig.Config{RateLimitEnabled: false}
	router := NewRouterWithClient(zap.NewNop(), mockClient, nil, cfg)

	// Missing Idempotency-Key should return structured error
	body := `{"amount": 5000, "currency": "USD", "rail": "card"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/payouts", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	// Verify response contains both code and message
	assert.Contains(t, rec.Body.String(), `"code"`)
	assert.Contains(t, rec.Body.String(), `"message"`)
	assert.Contains(t, rec.Body.String(), "Idempotency-Key")
}
