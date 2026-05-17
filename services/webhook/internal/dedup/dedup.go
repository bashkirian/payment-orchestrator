package dedup

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/rueidis"
)

const (
	// DefaultTTL is the default time-to-live for event deduplication keys.
	DefaultTTL = 24 * time.Hour
	// KeyPrefix is the prefix for all deduplication keys in Redis.
	KeyPrefix = "webhook:event:"
)

// Deduplicator is the interface for event deduplication.
type Deduplicator interface {
	IsProcessed(ctx context.Context, eventID string) (bool, error)
}

// Service provides event deduplication using Redis SETNX.
type Service struct {
	client rueidis.Client
	ttl    time.Duration
}

// NewService creates a new deduplication service.
func NewService(client rueidis.Client, ttl time.Duration) *Service {
	if ttl == 0 {
		ttl = DefaultTTL
	}
	return &Service{
		client: client,
		ttl:    ttl,
	}
}

// IsProcessed checks if an event has already been processed.
// Returns true if the event was already seen (duplicate), false if this is a new event.
func (s *Service) IsProcessed(ctx context.Context, eventID string) (bool, error) {
	key := KeyPrefix + eventID

	// SETNX returns 1 if the key was set (new event), 0 if it already existed (duplicate)
	result, err := s.client.Do(ctx, s.client.B().Setnx().Key(key).Value("1").Build()).AsBool()
	if err != nil {
		return false, fmt.Errorf("redis SETNX: %w", err)
	}

	// If SETNX returned false, the key already existed -> duplicate
	if !result {
		return true, nil
	}

	// Key was set, now set expiry
	s.client.Do(ctx, s.client.B().Expire().Key(key).Seconds(int64(s.ttl.Seconds())).Build())

	// New event, not a duplicate
	return false, nil
}
