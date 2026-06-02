package queue

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/bashkirian/fintech-project/services/orchestrator/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestCalculateDelay(t *testing.T) {
	tests := []struct {
		name       string
		attempt    int
		policy     config.RetryPolicy
		wantMinDur time.Duration
		wantMaxDur time.Duration
	}{
		{
			name:    "first attempt uses first delay",
			attempt: 1,
			policy: config.RetryPolicy{
				Delays: []time.Duration{1 * time.Second, 5 * time.Second, 15 * time.Second},
			},
			wantMinDur: 1 * time.Second,
			wantMaxDur: 2 * time.Second, // with up to 20% jitter
		},
		{
			name:    "second attempt uses second delay",
			attempt: 2,
			policy: config.RetryPolicy{
				Delays: []time.Duration{1 * time.Second, 5 * time.Second, 15 * time.Second},
			},
			wantMinDur: 5 * time.Second,
			wantMaxDur: 10 * time.Second, // with jitter
		},
		{
			name:    "attempt beyond delays uses last delay",
			attempt: 5,
			policy: config.RetryPolicy{
				Delays: []time.Duration{1 * time.Second, 5 * time.Second, 15 * time.Second},
			},
			wantMinDur: 15 * time.Second,
			wantMaxDur: 30 * time.Second, // with jitter
		},
		{
			name:    "zero attempt treated as first",
			attempt: 0,
			policy: config.RetryPolicy{
				Delays: []time.Duration{1 * time.Second, 5 * time.Second},
			},
			wantMinDur: 1 * time.Second,
			wantMaxDur: 2 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := &KafkaQueue{
				retryCfg: config.RetryConfig{
					Jitter: config.JitterConfig{
						Min:        100 * time.Millisecond,
						MaxPercent: 20,
					},
				},
			}

			got := q.calculateDelay(tt.attempt, tt.policy)

			// Due to randomness, we check bounds
			assert.GreaterOrEqual(t, got, tt.wantMinDur+100*time.Millisecond) // min jitter added
			assert.LessOrEqual(t, got, tt.wantMaxDur+100*time.Millisecond)
		})
	}
}

func TestAddJitter(t *testing.T) {
	tests := []struct {
		name     string
		delay    time.Duration
		jitter   config.JitterConfig
		wantMin  time.Duration
		wantMax  time.Duration
	}{
		{
			name:  "adds minimum jitter",
			delay: 1 * time.Second,
			jitter: config.JitterConfig{
				Min:        100 * time.Millisecond,
				MaxPercent: 0,
			},
			wantMin: 1100 * time.Millisecond,
			wantMax: 1100 * time.Millisecond,
		},
		{
			name:  "adds percentage jitter",
			delay: 1 * time.Second,
			jitter: config.JitterConfig{
				Min:        0,
				MaxPercent: 20,
			},
			wantMin: 1 * time.Second,
			wantMax: 1200 * time.Millisecond,
		},
		{
			name:  "adds both jitter types",
			delay: 1 * time.Second,
			jitter: config.JitterConfig{
				Min:        100 * time.Millisecond,
				MaxPercent: 20,
			},
			wantMin: 1100 * time.Millisecond,
			// max = delay + min + (delay + min) * 20% = 1000 + 100 + 220 = 1320ms
			wantMax: 1320 * time.Millisecond,
		},
		{
			name:  "no jitter configured",
			delay: 1 * time.Second,
			jitter: config.JitterConfig{},
			wantMin: 1 * time.Second,
			wantMax: 1 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := &KafkaQueue{
				retryCfg: config.RetryConfig{
					Jitter: tt.jitter,
				},
			}

			// Run multiple times to test randomness
			for i := 0; i < 100; i++ {
				got := q.addJitter(tt.delay)
				assert.GreaterOrEqual(t, got, tt.wantMin)
				assert.LessOrEqual(t, got, tt.wantMax)
			}
		})
	}
}

func TestRetryMessageSerialization(t *testing.T) {
	msg := RetryMessage{
		PayoutID:     "123e4567-e89b-12d3-a456-426614174000",
		Attempt:      2,
		MaxAttempts:  5,
		RetryType:    RetryTypeProvider,
		Provider:     "stripe",
		LastError:    "connection timeout",
		ScheduledAt:  time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		CreatedAt:    time.Date(2024, 1, 1, 11, 59, 0, 0, time.UTC),
	}

	// Test JSON serialization using encoding/json
	data, err := json.Marshal(msg)
	assert.NoError(t, err)
	assert.Contains(t, string(data), `"payout_id":"123e4567-e89b-12d3-a456-426614174000"`)
	assert.Contains(t, string(data), `"retry_type":"provider"`)

	// Test deserialization
	var decoded RetryMessage
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, msg.PayoutID, decoded.PayoutID)
	assert.Equal(t, msg.Attempt, decoded.Attempt)
	assert.Equal(t, msg.RetryType, decoded.RetryType)
	assert.Equal(t, msg.Provider, decoded.Provider)
}
