package redisclient

import (
	"context"
	"fmt"

	"github.com/redis/rueidis"
)

// New creates a rueidis client and verifies connectivity with PING.
func New(ctx context.Context, addr, password string) (rueidis.Client, error) {
	opts := rueidis.ClientOption{
		InitAddress: []string{addr},
		Password:    password,
	}

	client, err := rueidis.NewClient(opts)
	if err != nil {
		return nil, fmt.Errorf("create redis client: %w", err)
	}

	if err := client.Do(ctx, client.B().Ping().Build()).Error(); err != nil {
		client.Close()
		return nil, fmt.Errorf("redis ping: %w", err)
	}

	return client, nil
}
