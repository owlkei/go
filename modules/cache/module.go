package cache

import (
	"go.owlkei.com/owlkei/internal/core"
	"go.owlkei.com/owlkei/internal/memorycache"
)

type CacheOnInitFn = func()

type CacheModuleOptions struct {
	IsGlobal bool
	OnInit   CacheOnInitFn
	Backend  Cache
}

func Register(opts *CacheModuleOptions) *core.Module {
	if opts == nil {
		opts = &CacheModuleOptions{}
	}

	backend := opts.Backend
	if backend == nil {
		backend = memorycache.NewMemoryCache(nil)
	}

	svc := CacheService{
		Backend: backend,
	}

	module := core.NewModuleBuilder().
		Providers(svc).
		Build()

	module.IsGlobal = opts.IsGlobal
	module.OnInit = opts.OnInit
	return module
}
