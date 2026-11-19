package cache

import (
	"context"
	"time"

	"go.owlky.com/owlky/internal/memorycache"
)

type Cache interface {
	Get(ctx context.Context, key string) ([]byte, bool)
	Set(ctx context.Context, key string, val []byte, ttl time.Duration) error
	SetNX(ctx context.Context, key string, val []byte, ttl time.Duration) (bool, error)
	Incr(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error)
	Delete(ctx context.Context, key string) error
	Keys(ctx context.Context) []string
	TTL(ctx context.Context, key string) (time.Duration, bool)
}

var (
	ErrEmptyKey      = memorycache.ErrEmptyKey
	ErrNegativeTTL   = memorycache.ErrNegativeTTL
	ErrEntryTooLarge = memorycache.ErrEntryTooLarge
	ErrNotCounter    = memorycache.ErrNotCounter
)

type MemoryCacheOptions = memorycache.MemoryCacheOptions

func NewMemoryCache(opts *MemoryCacheOptions) Cache {
	return memorycache.NewMemoryCache(opts)
}
