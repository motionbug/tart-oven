package main

import "time"

const performanceHistoryLimit = 1440

// PerformanceSample contains the host performance measurements captured at a
// point in time. Availability fields indicate whether the corresponding
// platform metric was available when the sample was collected.
type PerformanceSample struct {
	Timestamp               time.Time `json:"timestamp"`
	CPUPercent              float64   `json:"cpuPercent"`
	CPUAvailable            bool      `json:"cpuAvailable"`
	MemoryUsedBytes         uint64    `json:"memoryUsedBytes"`
	MemoryTotalBytes        uint64    `json:"memoryTotalBytes"`
	MemoryAvailable         bool      `json:"memoryAvailable"`
	MemoryPressure          string    `json:"memoryPressure"`
	PressureAvailable       bool      `json:"pressureAvailable"`
	SystemDiskUsedBytes     uint64    `json:"systemDiskUsedBytes"`
	SystemDiskTotalBytes    uint64    `json:"systemDiskTotalBytes"`
	SystemDiskAvailable     bool      `json:"systemDiskAvailable"`
	VMDiskUsedBytes         uint64    `json:"vmDiskUsedBytes"`
	VMDiskTotalBytes        uint64    `json:"vmDiskTotalBytes"`
	VMDiskAvailable         bool      `json:"vmDiskAvailable"`
	DiskReadBytesPerSecond  float64   `json:"diskReadBytesPerSecond"`
	DiskWriteBytesPerSecond float64   `json:"diskWriteBytesPerSecond"`
	DiskIOAvailable         bool      `json:"diskIOAvailable"`
	UptimeSeconds           uint64    `json:"uptimeSeconds"`
	UptimeAvailable         bool      `json:"uptimeAvailable"`
}

type PerformanceSnapshot struct {
	Latest  PerformanceSample   `json:"latest"`
	History []PerformanceSample `json:"history"`
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
