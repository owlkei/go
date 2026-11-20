package memorycache

import "go.owlkei.com/owlkei/internal/inspect"

func init() {
	inspect.CacheStateOf = cacheStateOf
}

func cacheStateOf(backend any) (inspect.CacheState, bool) {
	mc, ok := backend.(*MemoryCache)
	if !ok || mc == nil {
		return inspect.CacheState{}, false
	}
	st := inspect.CacheState{
		MaxEntries: mc.maxEntriesPerShard * numShards,
		MaxBytes:   mc.maxBytesPerShard * numShards,
	}
	for i := range mc.shards {
		s := &mc.shards[i]
		s.mu.RLock()
		st.Entries += len(s.slotByKey)
		st.Bytes += s.bytes
		st.Evictions += s.evictions
		st.Expirations += s.expirations
		s.mu.RUnlock()
		st.Hits += s.hits.Load()
		st.Misses += s.misses.Load()
	}
	mc.sweepMu.Lock()
	st.IsSweeping = mc.isArmed
	mc.sweepMu.Unlock()
	return st, true
}
