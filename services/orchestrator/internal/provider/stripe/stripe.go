// Package stripe implements provider.Client using the official Stripe Go SDK.
// SendPayout creates a PaymentIntent (immediate card charge); CancelPayout
// cancels it by the stored external_id.
package stripe

import (
	"context"
	"errors"
	"net/http"
	"time"

	stripe "github.com/stripe/stripe-go/v82"

	"github.com/bashkirian/fintech-project/services/orchestrator/internal/domain"
	"github.com/bashkirian/fintech-project/services/orchestrator/internal/provider"
)

// Config holds all tunables for the Stripe provider.
type Config struct {
	// APIKey is the Stripe secret key (sk_test_… or sk_live_…).
	APIKey string `yaml:"api_key"`
	// MaxRetries is the number of retries the SDK will attempt for
	// network errors, rate limits (429), and 5xx responses.
	// Defaults to 2 when zero.
	MaxRetries int64 `yaml:"max_retries"`
	// TimeoutSeconds is the per-request HTTP timeout.
	// Defaults to 30 when zero.
	TimeoutSeconds int `yaml:"timeout_seconds"`
	// BaseURL overrides the Stripe API base URL.  Used in unit tests to point
	// at a local httptest server.  Leave empty in production.
	BaseURL string `yaml:"-"`
}

// Client is the Stripe implementation of provider.Client.
type Client struct {
	sc *stripe.Client
}

// New constructs a ready-to-use Stripe Client from cfg.
func New(cfg Config) *Client {
	retries := cfg.MaxRetries
	if retries == 0 {
		retries = stripe.DefaultMaxNetworkRetries
	}
	timeoutSec := cfg.TimeoutSeconds
	if timeoutSec == 0 {
		timeoutSec = 30
	}

	backendCfg := &stripe.BackendConfig{
		HTTPClient:        &http.Client{Timeout: time.Duration(timeoutSec) * time.Second},
		MaxNetworkRetries: stripe.Int64(retries),
	}
	if cfg.BaseURL != "" {
		backendCfg.URL = stripe.String(cfg.BaseURL)
	}

	sc := stripe.NewClient(cfg.APIKey, stripe.WithBackends(stripe.NewBackendsWithConfig(backendCfg)))
	return &Client{sc: sc}
}

// SendPayout creates a Stripe PaymentIntent and immediately confirms it,
// returning the PaymentIntent ID (pi_…) as the external_id.
//
// In production the payment method would be fetched from the customer's stored
// payment methods. Here we use Stripe's built-in test token "pm_card_visa"
// which works with any test-mode API key.
func (c *Client) SendPayout(ctx context.Context, payout domain.Payout) (string, error) {
	params := &stripe.PaymentIntentCreateParams{
		Amount:             stripe.Int64(payout.AmountCents),
		Currency:           stripe.String(payout.Currency),
		PaymentMethodTypes: stripe.StringSlice([]string{"card"}),
		PaymentMethod:      stripe.String("pm_card_visa"),
		Confirm:            stripe.Bool(true),
		// OffSession because we charge server-side without the customer present.
		OffSession: stripe.Bool(true),
	}
	params.Context = ctx

	pi, err := c.sc.V1PaymentIntents.Create(ctx, params)
	if err != nil {
		return "", classifyError(err)
	}
	return pi.ID, nil
}

// CancelPayout cancels an existing PaymentIntent identified by payout.ExternalID.
func (c *Client) CancelPayout(ctx context.Context, payout domain.Payout) error {
	if payout.ExternalID == nil {
		return &provider.RetryableError{Retryable: false, Err: errors.New("stripe: payout has no external_id to cancel")}
	}
	_, err := c.sc.V1PaymentIntents.Cancel(ctx, *payout.ExternalID, nil)
	if err != nil {
		return classifyError(err)
	}
	return nil
}

// classifyError wraps a Stripe SDK error in a RetryableError.
// The SDK already retries network errors, 429, and 5xx internally; the errors
// that reach here have exhausted all retries or are terminal by nature.
func classifyError(err error) error {
	var stripeErr *stripe.Error
	if errors.As(err, &stripeErr) {
		switch stripeErr.Type {
		case stripe.ErrorTypeCard,
			stripe.ErrorTypeInvalidRequest,
			stripe.ErrorTypeIdempotency:
			return &provider.RetryableError{Retryable: false, Err: err}
		}
		// ErrorTypeAPI (5xx) errors that survived all SDK retries are still
		// considered retryable at the orchestrator level.
		return &provider.RetryableError{Retryable: true, Err: err}
	}
	// Context cancellation or other non-Stripe errors – pass through.
	return err
}
