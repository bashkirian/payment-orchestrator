package domain

import (
	"time"
)

// Provider identifies the payment provider that sent the event.
type Provider string

const (
	ProviderStripe Provider = "stripe"
	// Future providers can be added here:
	// ProviderPayPal  Provider = "paypal"
	// ProviderCrypto  Provider = "crypto"
)

// EventType categorizes the kind of provider event.
// Designed for extensibility - new types can be added without breaking changes.
type EventType string

const (
	EventTypePayoutSucceeded EventType = "payout_succeeded"
	EventTypePayoutFailed    EventType = "payout_failed"
	EventTypePayoutCanceled  EventType = "payout_canceled"
	// Future event types:
	// EventTypePayoutProcessing  EventType = "payout_processing"
	// EventTypePayoutRefunded    EventType = "payout_refunded"
	// EventTypePayoutPending     EventType = "payout_pending"
)

// EventStatus represents the outcome state of the event processing.
type EventStatus string

const (
	EventStatusReceived   EventStatus = "received"   // Event received, not yet processed
	EventStatusProcessing EventStatus = "processing" // Event is being handled
	EventStatusProcessed  EventStatus = "processed"  // Event successfully processed
	EventStatusFailed     EventStatus = "failed"     // Event processing failed
)

// ProviderEvent is a normalized representation of a webhook event from any payment provider.
// It abstracts away provider-specific details and provides a consistent interface for processing.
type ProviderEvent struct {
	// Provider identifies which payment provider sent this event.
	Provider Provider

	// ExternalID is the provider's resource identifier (e.g., Stripe PaymentIntent ID "pi_xxx").
	// Used to correlate the event with our internal payout record.
	ExternalID string

	// EventID is the provider's unique identifier for this specific event (e.g., Stripe Event ID "evt_xxx").
	// Used for idempotency - ensures each webhook event is processed exactly once.
	EventID string

	// Type categorizes the event (succeeded, failed, canceled, etc.).
	Type EventType

	// Status tracks the processing state of this event in our system.
	Status EventStatus

	// OccurredAt is the timestamp when the event occurred at the provider.
	OccurredAt time.Time

	// RawPayload contains the original event payload for debugging/audit purposes.
	// Optional - can be nil if not needed.
	RawPayload []byte

	// ErrorCode contains provider-specific error details for failed events.
	// Optional - only populated for failure events.
	ErrorCode string

	// ErrorMessage contains a human-readable error description.
	// Optional - only populated for failure events.
	ErrorMessage string
}

// IsProcessed returns true if the event has been successfully processed.
func (e ProviderEvent) IsProcessed() bool {
	return e.Status == EventStatusProcessed
}

// IsFailure returns true if this event represents a failed payout.
func (e ProviderEvent) IsFailure() bool {
	return e.Type == EventTypePayoutFailed
}
