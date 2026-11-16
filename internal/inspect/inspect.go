package inspect

type CacheState struct {
	Entries, MaxEntries                  int
	Bytes, MaxBytes                      int64
	Hits, Misses, Evictions, Expirations uint64
	IsSweeping                           bool
}

type ConfigState struct {
	Files []string
	Keys  []string
}

var CacheStateOf = func(any) (CacheState, bool) {
	return CacheState{}, false
}

var ConfigStateOf = func(any) (ConfigState, bool) {
	return ConfigState{}, false
}
