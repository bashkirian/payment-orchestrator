package stripeadapter

import (
	"testing"
	"time"

	stripe "github.com/stripe/stripe-go/v82"

	"github.com/bashkirian/fintech-project/services/webhook/internal/domain"
)

func TestEventParser_Parse(t *testing.T) {
	parser := NewEventParser()

	tests := []struct {
		name           string
		event          *stripe.Event
		wantType       domain.EventType
		wantExternalID string
		wantErr        bool
	}{
		{
			name: "payment_intent.succeeded",
			event: &stripe.Event{
				ID:      "evt_test_123",
				Type:    stripe.EventTypePaymentIntentSucceeded,
				Created: 1700000000,
				Data: &stripe.EventData{
					Object: map[string]any{
						"id": "pi_test_external_123",
					},
				},
			},
			wantType:       domain.EventTypePayoutSucceeded,
			wantExternalID: "pi_test_external_123",
			wantErr:        false,
		},
		{
			name: "payment_intent.payment_failed with error",
			event: &stripe.Event{
				ID:      "evt_test_456",
				Type:    stripe.EventTypePaymentIntentPaymentFailed,
				Created: 1700000000,
				Data: &stripe.EventData{
					Object: map[string]any{
						"id": "pi_test_external_456",
						"last_payment_error": map[string]any{
							"code":    "card_declined",
							"message": "Your card was declined.",
						},
					},
				},
			},
			wantType:       domain.EventTypePayoutFailed,
			wantExternalID: "pi_test_external_456",
			wantErr:        false,
		},
		{
			name: "payment_intent.canceled",
			event: &stripe.Event{
				ID:      "evt_test_789",
				Type:    stripe.EventTypePaymentIntentCanceled,
				Created: 1700000000,
				Data: &stripe.EventData{
					Object: map[string]any{
						"id": "pi_test_external_789",
					},
				},
			},
			wantType:       domain.EventTypePayoutCanceled,
			wantExternalID: "pi_test_external_789",
			wantErr:        false,
		},
		{
			name: "unsupported event type",
			event: &stripe.Event{
				ID:      "evt_test_unsupported",
				Type:    stripe.EventTypePaymentIntentCreated,
				Created: 1700000000,
				Data: &stripe.EventData{
					Object: map[string]any{
						"id": "pi_test_external",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "missing object id",
			event: &stripe.Event{
				ID:      "evt_test_missing_id",
				Type:    stripe.EventTypePaymentIntentSucceeded,
				Created: 1700000000,
				Data: &stripe.EventData{
					Object: map[string]any{
						"amount": 1000,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "nil data object",
			event: &stripe.Event{
				ID:      "evt_test_nil_data",
				Type:    stripe.EventTypePaymentIntentSucceeded,
				Created: 1700000000,
				Data:    nil,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parser.Parse(tt.event)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Parse() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Parse() unexpected error: %v", err)
				return
			}

			if got.Provider != domain.ProviderStripe {
				t.Errorf("Parse() Provider = %v, want %v", got.Provider, domain.ProviderStripe)
			}
			if got.EventID != tt.event.ID {
				t.Errorf("Parse() EventID = %v, want %v", got.EventID, tt.event.ID)
			}
			if got.Type != tt.wantType {
				t.Errorf("Parse() Type = %v, want %v", got.Type, tt.wantType)
			}
			if got.ExternalID != tt.wantExternalID {
				t.Errorf("Parse() ExternalID = %v, want %v", got.ExternalID, tt.wantExternalID)
			}
			if got.Status != domain.EventStatusReceived {
				t.Errorf("Parse() Status = %v, want %v", got.Status, domain.EventStatusReceived)
			}
			// Verify timestamp
			expectedTime := time.Unix(tt.event.Created, 0)
			if !got.OccurredAt.Equal(expectedTime) {
				t.Errorf("Parse() OccurredAt = %v, want %v", got.OccurredAt, expectedTime)
			}
		})
	}
}

func TestEventParser_ExtractErrorDetails(t *testing.T) {
	parser := NewEventParser()

	event := &stripe.Event{
		ID:      "evt_test_failed",
		Type:    stripe.EventTypePaymentIntentPaymentFailed,
		Created: 1700000000,
		Data: &stripe.EventData{
			Object: map[string]any{
				"id": "pi_test_failed",
				"last_payment_error": map[string]any{
					"code":    "insufficient_funds",
					"message": "Insufficient funds in the account",
				},
			},
		},
	}

	got, err := parser.Parse(event)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}

	if got.ErrorCode != "insufficient_funds" {
		t.Errorf("ErrorCode = %v, want insufficient_funds", got.ErrorCode)
	}
	if got.ErrorMessage != "Insufficient funds in the account" {
		t.Errorf("ErrorMessage = %v, want 'Insufficient funds in the account'", got.ErrorMessage)
	}
}

func TestEventParser_SupportedEventTypes(t *testing.T) {
	parser := NewEventParser()
	types := parser.SupportedEventTypes()

	expected := []string{
		"payment_intent.succeeded",
		"payment_intent.payment_failed",
		"payment_intent.canceled",
	}

	if len(types) != len(expected) {
		t.Errorf("SupportedEventTypes() returned %d types, want %d", len(types), len(expected))
	}

	for i, want := range expected {
		if types[i] != want {
			t.Errorf("SupportedEventTypes()[%d] = %v, want %v", i, types[i], want)
		}
	}
}

func TestProviderEvent_IsProcessed(t *testing.T) {
	tests := []struct {
		name   string
		event  domain.ProviderEvent
		expect bool
	}{
		{
			name: "processed event",
			event: domain.ProviderEvent{
				Status: domain.EventStatusProcessed,
			},
			expect: true,
		},
		{
			name: "received event",
			event: domain.ProviderEvent{
				Status: domain.EventStatusReceived,
			},
			expect: false,
		},
		{
			name: "failed event",
			event: domain.ProviderEvent{
				Status: domain.EventStatusFailed,
			},
			expect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.event.IsProcessed(); got != tt.expect {
				t.Errorf("IsProcessed() = %v, want %v", got, tt.expect)
			}
		})
	}
}

func TestProviderEvent_IsFailure(t *testing.T) {
	tests := []struct {
		name   string
		event  domain.ProviderEvent
		expect bool
	}{
		{
			name: "failed payout",
			event: domain.ProviderEvent{
				Type: domain.EventTypePayoutFailed,
			},
			expect: true,
		},
		{
			name: "succeeded payout",
			event: domain.ProviderEvent{
				Type: domain.EventTypePayoutSucceeded,
			},
			expect: false,
		},
		{
			name: "canceled payout",
			event: domain.ProviderEvent{
				Type: domain.EventTypePayoutCanceled,
			},
			expect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.event.IsFailure(); got != tt.expect {
				t.Errorf("IsFailure() = %v, want %v", got, tt.expect)
			}
		})
	}
}
