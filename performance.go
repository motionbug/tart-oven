package main

import (
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
	"golang.org/x/sys/unix"
)

const performanceHistoryLimit = 1440

// PerformanceSample contains the host performance measurements captured at a
// point in time. Availability fields indicate whether the corresponding
// platform metric was available when the sample was collected.
type PerformanceSample struct {
	Timestamp               time.Time `json:"timestamp"`
	MemoryUsedBytes         uint64    `json:"memoryUsedBytes"`
	MemoryTotalBytes        uint64    `json:"memoryTotalBytes"`
	SystemDiskUsedBytes     uint64    `json:"systemDiskUsedBytes"`
	SystemDiskTotalBytes    uint64    `json:"systemDiskTotalBytes"`
	VMDiskUsedBytes         uint64    `json:"vmDiskUsedBytes"`
	VMDiskTotalBytes        uint64    `json:"vmDiskTotalBytes"`
	UptimeSeconds           uint64    `json:"uptimeSeconds"`
	CPUPercent              float64   `json:"cpuPercent"`
	DiskReadBytesPerSecond  float64   `json:"diskReadBytesPerSecond"`
	DiskWriteBytesPerSecond float64   `json:"diskWriteBytesPerSecond"`
	MemoryPressure          string    `json:"memoryPressure"`
	CPUAvailable            bool      `json:"cpuAvailable"`
	MemoryAvailable         bool      `json:"memoryAvailable"`
	PressureAvailable       bool      `json:"pressureAvailable"`
	SystemDiskAvailable     bool      `json:"systemDiskAvailable"`
	VMDiskAvailable         bool      `json:"vmDiskAvailable"`
	DiskIOAvailable         bool      `json:"diskIOAvailable"`
	UptimeAvailable         bool      `json:"uptimeAvailable"`
}

type PerformanceSnapshot struct {
	Latest  PerformanceSample   `json:"latest"`
	History []PerformanceSample `json:"history"`
}

type performanceSource interface {
	CPUPercent() (float64, error)
	VirtualMemory() (used, total uint64, err error)
	MemoryPressure() (int, error)
	DiskUsage(path string) (used, total uint64, err error)
	DiskCounters() (readBytes, writeBytes uint64, err error)
	Uptime() (uint64, error)
}

type performanceCollector struct {
	mu                           sync.Mutex
	source                       performanceSource
	previousDiskReadBytes        uint64
	previousDiskWriteBytes       uint64
	previousDiskCountersRecorded time.Time
}

func (m *Manager) updatePerformance(now time.Time) {
	m.mu.Lock()
	collector := m.performanceCollector
	vmStoragePath := m.cfg.VMStoragePath
	m.mu.Unlock()

	sample := collector.Collect(now, vmStoragePath)

	m.mu.Lock()
	m.performanceHistory = appendPerformanceSample(m.performanceHistory, sample)
	m.mu.Unlock()
}

func (m *Manager) performanceSnapshot() PerformanceSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()

	history := make([]PerformanceSample, len(m.performanceHistory))
	copy(history, m.performanceHistory)
	snapshot := PerformanceSnapshot{History: history}
	if len(history) > 0 {
		snapshot.Latest = history[len(history)-1]
	}
	return snapshot
}

func (m *Manager) handlePerformance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, m.performanceSnapshot())
}

func (c *performanceCollector) Collect(now time.Time, vmStoragePath string) PerformanceSample {
	c.mu.Lock()
	defer c.mu.Unlock()

	sample := PerformanceSample{Timestamp: now}

	if cpuPercent, err := c.source.CPUPercent(); err == nil {
		sample.CPUPercent = clampCPUPercent(cpuPercent)
		sample.CPUAvailable = true
	}
	if used, total, err := c.source.VirtualMemory(); err == nil {
		sample.MemoryUsedBytes = used
		sample.MemoryTotalBytes = total
		sample.MemoryAvailable = true
	}
	if pressure, err := c.source.MemoryPressure(); err == nil {
		sample.MemoryPressure = pressureName(pressure)
		sample.PressureAvailable = true
	}
	if used, total, err := c.source.DiskUsage("/"); err == nil {
		sample.SystemDiskUsedBytes = used
		sample.SystemDiskTotalBytes = total
		sample.SystemDiskAvailable = true
	}
	if used, total, err := c.source.DiskUsage(vmStoragePath); err == nil {
		sample.VMDiskUsedBytes = used
		sample.VMDiskTotalBytes = total
		sample.VMDiskAvailable = true
	}
	if reads, writes, err := c.source.DiskCounters(); err == nil {
		sample.DiskIOAvailable = true
		if !c.previousDiskCountersRecorded.IsZero() {
			elapsed := now.Sub(c.previousDiskCountersRecorded).Seconds()
			if elapsed > 0 && reads >= c.previousDiskReadBytes && writes >= c.previousDiskWriteBytes {
				sample.DiskReadBytesPerSecond = float64(reads-c.previousDiskReadBytes) / elapsed
				sample.DiskWriteBytesPerSecond = float64(writes-c.previousDiskWriteBytes) / elapsed
			}
		}
		c.previousDiskReadBytes = reads
		c.previousDiskWriteBytes = writes
		c.previousDiskCountersRecorded = now
	}
	if uptime, err := c.source.Uptime(); err == nil {
		sample.UptimeSeconds = uptime
		sample.UptimeAvailable = true
	}

	return sample
}

type systemPerformanceSource struct{}

func (systemPerformanceSource) CPUPercent() (float64, error) {
	percentages, err := cpu.Percent(0, false)
	if err != nil {
		return 0, err
	}
	if len(percentages) == 0 {
		return 0, errors.New("cpu usage unavailable")
	}
	return percentages[0], nil
}

func (systemPerformanceSource) VirtualMemory() (uint64, uint64, error) {
	stats, err := mem.VirtualMemory()
	if err != nil {
		return 0, 0, err
	}
	return stats.Used, stats.Total, nil
}

func (systemPerformanceSource) MemoryPressure() (int, error) {
	pressure, err := unix.SysctlUint32("kern.memorystatus_vm_pressure_level")
	return int(pressure), err
}

func (systemPerformanceSource) DiskUsage(path string) (uint64, uint64, error) {
	usage, err := disk.Usage(path)
	if err != nil {
		return 0, 0, err
	}
	return usage.Used, usage.Total, nil
}

func (systemPerformanceSource) DiskCounters() (uint64, uint64, error) {
	counters, err := disk.IOCounters()
	if err != nil {
		return 0, 0, err
	}

	var reads, writes uint64
	for _, counter := range counters {
		reads += counter.ReadBytes
		writes += counter.WriteBytes
	}
	return reads, writes, nil
}

func (systemPerformanceSource) Uptime() (uint64, error) {
	return host.Uptime()
}

func pressureName(level int) string {
	switch {
	case level <= 0:
		return "normal"
	case level <= 2:
		return "warning"
	default:
		return "critical"
	}
}

func clampCPUPercent(percent float64) float64 {
	if percent < 0 {
		return 0
	}
	if percent > 100 {
		return 100
	}
	return percent
}

func appendPerformanceSample(history []PerformanceSample, sample PerformanceSample) []PerformanceSample {
	if len(history) < performanceHistoryLimit {
		return append(history, sample)
	}

	next := make([]PerformanceSample, performanceHistoryLimit)
	copy(next, history[1:])
	next[performanceHistoryLimit-1] = sample
	return next
}
