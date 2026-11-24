package cache

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

const operationTimeout = 100 * time.Millisecond

// RedisStore deliberately treats Redis as an optional performance layer. A
// cache outage must not prevent live insurer quotes from being requested.
type RedisStore struct {
	client *redis.Client
}

func NewRedisStore(address string) *RedisStore {
	return &RedisStore{client: redis.NewClient(&redis.Options{Addr: address})}
}

func (s *RedisStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	operation, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()

	value, err := s.client.Get(operation, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return value, true, nil
}

func (s *RedisStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	operation, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	return s.client.Set(operation, key, value, ttl).Err()
}

func (s *RedisStore) Close() error {
	return s.client.Close()
}
