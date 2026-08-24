package main

import (
	"runtime/debug"
	"runtime/metrics"
)

const goMemoryReleaseThreshold = 64 << 20

type goMemory interface {
	HeapMemory() (idle uint64, released uint64)
	FreeOSMemory()
}

type runtimeGoMemory struct{}

func (runtimeGoMemory) HeapMemory() (idle uint64, released uint64) {
	samples := []metrics.Sample{
		{Name: "/memory/classes/heap/idle:bytes"},
		{Name: "/memory/classes/heap/released:bytes"},
	}
	metrics.Read(samples)
	if samples[0].Value.Kind() == metrics.KindUint64 {
		idle = samples[0].Value.Uint64()
	}
	if samples[1].Value.Kind() == metrics.KindUint64 {
		released = samples[1].Value.Uint64()
	}
	return idle, released
}

func (runtimeGoMemory) FreeOSMemory() { debug.FreeOSMemory() }

// maybeReleaseGoMemory returns idle heap pages to macOS only when Tart Oven is
// retaining enough unreleased memory to justify a synchronous GC/scavenge.
func maybeReleaseGoMemory(memory goMemory) (uint64, bool) {
	idle, released := memory.HeapMemory()
	if released >= idle {
		return 0, false
	}
	releasable := idle - released
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
