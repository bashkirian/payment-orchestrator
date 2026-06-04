package queue

import (
	"context"
	"time"

	"github.com/bashkirian/fintech-project/services/orchestrator/internal/domain"
)

// RetryType indicates the retry level (provider or global).
type RetryType string

const (
	RetryTypeProvider RetryType = "provider" // retry same provider
	RetryTypeGlobal   RetryType = "global"   // retry with fresh provider selection
)

// RetryMessage represents a message in the retry queue.
type RetryMessage struct {
	PayoutID         string          `json:"payout_id"`
	Attempt          int             `json:"attempt"`           // current attempt number (1-indexed)
	MaxAttempts      int             `json:"max_attempts"`
	RetryType        RetryType       `json:"retry_type"`
	Provider         domain.Provider `json:"provider"`          // for provider-level retry
	LastError        string          `json:"last_error"`
	ScheduledAt      time.Time       `json:"scheduled_at"`      // when to process
	CreatedAt        time.Time       `json:"created_at"`
}

// Publisher publishes messages to retry queues.
type Publisher interface {
	// PublishProviderRetry queues a retry for the same provider.
	PublishProviderRetry(ctx context.Context, msg RetryMessage) error

	// PublishGlobalRetry queues a retry with fresh provider selection.
	PublishGlobalRetry(ctx context.Context, msg RetryMessage) error

	// PublishDLQ sends a message to the dead letter queue (max retries exceeded).
	PublishDLQ(ctx context.Context, msg RetryMessage) error

	// Close closes the publisher connection.
	Close() error
}

// Consumer consumes messages from retry queues.
type Consumer interface {
	// Consume starts consuming messages from retry topics.
	// Messages are delivered to the handler function.
	Consume(ctx context.Context, handler func(ctx context.Context, msg RetryMessage) error) error

	// Close closes the consumer connection.
	Close() error
}
