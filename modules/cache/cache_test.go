package cache

import (
	"context"
	"testing"

	"github.com/dangduoc08/ginject/internal/inspect"
	"github.com/dangduoc08/ginject/internal/memorycache"
	"github.com/dangduoc08/ginject/internal/test"
)

func TestNewMemoryCache_NilUsesDefaults(t *testing.T) {
	got, ok := inspect.CacheStateOf(NewMemoryCache(nil))
	if !ok {
		t.Fatal(test.DiffMessage(ok, true, "NewMemoryCache returns the built-in cache"))
	}
	want, _ := inspect.CacheStateOf(memorycache.NewMemoryCache(nil))
	if got.MaxEntries != want.MaxEntries || got.MaxBytes != want.MaxBytes {
		t.Error(test.DiffMessage([]any{got.MaxEntries, got.MaxBytes}, []any{want.MaxEntries, want.MaxBytes}, "B19: nil gives the defaults Register(nil) uses"))
	}
}

func TestNewMemoryCache_PassesOptions(t *testing.T) {
	opts := &MemoryCacheOptions{MaxEntries: 1000, MaxBytes: 1 << 20}
	got, _ := inspect.CacheStateOf(NewMemoryCache(opts))
	want, _ := inspect.CacheStateOf(memorycache.NewMemoryCache(opts))
	if got.MaxEntries != want.MaxEntries || got.MaxBytes != want.MaxBytes || got.MaxEntries == 0 {
		t.Error(test.DiffMessage([]any{got.MaxEntries, got.MaxBytes}, []any{want.MaxEntries, want.MaxBytes}, "B19: the options reach the built-in cache"))
	}
	if err := NewMemoryCache(opts).Set(context.Background(), "k", []byte("v"), 0); err != nil {
		t.Error(test.DiffMessage(err, nil, "the returned Cache works"))
	}
}
