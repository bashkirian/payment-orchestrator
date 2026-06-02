package queue

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/bashkirian/fintech-project/services/orchestrator/internal/config"
	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"
)

// KafkaQueue implements Publisher and Consumer using kafka-go.
type KafkaQueue struct {
	writer       *kafka.Writer
	reader       *kafka.Reader
	cfg          config.KafkaConfig
	retryCfg     config.RetryConfig
	log          *zap.Logger
}

// NewKafkaQueue creates a new Kafka-based queue.
func NewKafkaQueue(cfg config.KafkaConfig, retryCfg config.RetryConfig, log *zap.Logger) *KafkaQueue {
	return &KafkaQueue{
		cfg:      cfg,
		retryCfg: retryCfg,
		log:      log,
	}
}

// Connect initializes the Kafka writer and reader.
func (q *KafkaQueue) Connect(ctx context.Context) error {
	// Writer for publishing messages
	q.writer = &kafka.Writer{
		Addr:         kafka.TCP(q.cfg.Brokers...),
		Balancer:     &kafka.LeastBytes{},
		RequiredAcks: kafka.RequireOne,
		Async:        false,
	}

	// Reader for consuming messages (reads from all retry topics)
	q.reader = kafka.NewReader(kafka.ReaderConfig{
		Brokers:     q.cfg.Brokers,
		GroupID:     q.cfg.ConsumerGroup,
		Topic:       q.cfg.Topics.ProviderRetry, // primary topic
		MinBytes:    1,
		MaxBytes:    10e6, // 10MB
		StartOffset: kafka.FirstOffset,
	})

	q.log.Info("connected to Kafka",
		zap.Strings("brokers", q.cfg.Brokers),
		zap.String("consumer_group", q.cfg.ConsumerGroup),
	)

	return nil
}

// PublishProviderRetry queues a retry for the same provider.
func (q *KafkaQueue) PublishProviderRetry(ctx context.Context, msg RetryMessage) error {
	delay := q.calculateDelay(msg.Attempt, q.retryCfg.Provider)
	msg.ScheduledAt = time.Now().Add(delay)
	msg.RetryType = RetryTypeProvider

	return q.publish(ctx, q.cfg.Topics.ProviderRetry, msg)
}

// PublishGlobalRetry queues a retry with fresh provider selection.
func (q *KafkaQueue) PublishGlobalRetry(ctx context.Context, msg RetryMessage) error {
	delay := q.calculateDelay(msg.Attempt, q.retryCfg.Global)
	msg.ScheduledAt = time.Now().Add(delay)
	msg.RetryType = RetryTypeGlobal

	return q.publish(ctx, q.cfg.Topics.GlobalRetry, msg)
}

// PublishDLQ sends a message to the dead letter queue.
func (q *KafkaQueue) PublishDLQ(ctx context.Context, msg RetryMessage) error {
	msg.ScheduledAt = time.Now()
	return q.publish(ctx, q.cfg.Topics.DLQ, msg)
}

// publish sends a message to the specified topic.
func (q *KafkaQueue) publish(ctx context.Context, topic string, msg RetryMessage) error {
	msg.CreatedAt = time.Now()

	value, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal retry message: %w", err)
	}

	err = q.writer.WriteMessages(ctx, kafka.Message{
		Topic: topic,
		Key:   []byte(msg.PayoutID), // partition key for ordering
		Value: value,
		Headers: []kafka.Header{
			{Key: "retry_type", Value: []byte(msg.RetryType)},
		},
	})
	if err != nil {
		return fmt.Errorf("write to Kafka: %w", err)
	}

	q.log.Info("published retry message",
		zap.String("topic", topic),
		zap.String("payout_id", msg.PayoutID),
		zap.String("retry_type", string(msg.RetryType)),
		zap.Int("attempt", msg.Attempt),
		zap.Time("scheduled_at", msg.ScheduledAt),
	)

	return nil
}

// Consume starts consuming messages from retry topics.
func (q *KafkaQueue) Consume(ctx context.Context, handler func(ctx context.Context, msg RetryMessage) error) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		m, err := q.reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			q.log.Error("fetch message", zap.Error(err))
			continue
		}

		var msg RetryMessage
		if err := json.Unmarshal(m.Value, &msg); err != nil {
			q.log.Error("unmarshal message", zap.Error(err), zap.ByteString("raw", m.Value))
			// Commit the message to avoid reprocessing invalid messages
			if err := q.reader.CommitMessages(ctx, m); err != nil {
				q.log.Error("commit invalid message", zap.Error(err))
			}
			continue
		}

		// Wait until scheduled time (delayed execution)
		if time.Now().Before(msg.ScheduledAt) {
			delay := time.Until(msg.ScheduledAt)
			q.log.Debug("waiting for scheduled time",
				zap.String("payout_id", msg.PayoutID),
				zap.Duration("delay", delay),
			)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		// Process the message
		if err := handler(ctx, msg); err != nil {
			q.log.Error("handle retry message",
				zap.Error(err),
				zap.String("payout_id", msg.PayoutID),
			)
			// Don't commit on error - message will be redelivered
			continue
		}

		// Commit the message after successful processing
		if err := q.reader.CommitMessages(ctx, m); err != nil {
			q.log.Error("commit message", zap.Error(err))
		}
	}
}

// Close closes the Kafka connections.
func (q *KafkaQueue) Close() error {
	var errs []error

	if q.writer != nil {
		if err := q.writer.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close writer: %w", err))
		}
	}

	if q.reader != nil {
		if err := q.reader.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close reader: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("close errors: %v", errs)
	}

	return nil
}

// calculateDelay calculates the delay for a given attempt using tiered delays + jitter.
// Inspired by Hyperswitch's approach with added jitter for thundering herd prevention.
func (q *KafkaQueue) calculateDelay(attempt int, policy config.RetryPolicy) time.Duration {
	if attempt < 1 {
		attempt = 1
	}

	// Get delay from tiered configuration
	var delay time.Duration
	if attempt <= len(policy.Delays) {
		delay = policy.Delays[attempt-1]
	} else {
		// Use last configured delay for attempts beyond the list
		delay = policy.Delays[len(policy.Delays)-1]
	}

	// Add jitter to prevent thundering herd
	delay = q.addJitter(delay)

	return delay
}

// addJitter adds random jitter to the delay.
// Uses full jitter approach: delay + random(0, delay * max_percent/100)
func (q *KafkaQueue) addJitter(delay time.Duration) time.Duration {
	jitterCfg := q.retryCfg.Jitter

	// Ensure minimum jitter
	if jitterCfg.Min > 0 {
		delay += jitterCfg.Min
	}

	// Add percentage-based jitter
	if jitterCfg.MaxPercent > 0 {
		maxJitter := delay * time.Duration(jitterCfg.MaxPercent) / 100

		// Generate random jitter
		randJitter, err := rand.Int(rand.Reader, big.NewInt(int64(maxJitter)))
		if err == nil {
			delay += time.Duration(randJitter.Int64())
		}
	}

	return delay
}

// ParsePayoutID extracts payout ID from the message key or value.
func ParsePayoutID(m kafka.Message) (uuid.UUID, error) {
	return uuid.Parse(string(m.Key))
}
