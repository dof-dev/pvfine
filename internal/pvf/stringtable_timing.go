package pvf

import "time"

// StringTableIndexStats counts actual lazy builds, not cached lookups.
// Durations and payload counts are cumulative for this archive instance.
type StringTableIndexStats struct {
	Revision          uint64  `json:"revision"`
	State             string  `json:"state"`
	ActiveBuilds      int     `json:"activeBuilds"`
	MappingBuilds     int     `json:"mappingBuilds"`
	MappingCount      int     `json:"mappingCount"`
	TableBuilds       int     `json:"tableBuilds"`
	FailedTables      int     `json:"failedTables"`
	Entries           int     `json:"entries"`
	Bytes             int64   `json:"bytes"`
	MappingDurationMs float64 `json:"mappingDurationMs"`
	TableDurationMs   float64 `json:"tableDurationMs"`
	BuildDurationMs   float64 `json:"buildDurationMs"`
}

// StringTableIndexEvent reports one mapping or UTF-16 payload index build.
type StringTableIndexEvent struct {
	ArchivePath string                `json:"archivePath"`
	Kind        string                `json:"kind"`
	Path        string                `json:"path"`
	State       string                `json:"state"`
	DurationMs  float64               `json:"durationMs"`
	Entries     int                   `json:"entries"`
	Bytes       int64                 `json:"bytes"`
	Error       string                `json:"error"`
	Stats       StringTableIndexStats `json:"stats"`
}

func (a *Archive) SetStringTableIndexObserver(observer func(StringTableIndexEvent)) {
	a.tables.mu.Lock()
	a.tables.observer = observer
	event := StringTableIndexEvent{
		ArchivePath: a.sourcePath, Kind: "snapshot", Stats: a.stringTableIndexStatsLocked(),
	}
	event.State = event.Stats.State
	a.tables.mu.Unlock()
	if observer != nil {
		observer(event)
	}
}

func (a *Archive) StringTableIndexStats() StringTableIndexStats {
	a.tables.mu.Lock()
	defer a.tables.mu.Unlock()
	return a.stringTableIndexStatsLocked()
}

func (a *Archive) stringTableIndexStatsLocked() StringTableIndexStats {
	stats := a.tables.stats
	stats.State = "idle"
	if stats.ActiveBuilds > 0 {
		stats.State = "building"
	} else if stats.MappingBuilds > 0 || stats.TableBuilds > 0 {
		stats.State = "ready"
	}
	return stats
}

func (a *Archive) reportStringTableIndex(event StringTableIndexEvent) {
	a.tables.mu.Lock()
	stats := &a.tables.stats
	stats.Revision++
	if event.State == "building" {
		stats.ActiveBuilds++
	} else {
		stats.ActiveBuilds--
		if event.Kind == "mapping" {
			stats.MappingBuilds++
			stats.MappingCount = event.Entries
			stats.MappingDurationMs += event.DurationMs
		} else {
			stats.TableBuilds++
			stats.TableDurationMs += event.DurationMs
			stats.Entries += event.Entries
			stats.Bytes += event.Bytes
			if event.State == "error" {
				stats.FailedTables++
			}
		}
		stats.BuildDurationMs = stats.MappingDurationMs + stats.TableDurationMs
	}
	event.ArchivePath = a.sourcePath
	event.Stats = a.stringTableIndexStatsLocked()
	observer := a.tables.observer
	a.tables.mu.Unlock()
	// Observers may inspect the snapshot; never invoke them under tables.mu.
	if observer != nil {
		observer(event)
	}
}

func stringTableElapsedMs(started time.Time) float64 {
	return float64(time.Since(started)) / float64(time.Millisecond)
}
