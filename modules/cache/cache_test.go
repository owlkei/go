package cache

import (
	"context"
	"testing"

	"go.owlkei.com/owlkei/internal/inspect"
	"go.owlkei.com/owlkei/internal/memorycache"
	"go.owlkei.com/owlkei/internal/test"
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

func TestCacheErrors_Prefix(t *testing.T) {
	cases := map[error]string{
		ErrEmptyKey:      "cache: key must not be empty",
		ErrNegativeTTL:   "cache: ttl must not be negative",
		ErrEntryTooLarge: "cache: entry is larger than the cache allows",
		ErrNotCounter:    "cache: value is not a counter",
	}
	for err, want := range cases {
		if err.Error() != want {
			t.Error(test.DiffMessage(err.Error(), want, "B13: errors carry the prefix of the package users import"))
		}
	}
	if err := NewMemoryCache(nil).Set(context.Background(), "", nil, 0); err != ErrEmptyKey {
		t.Error(test.DiffMessage(err, ErrEmptyKey, "B13: the backend returns the sentinel callers compare"))
	}
}
