package cache

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"go.owlkei.com/owlkei/internal/core"
	"go.owlkei.com/owlkei/internal/memorycache"
	"go.owlkei.com/owlkei/internal/test"
)

type consumerA struct {
	Cache CacheService
}

func (c consumerA) NewProvider() core.Provider { return c }

type consumerB struct {
	Cache CacheService
}

func (c consumerB) NewProvider() core.Provider { return c }

func serviceOf(t *testing.T, app *core.App) CacheService {
	t.Helper()
	svc, ok := app.Get(CacheService{}).(CacheService)
	if !ok {
		t.Fatal(test.DiffMessage(app.Get(CacheService{}), "a CacheService", "the module provides CacheService"))
	}
	return svc
}

var background = context.Background()

func newService() CacheService {
	return CacheService{Backend: memorycache.NewMemoryCache(nil)}
}

func be(v int64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(v))
	return b
}

type recordedCall struct {
	method string
	ctx    context.Context
	key    string
	val    []byte
	ttl    time.Duration
	delta  int64
}

type recordingBackend struct {
	calls []recordedCall
}

type ctxKey struct{}

func (r *recordingBackend) Get(ctx context.Context, key string) ([]byte, bool) {
	r.calls = append(r.calls, recordedCall{method: "Get", ctx: ctx, key: key})
	return []byte("get-result"), true
}

func (r *recordingBackend) Set(ctx context.Context, key string, val []byte, ttl time.Duration) error {
	r.calls = append(r.calls, recordedCall{method: "Set", ctx: ctx, key: key, val: val, ttl: ttl})
	return errors.New("set-error")
}

func (r *recordingBackend) SetNX(ctx context.Context, key string, val []byte, ttl time.Duration) (bool, error) {
	r.calls = append(r.calls, recordedCall{method: "SetNX", ctx: ctx, key: key, val: val, ttl: ttl})
	return true, errors.New("setnx-error")
}

func (r *recordingBackend) Incr(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error) {
	r.calls = append(r.calls, recordedCall{method: "Incr", ctx: ctx, key: key, delta: delta, ttl: ttl})
	return 4242, errors.New("incr-error")
}

func (r *recordingBackend) Delete(ctx context.Context, key string) error {
	r.calls = append(r.calls, recordedCall{method: "Delete", ctx: ctx, key: key})
	return errors.New("delete-error")
}

func (r *recordingBackend) Keys(ctx context.Context) []string {
	r.calls = append(r.calls, recordedCall{method: "Keys", ctx: ctx})
	return []string{"keys-result"}
}

func (r *recordingBackend) TTL(ctx context.Context, key string) (time.Duration, bool) {
	r.calls = append(r.calls, recordedCall{method: "TTL", ctx: ctx, key: key})
	return 77 * time.Second, true
}

var _ Cache = (*recordingBackend)(nil)

var _ Cache = (*memorycache.MemoryCache)(nil)
