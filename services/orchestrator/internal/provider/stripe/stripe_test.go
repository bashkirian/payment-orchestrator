package stripe_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bashkirian/fintech-project/services/orchestrator/internal/domain"
	"github.com/bashkirian/fintech-project/services/orchestrator/internal/provider"
	stripeprovider "github.com/bashkirian/fintech-project/services/orchestrator/internal/provider/stripe"
)

// stripeHandler is a minimal Stripe API stub that routes:
//   POST /v1/payment_intents        → createHandler
//   POST /v1/payment_intents/*/cancel → cancelHandler
type stripeHandler struct {
	createHandler func(w http.ResponseWriter, r *http.Request)
	cancelHandler func(w http.ResponseWriter, r *http.Request)
}

func (h *stripeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/v1/payment_intents":
		h.createHandler(w, r)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/cancel"):
		h.cancelHandler(w, r)
	default:
		http.NotFound(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func stripeSuccessPI(id string) map[string]any {
	return map[string]any{
		"id":       id,
		"object":   "payment_intent",
		"status":   "succeeded",
		"amount":   1000,
		"currency": "usd",
		"livemode": false,
	}
}

func stripeErrorBody(errType, code, msg string) map[string]any {
	return map[string]any{
		"error": map[string]any{
			"type":    errType,
			"code":    code,
			"message": msg,
		},
	}
}

func newClient(t *testing.T, srv *httptest.Server) *stripeprovider.Client {
	t.Helper()
	return stripeprovider.New(stripeprovider.Config{
		APIKey:     "sk_test_dummy",
		MaxRetries: 0, // no retries so tests are fast
		BaseURL:    srv.URL,
	})
}

func testPayout() domain.Payout {
	extID := "pi_existing"
	return domain.Payout{
		ID:          uuid.New(),
		AmountCents: 1000,
		Currency:    "usd",
		Rail:        domain.RailCard,
		Provider:    domain.ProviderStripe,
		ExternalID:  &extID,
	}
}

// TestSendPayout_Success verifies a happy-path PaymentIntent creation.
func TestSendPayout_Success(t *testing.T) {
	const piID = "pi_test_success_001"
	srv := httptest.NewServer(&stripeHandler{
		createHandler: func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, stripeSuccessPI(piID))
		},
		cancelHandler: func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("cancel should not be called")
		},
	})
	defer srv.Close()

	payout := testPayout()
	externalID, err := newClient(t, srv).SendPayout(context.Background(), payout)

	require.NoError(t, err)
	assert.Equal(t, piID, externalID)
}

// TestSendPayout_CardDeclined verifies a card_error is wrapped as non-retryable.
func TestSendPayout_CardDeclined(t *testing.T) {
	srv := httptest.NewServer(&stripeHandler{
		createHandler: func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusPaymentRequired,
				stripeErrorBody("card_error", "card_declined", "Your card was declined."))
		},
		cancelHandler: func(w http.ResponseWriter, r *http.Request) {},
	})
	defer srv.Close()

	_, err := newClient(t, srv).SendPayout(context.Background(), testPayout())

	require.Error(t, err)
	var re *provider.RetryableError
	require.True(t, errors.As(err, &re), "expected *provider.RetryableError")
	assert.False(t, re.Retryable, "card_declined must be non-retryable")
}

// TestSendPayout_InvalidRequest verifies an invalid_request_error is non-retryable.
func TestSendPayout_InvalidRequest(t *testing.T) {
	srv := httptest.NewServer(&stripeHandler{
		createHandler: func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusBadRequest,
				stripeErrorBody("invalid_request_error", "parameter_invalid_empty", "amount is required"))
		},
		cancelHandler: func(w http.ResponseWriter, r *http.Request) {},
	})
	defer srv.Close()

	_, err := newClient(t, srv).SendPayout(context.Background(), testPayout())

	require.Error(t, err)
	var re *provider.RetryableError
	require.True(t, errors.As(err, &re))
	assert.False(t, re.Retryable)
}

// TestSendPayout_APIError verifies a 500 api_error is wrapped as retryable.
func TestSendPayout_APIError(t *testing.T) {
	srv := httptest.NewServer(&stripeHandler{
		createHandler: func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusInternalServerError,
				stripeErrorBody("api_error", "", "An internal error occurred."))
		},
		cancelHandler: func(w http.ResponseWriter, r *http.Request) {},
	})
	defer srv.Close()

	_, err := newClient(t, srv).SendPayout(context.Background(), testPayout())

	require.Error(t, err)
	var re *provider.RetryableError
	require.True(t, errors.As(err, &re))
	assert.True(t, re.Retryable, "api_error must be retryable")
}

// TestCancelPayout_Success verifies successful PaymentIntent cancellation.
func TestCancelPayout_Success(t *testing.T) {
	const piID = "pi_to_cancel"
	srv := httptest.NewServer(&stripeHandler{
		createHandler: func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("create should not be called")
		},
		cancelHandler: func(w http.ResponseWriter, r *http.Request) {
			assert.True(t, strings.Contains(r.URL.Path, piID))
			resp := stripeSuccessPI(piID)
			resp["status"] = "canceled"
			writeJSON(w, http.StatusOK, resp)
		},
	})
	defer srv.Close()

	extID := piID
	payout := testPayout()
	payout.ExternalID = &extID

	err := newClient(t, srv).CancelPayout(context.Background(), payout)
	require.NoError(t, err)
}

// TestCancelPayout_NoExternalID verifies that CancelPayout with a nil
// ExternalID returns a non-retryable error without hitting the network.
func TestCancelPayout_NoExternalID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("no HTTP call expected")
	}))
	defer srv.Close()

	payout := testPayout()
	payout.ExternalID = nil

	err := newClient(t, srv).CancelPayout(context.Background(), payout)

	require.Error(t, err)
	var re *provider.RetryableError
	require.True(t, errors.As(err, &re))
	assert.False(t, re.Retryable)
}
