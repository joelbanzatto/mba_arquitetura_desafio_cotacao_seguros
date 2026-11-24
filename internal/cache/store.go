package cache

import (
	"context"
	"time"
)

// Store is a small cache boundary so the quotation service can be tested
// deterministically without a network dependency.
type Store interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
}
