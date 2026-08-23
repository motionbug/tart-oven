package main

import (
	"runtime"
	"runtime/debug"
)

const goMemoryReleaseThreshold = 64 << 20

type goMemory interface {
	ReadMemStats() runtime.MemStats
	FreeOSMemory()
}

type runtimeGoMemory struct{}

func (runtimeGoMemory) ReadMemStats() runtime.MemStats {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return stats
}

func (runtimeGoMemory) FreeOSMemory() { debug.FreeOSMemory() }

// maybeReleaseGoMemory returns idle heap pages to macOS only when Tart Oven is
// retaining enough unreleased memory to justify a synchronous GC/scavenge.
func maybeReleaseGoMemory(memory goMemory) (uint64, bool) {
	stats := memory.ReadMemStats()
	if stats.HeapReleased >= stats.HeapIdle {
		return 0, false
	}
	releasable := stats.HeapIdle - stats.HeapReleased
	if releasable < goMemoryReleaseThreshold {
		return releasable, false
	}
	memory.FreeOSMemory()
	return releasable, true
}

func deferVMStartForPressure(sample PerformanceSample) bool {
	return sample.PressureAvailable && sample.MemoryPressure == "critical"
}

func deferVMStartForHistory(history []PerformanceSample) bool {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].PressureAvailable {
			return deferVMStartForPressure(history[i])
		}
	}
	return false
}

func stopAllowedForState(state string) bool {
	return state != "suspended"
}
