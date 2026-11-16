package infra

import (
	"github.com/dangduoc08/ginject/modules/cache"
)

var CacheModule = cache.Register(&cache.CacheModuleOptions{})
