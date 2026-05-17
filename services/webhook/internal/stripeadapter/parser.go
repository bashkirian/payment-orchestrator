package stripeadapter

import (
	"fmt"
	"time"

	stripe "github.com/stripe/stripe-go/v82"

	"github.com/bashkirian/fintech-project/services/webhook/internal/domain"
)

// EventParser converts Stripe webhook events into normalized ProviderEvent structs.
type EventParser struct{}

// NewEventParser creates a new Stripe event parser.
func NewEventParser() *EventParser {
	return &EventParser{}
}

// Parse converts a Stripe Event into a normalized ProviderEvent.
// Returns an error if the event type is not supported.
func (p *EventParser) Parse(event *stripe.Event) (domain.ProviderEvent, error) {
	externalID, err := p.extractExternalID(event)
	if err != nil {
		return domain.ProviderEvent{}, fmt.Errorf("extract external ID: %w", err)
	}

	eventType, err := p.mapEventType(event.Type)
	if err != nil {
		return domain.ProviderEvent{}, fmt.Errorf("map event type %q: %w", event.Type, err)
	}

	providerEvent := domain.ProviderEvent{
		Provider:   domain.ProviderStripe,
		ExternalID: externalID,
		EventID:    event.ID,
		Type:       eventType,
		Status:     domain.EventStatusReceived,
		OccurredAt: time.Unix(event.Created, 0),
	}

	// Extract error details for failed payments
	if eventType == domain.EventTypePayoutFailed {
		p.extractErrorDetails(event, &providerEvent)
	}

	return providerEvent, nil
}

// mapEventType converts Stripe event type to our normalized EventType.
// Returns an error for unsupported event types.
func (p *EventParser) mapEventType(stripeType stripe.EventType) (domain.EventType, error) {
	switch stripeType {
	case stripe.EventTypePaymentIntentSucceeded:
		return domain.EventTypePayoutSucceeded, nil
	case stripe.EventTypePaymentIntentPaymentFailed:
		return domain.EventTypePayoutFailed, nil
	case stripe.EventTypePaymentIntentCanceled:
		return domain.EventTypePayoutCanceled, nil
	default:
		return "", fmt.Errorf("unsupported event type: %s", stripeType)
	}
}

// extractExternalID extracts the PaymentIntent ID from the event data.
func (p *EventParser) extractExternalID(event *stripe.Event) (string, error) {
	// The data object contains the PaymentIntent
	if event.Data == nil || event.Data.Object == nil {
		return "", fmt.Errorf("event data.object is nil")
	}

	id, ok := event.Data.Object["id"].(string)
	if !ok || id == "" {
		return "", fmt.Errorf("event data.object.id is missing or not a string")
	}

	return id, nil
}

// extractErrorDetails populates error code and message for failed payment events.
func (p *EventParser) extractErrorDetails(event *stripe.Event, providerEvent *domain.ProviderEvent) {
	if event.Data == nil || event.Data.Object == nil {
		return
	}

	// Extract last_payment_error if present
	lastPaymentError, ok := event.Data.Object["last_payment_error"].(map[string]any)
	if !ok {
		return
	}

	if code, ok := lastPaymentError["code"].(string); ok {
		providerEvent.ErrorCode = code
	}
	if message, ok := lastPaymentError["message"].(string); ok {
		providerEvent.ErrorMessage = message
	}
}

// SupportedEventTypes returns the list of Stripe event types this parser handles.
func (p *EventParser) SupportedEventTypes() []string {
	return []string{
		"payment_intent.succeeded",
		"payment_intent.payment_failed",
		"payment_intent.canceled",
	}
}
