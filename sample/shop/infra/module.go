package infra

import (
	"go.owlky.com/owlky/modules/cache"
)

var CacheModule = cache.Register(&cache.CacheModuleOptions{})
