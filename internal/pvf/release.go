package pvf

// Release drops the large in-memory buffers and caches owned by an archive.
// Callers must ensure no archive operation is still running before invoking
// it. The archive object itself remains valid only as an empty shell after
// this call.
func (a *Archive) Release() int64 {
	if a == nil {
		return 0
	}

	a.cacheMu.Lock()
	released := int64(len(a.data) + len(a.pageKeys) + len(a.items)*24 + len(a.groups)*8)
	released += int64(len(a.strA) + len(a.strW))
	for _, value := range a.chunkCache {
		released += int64(len(value))
	}
	for _, value := range a.overlay {
		released += int64(len(value))
	}
	a.data = nil
	a.pageKeys = nil
	a.items = nil
	a.groups = nil
	a.strA = nil
	a.strW = nil
	a.strAIdx = nil
	a.strWIdx = nil
	a.resolveCache = nil
	a.resolveCacheOrder = nil
	a.resolveCacheBytes = 0
	a.chunkCache = nil
	a.chunkCacheMeta = nil
	a.chunkCacheBytes = 0
	a.chunkCacheClock = 0
	a.overlay = nil
	a.pathIndex = nil
	a.removedSpans = nil
	a.sourcePath = ""
	a.cacheMu.Unlock()

	a.tables.mu.Lock()
	a.tables.state = nil
	a.tables.mu.Unlock()

	a.scriptRenderer = nil
	a.canonicalScriptRenderer = nil
	a.hdr = Header{}
	a.keys = keySet{}
	a.format = formatProfile{}
	a.tableOff = 0
	a.hashOff = 0
	a.nameOff = 0
	a.grpiOff = 0
	a.bodyOff = 0
	a.hashSize = 0
	a.nameSize = 0
	a.grpiSize = 0
	return released
}
