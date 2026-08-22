package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestManagerUpdatePerformanceAppendsAndUpdatesHeader(t *testing.T) {
	source := &fakePerformanceSource{cpu: 55.6, memoryUsed: 2 << 30, memoryTotal: 8 << 30, vmUsed: 20 << 30, vmTotal: 80 << 30}
	m := &Manager{cfg: Config{VMStoragePath: "/vm"}, performanceCollector: &performanceCollector{source: source}}
	m.updatePerformance(time.Unix(100, 0))
	if len(m.performanceHistory) != 1 {
		t.Fatalf("history = %d", len(m.performanceHistory))
	}
	if m.hostStats.CPUPercent != 56 || m.hostStats.MemUsedMB != 2048 || m.hostStats.DiskTotalGB != 80 {
		t.Fatalf("header = %+v", m.hostStats)
	}
}

func TestManagerUpdatePerformanceDoesNotHoldManagerLockWhileCollecting(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	source := &fakePerformanceSource{cpuEntered: entered, cpuRelease: release}
	m := &Manager{cfg: Config{VMStoragePath: "/vm"}, performanceCollector: &performanceCollector{source: source}}

	done := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
			<-done
		}
	}()
	go func() {
		m.updatePerformance(time.Unix(100, 0))
		close(done)
	}()
	<-entered

	if !m.mu.TryLock() {
		t.Fatal("Manager lock held while collecting performance metrics")
	}
	m.mu.Unlock()
	close(release)
	released = true
	<-done
}

func TestPerformanceSnapshotCopiesHistory(t *testing.T) {
	m := &Manager{performanceHistory: []PerformanceSample{{UptimeSeconds: 1}}}
	s := m.performanceSnapshot()
	s.History[0].UptimeSeconds = 99
	if m.performanceHistory[0].UptimeSeconds != 1 {
		t.Fatal("history escaped Manager lock")
	}
}

func TestPerformanceEndpointReturnsOnlyPerformancePayload(t *testing.T) {
	m := &Manager{performanceHistory: []PerformanceSample{{CPUPercent: 12, CPUAvailable: true}}}
	w := httptest.NewRecorder()
	m.handlePerformance(w, httptest.NewRequest(http.MethodGet, "/api/performance", nil))
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), `"vms"`) || !strings.Contains(w.Body.String(), `"history"`) {
		t.Fatalf("response = %d %s", w.Code, w.Body.String())
	}
}

func TestPerformanceEndpointRejectsPost(t *testing.T) {
	m := &Manager{}
	w := httptest.NewRecorder()
	m.handlePerformance(w, httptest.NewRequest(http.MethodPost, "/api/performance", nil))
	if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("response = %d Allow=%q", w.Code, w.Header().Get("Allow"))
	}
}

func TestAppendPerformanceSampleKeepsNewest1440(t *testing.T) {
	var history []PerformanceSample
	for i := 0; i < performanceHistoryLimit+2; i++ {
		history = appendPerformanceSample(history, PerformanceSample{UptimeSeconds: uint64(i)})
	}
	if len(history) != performanceHistoryLimit {
		t.Fatalf("length = %d", len(history))
	}
	if history[0].UptimeSeconds != 2 || history[len(history)-1].UptimeSeconds != 1441 {
		t.Fatalf("wrong retained range: %d..%d", history[0].UptimeSeconds, history[len(history)-1].UptimeSeconds)
	}
}

func TestAppendPerformanceSampleDoesNotMutateInputAtCapacity(t *testing.T) {
	history := make([]PerformanceSample, performanceHistoryLimit, performanceHistoryLimit)
	history[0].UptimeSeconds = 7
	next := appendPerformanceSample(history, PerformanceSample{UptimeSeconds: 99})
	if history[0].UptimeSeconds != 7 || next[len(next)-1].UptimeSeconds != 99 {
		t.Fatal("input mutated")
	}
}

func TestPerformanceCollectorCollectsIndependentGroups(t *testing.T) {
	source := &fakePerformanceSource{cpu: 42.5, memoryUsed: 6 << 30, memoryTotal: 16 << 30, pressure: 1, systemUsed: 50 << 30, systemTotal: 100 << 30, vmUsed: 300 << 30, vmTotal: 500 << 30, reads: 1000, writes: 2000, uptime: 3600}
	c := performanceCollector{source: source}
	s := c.Collect(time.Unix(100, 0), "/Volumes/VMs")
	if s.CPUPercent != 42.5 || s.MemoryPressure != "warning" || s.VMDiskTotalBytes != 500<<30 || !s.UptimeAvailable {
		t.Fatalf("sample = %+v", s)
	}
	if s.DiskReadBytesPerSecond != 0 || s.DiskWriteBytesPerSecond != 0 {
		t.Fatal("first I/O rate is not zero")
	}
}

func TestPerformanceCollectorCalculatesDiskRatesAndCounterReset(t *testing.T) {
	source := &fakePerformanceSource{reads: 1000, writes: 2000}
	c := performanceCollector{source: source}
	c.Collect(time.Unix(100, 0), "/vm")
	source.reads, source.writes = 7000, 5000
	s := c.Collect(time.Unix(160, 0), "/vm")
	if s.DiskReadBytesPerSecond != 100 || s.DiskWriteBytesPerSecond != 50 {
		t.Fatalf("rates = %v/%v", s.DiskReadBytesPerSecond, s.DiskWriteBytesPerSecond)
	}
	source.reads, source.writes = 1, 1
	s = c.Collect(time.Unix(220, 0), "/vm")
	if s.DiskReadBytesPerSecond != 0 || s.DiskWriteBytesPerSecond != 0 {
		t.Fatal("counter reset produced a rate")
	}
}

func TestClampCPUPercent(t *testing.T) {
	tests := []struct {
		name    string
		percent float64
		want    float64
	}{
		{name: "below zero", percent: -0.1, want: 0},
		{name: "zero", percent: 0, want: 0},
		{name: "within range", percent: 42.5, want: 42.5},
		{name: "one hundred", percent: 100, want: 100},
		{name: "above one hundred", percent: 100.1, want: 100},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := clampCPUPercent(test.percent); got != test.want {
				t.Fatalf("clampCPUPercent(%v) = %v, want %v", test.percent, got, test.want)
			}
		})
	}
}

func TestPerformanceCollectorTreatsNonPositiveElapsedTimeAsZeroRate(t *testing.T) {
	tests := []struct {
		name       string
		secondTime time.Time
	}{
		{name: "same timestamp", secondTime: time.Unix(100, 0)},
		{name: "timestamp moved backwards", secondTime: time.Unix(99, 0)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := &fakePerformanceSource{reads: 1000, writes: 2000}
			collector := performanceCollector{source: source}
			collector.Collect(time.Unix(100, 0), "/vm")
			source.reads, source.writes = 7000, 5000

			sample := collector.Collect(test.secondTime, "/vm")
			if !sample.DiskIOAvailable {
				t.Fatal("disk I/O marked unavailable")
			}
			if sample.DiskReadBytesPerSecond != 0 || sample.DiskWriteBytesPerSecond != 0 {
				t.Fatalf("rates = %v/%v, want 0/0", sample.DiskReadBytesPerSecond, sample.DiskWriteBytesPerSecond)
			}
		})
	}
}

func TestPerformanceCollectorMarksOnlyFailedGroupUnavailable(t *testing.T) {
	tests := []struct {
		name string
		fail func(*fakePerformanceSource)
	}{
		{name: "cpu", fail: func(source *fakePerformanceSource) { source.cpuErr = errors.New("cpu unavailable") }},
		{name: "memory", fail: func(source *fakePerformanceSource) { source.memoryErr = errors.New("memory unavailable") }},
		{name: "pressure", fail: func(source *fakePerformanceSource) { source.pressureErr = errors.New("pressure unavailable") }},
		{name: "system disk", fail: func(source *fakePerformanceSource) { source.systemDiskErr = errors.New("system disk unavailable") }},
		{name: "vm disk", fail: func(source *fakePerformanceSource) { source.vmDiskErr = errors.New("vm disk unavailable") }},
		{name: "disk io", fail: func(source *fakePerformanceSource) { source.ioErr = errors.New("disk io unavailable") }},
		{name: "uptime", fail: func(source *fakePerformanceSource) { source.uptimeErr = errors.New("uptime unavailable") }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := &fakePerformanceSource{
				cpu: 25, memoryUsed: 6 << 30, memoryTotal: 16 << 30, pressure: 1,
				systemUsed: 50 << 30, systemTotal: 100 << 30,
				vmUsed: 300 << 30, vmTotal: 500 << 30,
				reads: 1000, writes: 2000, uptime: 3600,
			}
			test.fail(source)

			sample := (&performanceCollector{source: source}).Collect(time.Unix(100, 0), "/vm")
			availability := map[string]bool{
				"cpu": sample.CPUAvailable, "memory": sample.MemoryAvailable,
				"pressure": sample.PressureAvailable, "system disk": sample.SystemDiskAvailable,
				"vm disk": sample.VMDiskAvailable, "disk io": sample.DiskIOAvailable,
				"uptime": sample.UptimeAvailable,
			}
			for group, got := range availability {
				want := group != test.name
				if got != want {
					t.Errorf("%s availability = %t, want %t when %s fails", group, got, want, test.name)
				}
			}
		})
	}
}

func TestPerformanceCollectorKeepsCPUWhenMemoryFails(t *testing.T) {
	source := &fakePerformanceSource{cpu: 25, memoryErr: errors.New("unavailable"), uptime: 99}
	s := (&performanceCollector{source: source}).Collect(time.Unix(100, 0), "/vm")
	if !s.CPUAvailable || s.MemoryAvailable || !s.UptimeAvailable {
		t.Fatalf("flags = %+v", s)
	}
}

func TestPressureNameUsesAppleKernelLevels(t *testing.T) {
	for level, want := range map[int]string{0: "normal", 1: "warning", 2: "warning", 3: "critical", 4: "critical"} {
		if got := pressureName(level); got != want {
			t.Errorf("level %d = %q, want %q", level, got, want)
		}
	}
}

type fakePerformanceSource struct {
	cpu                             float64
	memoryUsed, memoryTotal         uint64
	pressure                        int
	systemUsed, systemTotal         uint64
	vmUsed, vmTotal                 uint64
	reads, writes                   uint64
	uptime                          uint64
	cpuErr, memoryErr, pressureErr  error
	systemDiskErr, vmDiskErr, ioErr error
	uptimeErr                       error
	cpuEntered                      chan struct{}
	cpuRelease                      chan struct{}
}

func (s *fakePerformanceSource) CPUPercent() (float64, error) {
	if s.cpuEntered != nil {
		close(s.cpuEntered)
		<-s.cpuRelease
	}
	return s.cpu, s.cpuErr
}

func (s *fakePerformanceSource) VirtualMemory() (uint64, uint64, error) {
	return s.memoryUsed, s.memoryTotal, s.memoryErr
}

func (s *fakePerformanceSource) MemoryPressure() (int, error) {
	return s.pressure, s.pressureErr
}

func (s *fakePerformanceSource) DiskUsage(path string) (uint64, uint64, error) {
	if path == "/" {
		return s.systemUsed, s.systemTotal, s.systemDiskErr
	}
	return s.vmUsed, s.vmTotal, s.vmDiskErr
}

func (s *fakePerformanceSource) DiskCounters() (uint64, uint64, error) {
	return s.reads, s.writes, s.ioErr
}

func (s *fakePerformanceSource) Uptime() (uint64, error) {
	return s.uptime, s.uptimeErr
}
