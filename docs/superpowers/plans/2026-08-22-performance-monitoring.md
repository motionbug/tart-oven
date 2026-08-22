# Performance Monitoring Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship Tart Oven 1.30 with a dedicated, dependency-free host Performance page containing current metrics and 24-hour charts sampled every 60 seconds.

**Architecture:** A focused Go collector creates independent metric-group results and converts cumulative disk counters into rates. `Manager` retains a 1,440-sample in-memory ring, exposes it through a dedicated endpoint, and continues sending only the latest compact host summary through existing state snapshots; the embedded browser UI draws responsive canvas charts.

**Tech Stack:** Go 1.24.3, `github.com/shirou/gopsutil/v3` v3.24.5, `golang.org/x/sys/unix`, `net/http`, embedded HTML/CSS/JavaScript canvas, Go standard-library tests.

**Spec:** `docs/superpowers/specs/2026-08-22-performance-monitoring-design.md`

## Global Constraints

- Release version is exactly `1.30`.
- Sample once every 60 seconds and retain exactly 1,440 samples in memory.
- History resets when the Tart Oven process restarts.
- Everything ships in the Go binary; do not add external commands, services, JavaScript packages, or runtime assets.
- Thresholds change colours only; do not generate alerts or log entries.
- One metric-group failure must not suppress other metrics.
- Preserve the existing `/api/vms` and SSE payload size by excluding performance history.
- Keep the existing header summary and replace its five-minute load value with actual CPU utilization.

---

### Task 1: Performance model and bounded history

**Files:**
- Create: `performance.go`
- Create: `performance_test.go`

**Interfaces:**
- Produces: `const performanceHistoryLimit = 1440`.
- Produces: `PerformanceSample` with JSON fields `timestamp`, `cpuPercent`, `cpuAvailable`, `memoryUsedBytes`, `memoryTotalBytes`, `memoryAvailable`, `memoryPressure`, `pressureAvailable`, `systemDiskUsedBytes`, `systemDiskTotalBytes`, `systemDiskAvailable`, `vmDiskUsedBytes`, `vmDiskTotalBytes`, `vmDiskAvailable`, `diskReadBytesPerSecond`, `diskWriteBytesPerSecond`, `diskIOAvailable`, `uptimeSeconds`, and `uptimeAvailable`.
- Produces: `PerformanceSnapshot` with `latest` and `history` JSON fields.
- Produces: `appendPerformanceSample([]PerformanceSample, PerformanceSample) []PerformanceSample`.

- [ ] **Step 1: Write the failing bounded-history tests**

```go
func TestAppendPerformanceSampleKeepsNewest1440(t *testing.T) {
	var history []PerformanceSample
	for i := 0; i < performanceHistoryLimit+2; i++ {
		history = appendPerformanceSample(history, PerformanceSample{UptimeSeconds: uint64(i)})
	}
	if len(history) != performanceHistoryLimit { t.Fatalf("length = %d", len(history)) }
	if history[0].UptimeSeconds != 2 || history[len(history)-1].UptimeSeconds != 1441 {
		t.Fatalf("wrong retained range: %d..%d", history[0].UptimeSeconds, history[len(history)-1].UptimeSeconds)
	}
}

func TestAppendPerformanceSampleDoesNotMutateInputAtCapacity(t *testing.T) {
	history := make([]PerformanceSample, performanceHistoryLimit, performanceHistoryLimit)
	history[0].UptimeSeconds = 7
	next := appendPerformanceSample(history, PerformanceSample{UptimeSeconds: 99})
	if history[0].UptimeSeconds != 7 || next[len(next)-1].UptimeSeconds != 99 { t.Fatal("input mutated") }
}
```

- [ ] **Step 2: Run test to verify RED**

Run: `go test ./... -run TestAppendPerformanceSample -count=1`

Expected: FAIL because the performance model does not exist.

- [ ] **Step 3: Implement the model and bounded append**

Define the exact fields above with concrete types: time as `time.Time`, percentages and rates as `float64`, byte and uptime values as `uint64`, availability as `bool`, and pressure as `string`. Append normally below the cap. At the cap, allocate a new 1,440-element slice, copy `history[1:]`, and place the new sample last.

- [ ] **Step 4: Verify GREEN**

Run: `gofmt -w performance.go performance_test.go && go test ./... -run TestAppendPerformanceSample -count=1 && go test ./... -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add performance.go performance_test.go
git commit -m "feat: add bounded performance history model"
```

### Task 2: Go-native host metrics collector

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Modify: `performance.go`
- Modify: `performance_test.go`

**Interfaces:**
- Consumes: `PerformanceSample` from Task 1.
- Produces: `performanceSource` methods `CPUPercent()`, `VirtualMemory()`, `MemoryPressure()`, `DiskUsage(path)`, `DiskCounters()`, and `Uptime()`.
- Produces: `performanceCollector` holding the source and previous disk counters/time.
- Produces: `(*performanceCollector).Collect(now time.Time, vmStoragePath string) PerformanceSample`.
- Produces: `systemPerformanceSource` backed by gopsutil and `unix.SysctlUint32("kern.memorystatus_vm_pressure_level")`.
- Produces: `pressureName(int) string`, mapping 0 to `normal`, 1-2 to `warning`, and 3+ to `critical`.

- [ ] **Step 1: Write failing collector tests using a deterministic fake source**

```go
func TestPerformanceCollectorCollectsIndependentGroups(t *testing.T) {
	source := &fakePerformanceSource{cpu: 42.5, memoryUsed: 6 << 30, memoryTotal: 16 << 30, pressure: 1, systemUsed: 50 << 30, systemTotal: 100 << 30, vmUsed: 300 << 30, vmTotal: 500 << 30, reads: 1000, writes: 2000, uptime: 3600}
	c := performanceCollector{source: source}
	s := c.Collect(time.Unix(100, 0), "/Volumes/VMs")
	if s.CPUPercent != 42.5 || s.MemoryPressure != "warning" || s.VMDiskTotalBytes != 500<<30 || !s.UptimeAvailable { t.Fatalf("sample = %+v", s) }
	if s.DiskReadBytesPerSecond != 0 || s.DiskWriteBytesPerSecond != 0 { t.Fatal("first I/O rate is not zero") }
}

func TestPerformanceCollectorCalculatesDiskRatesAndCounterReset(t *testing.T) {
	source := &fakePerformanceSource{reads: 1000, writes: 2000}
	c := performanceCollector{source: source}
	c.Collect(time.Unix(100, 0), "/vm")
	source.reads, source.writes = 7000, 5000
	s := c.Collect(time.Unix(160, 0), "/vm")
	if s.DiskReadBytesPerSecond != 100 || s.DiskWriteBytesPerSecond != 50 { t.Fatalf("rates = %v/%v", s.DiskReadBytesPerSecond, s.DiskWriteBytesPerSecond) }
	source.reads, source.writes = 1, 1
	s = c.Collect(time.Unix(220, 0), "/vm")
	if s.DiskReadBytesPerSecond != 0 || s.DiskWriteBytesPerSecond != 0 { t.Fatal("counter reset produced a rate") }
}

func TestPerformanceCollectorKeepsCPUWhenMemoryFails(t *testing.T) {
	source := &fakePerformanceSource{cpu: 25, memoryErr: errors.New("unavailable"), uptime: 99}
	s := (&performanceCollector{source: source}).Collect(time.Unix(100, 0), "/vm")
	if !s.CPUAvailable || s.MemoryAvailable || !s.UptimeAvailable { t.Fatalf("flags = %+v", s) }
}

func TestPressureNameUsesAppleKernelLevels(t *testing.T) {
	for level, want := range map[int]string{0: "normal", 1: "warning", 2: "warning", 3: "critical", 4: "critical"} {
		if got := pressureName(level); got != want { t.Errorf("level %d = %q, want %q", level, got, want) }
	}
}
```

The fake returns `/` capacity separately from the supplied VM path and exposes one error field per metric group.

- [ ] **Step 2: Run tests to verify RED**

Run: `go test ./... -run 'TestPerformanceCollector|TestPressureName' -count=1`

Expected: FAIL because the collector does not exist.

- [ ] **Step 3: Add the pinned module**

Run: `go get github.com/shirou/gopsutil/v3@v3.24.5 && go mod tidy`

Expected: `go.mod` retains Go 1.24.3 and adds gopsutil v3.24.5 directly.

- [ ] **Step 4: Implement the minimal collector**

Use `cpu.Percent(0, false)`, `mem.VirtualMemory()`, `disk.Usage(path)`, `disk.IOCounters()`, `host.Uptime()`, and `unix.SysctlUint32`. Sum physical-disk counters, clamp CPU to 0-100, and treat first observation, non-positive elapsed time, or a counter decrease as zero throughput. Call every group independently and set only its own availability flag.

- [ ] **Step 5: Verify GREEN**

Run: `gofmt -w performance.go performance_test.go && go test ./... -run 'TestPerformanceCollector|TestPressureName' -count=1 && go test ./... -count=1`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum performance.go performance_test.go
git commit -m "feat: collect native host performance metrics"
```

### Task 3: Manager scheduling and history API

**Files:**
- Modify: `main.go`
- Modify: `performance.go`
- Modify: `performance_test.go`

**Interfaces:**
- Adds to `Manager`: `performanceCollector *performanceCollector` and `performanceHistory []PerformanceSample`, guarded by `mu`.
- Produces: `(*Manager).updatePerformance(time.Time)` and `(*Manager).performanceSnapshot()`.
- Produces: `(*Manager).handlePerformance(http.ResponseWriter, *http.Request)` at `GET /api/performance`; other methods return 405 with `Allow: GET`.
- Preserves: existing `HostStats` JSON fields, derived from the latest sample using nearest-integer CPU and byte-to-MB/GB conversions.

- [ ] **Step 1: Write failing Manager/API tests**

```go
func TestManagerUpdatePerformanceAppendsAndUpdatesHeader(t *testing.T) {
	source := &fakePerformanceSource{cpu: 55.6, memoryUsed: 2 << 30, memoryTotal: 8 << 30, vmUsed: 20 << 30, vmTotal: 80 << 30}
	m := &Manager{cfg: Config{VMStoragePath: "/vm"}, performanceCollector: &performanceCollector{source: source}}
	m.updatePerformance(time.Unix(100, 0))
	if len(m.performanceHistory) != 1 { t.Fatalf("history = %d", len(m.performanceHistory)) }
	if m.hostStats.CPUPercent != 56 || m.hostStats.MemUsedMB != 2048 || m.hostStats.DiskTotalGB != 80 { t.Fatalf("header = %+v", m.hostStats) }
}

func TestPerformanceSnapshotCopiesHistory(t *testing.T) {
	m := &Manager{performanceHistory: []PerformanceSample{{UptimeSeconds: 1}}}
	s := m.performanceSnapshot()
	s.History[0].UptimeSeconds = 99
	if m.performanceHistory[0].UptimeSeconds != 1 { t.Fatal("history escaped Manager lock") }
}

func TestPerformanceEndpointReturnsOnlyPerformancePayload(t *testing.T) {
	m := &Manager{performanceHistory: []PerformanceSample{{CPUPercent: 12, CPUAvailable: true}}}
	w := httptest.NewRecorder()
	m.handlePerformance(w, httptest.NewRequest(http.MethodGet, "/api/performance", nil))
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), `"vms"`) || !strings.Contains(w.Body.String(), `"history"`) { t.Fatalf("response = %d %s", w.Code, w.Body.String()) }
}
```

Add a second endpoint test expecting 405 and `Allow: GET` for POST.

- [ ] **Step 2: Run tests to verify RED**

Run: `go test ./... -run 'TestManagerUpdatePerformance|TestPerformanceSnapshot|TestPerformanceEndpoint' -count=1`

Expected: FAIL because Manager integration is absent.

- [ ] **Step 3: Implement Manager integration**

Initialize `performanceCollector` with `systemPerformanceSource` in Manager construction. Replace startup and 60-second `updateHostStats()` calls with `updatePerformance(time.Now())`. Delete `sysctlInt`, `loadAvg5`, `memUsedMB`, `diskUsage`, and `updateHostStats` after the new path supplies the header fields. Register `/api/performance` in `routes()` and return copied history.

- [ ] **Step 4: Verify GREEN and locking**

Run: `gofmt -w main.go performance.go performance_test.go && go test ./... -run 'TestManagerUpdatePerformance|TestPerformanceSnapshot|TestPerformanceEndpoint' -count=1 && go test ./... -count=1 && go test -race ./... -count=1`

Expected: PASS with no races.

- [ ] **Step 5: Commit**

```bash
git add main.go performance.go performance_test.go
git commit -m "feat: expose sampled performance history"
```

### Task 4: Performance tab, cards, and 24-hour charts

**Files:**
- Modify: `index.html`
- Modify: `index_test.go`

**Interfaces:**
- Consumes: `GET /api/performance` and every `PerformanceSample` JSON field.
- Produces: `data-tab="performance"`, `id="tab-performance"`, cards `perfCpu`, `perfMemory`, `perfPressure`, `perfSystemDisk`, `perfVMDisk`, `perfUptime`, and `perfUpdated`.
- Produces: canvases `cpuChart`, `memoryChart`, `diskCapacityChart`, and `diskIOChart`.
- Produces: `loadPerformance()`, `renderPerformance(snapshot)`, `drawLineChart(canvas, series, options)`, `performanceColour(kind, value)`, `formatBytes(value)`, and `formatRate(value)`.

- [ ] **Step 1: Write failing embedded-UI contract tests**

```go
func TestDashboardContainsPerformancePage(t *testing.T) {
	b, err := content.ReadFile("index.html")
	if err != nil { t.Fatal(err) }
	html := string(b)
	for _, want := range []string{`data-tab="performance"`, `id="tab-performance"`, `id="perfCpu"`, `id="perfPressure"`, `id="perfSystemDisk"`, `id="perfVMDisk"`, `id="cpuChart"`, `id="memoryChart"`, `id="diskCapacityChart"`, `id="diskIOChart"`, `/api/performance`, `function drawLineChart`} {
		if !strings.Contains(html, want) { t.Errorf("missing %q", want) }
	}
}

func TestPerformancePageUsesApprovedThresholds(t *testing.T) {
	b, err := content.ReadFile("index.html")
	if err != nil { t.Fatal(err) }
	html := string(b)
	for _, want := range []string{`value > 95`, `value > 80`, `value > 90`, `pressure === "critical"`, `pressure === "warning"`} {
		if !strings.Contains(html, want) { t.Errorf("missing threshold %q", want) }
	}
}

func TestPerformanceHistoryLoadsOnlyWhileVisible(t *testing.T) {
	b, err := content.ReadFile("index.html")
	if err != nil { t.Fatal(err) }
	html := string(b)
	for _, want := range []string{`if (id === "performance") loadPerformance();`, `if (activeTab === "performance") loadPerformance();`} {
		if !strings.Contains(html, want) { t.Errorf("missing guard %q", want) }
	}
}
```

- [ ] **Step 2: Run tests to verify RED**

Run: `go test ./... -run 'TestDashboardContainsPerformancePage|TestPerformancePageUsesApprovedThresholds|TestPerformanceHistoryLoadsOnlyWhileVisible' -count=1`

Expected: FAIL because the Performance page is absent.

- [ ] **Step 3: Implement cards and dependency-free canvas charts**

Add Performance between Dashboard and VM Management. Use a two-column desktop grid and one column below 760px. Unavailable flags display **Unavailable**. Draw device-pixel-ratio-scaled charts with oldest/newest time labels: CPU at 0-100; memory-used percentage with pressure markers; system and VM capacity percentages; read and write rates on an automatic byte-rate scale. Redraw on resize.

Change `CPU (5 min)` to `CPU`. Opening the tab loads history immediately. SSE render calls `loadPerformance()` only when `activeTab === "performance"`; `performanceLoadInFlight` prevents overlapping requests.

- [ ] **Step 4: Verify GREEN**

Run: `go test ./... -run 'TestDashboardContainsPerformancePage|TestPerformancePageUsesApprovedThresholds|TestPerformanceHistoryLoadsOnlyWhileVisible' -count=1 && go test ./... -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add index.html index_test.go
git commit -m "feat: add host performance dashboard"
```

### Task 5: Version 1.30 documentation and release notes

**Files:**
- Modify: `main.go`
- Modify: `README.md`
- Modify: `CHANGELOG.md`
- Create: `version_test.go`

**Interfaces:**
- Produces: `const version = "1.30"`, which also feeds package naming.
- Produces: README guidance for sampling, retention, metrics, status colours, unavailable states, and restart reset.
- Produces: a 1.30 CHANGELOG entry including replacement of load-average CPU display.

- [ ] **Step 1: Write the failing release consistency test**

```go
func TestReleaseVersion130IsConsistent(t *testing.T) {
	readme, err := os.ReadFile("README.md")
	if err != nil { t.Fatal(err) }
	changelog, err := os.ReadFile("CHANGELOG.md")
	if err != nil { t.Fatal(err) }
	if version != "1.30" { t.Fatalf("version = %q", version) }
	if !strings.Contains(string(readme), "Current release: **1.30**") { t.Fatal("README release mismatch") }
	if !strings.Contains(string(changelog), "## 1.30") { t.Fatal("CHANGELOG release missing") }
}
```

- [ ] **Step 2: Run test to verify RED**

Run: `go test ./... -run TestReleaseVersion130IsConsistent -count=1`

Expected: FAIL because the current release is 1.29.

- [ ] **Step 3: Update release metadata and operator documentation**

Set version 1.30. Update the README feature list and UI overview, then add a Performance section covering cards, charts, 60-second samples, 24-hour memory-only history, colours, unavailable metrics, and restart reset. Add a dated 1.30 CHANGELOG entry covering the collector, API, UI, and removal of shell-based host-stat collection.

- [ ] **Step 4: Verify GREEN**

Run: `gofmt -w version_test.go main.go && go test ./... -run TestReleaseVersion130IsConsistent -count=1 && go test ./... -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add main.go README.md CHANGELOG.md version_test.go
git commit -m "docs: release performance monitoring as 1.30"
```

### Task 6: End-to-end verification and package build

**Files:**
- Verify: all tracked source and documentation.
- Generate locally, do not commit: `TartOven-1.30.pkg`.

**Interfaces:**
- Consumes: Tasks 1-5.
- Produces: verified arm64 binary and unsigned macOS installer for manual testing.

- [ ] **Step 1: Run formatting, static analysis, tests, and race tests**

Run: `gofmt -w *.go && go vet ./... && go test ./... -count=1 && go test -race ./... -count=1`

Expected: every command exits 0 and the race detector reports no races.

- [ ] **Step 2: Verify UI and release metadata**

Run: `rg -n 'const version = "1.30"|Current release: \*\*1.30\*\*|## 1.30|/api/performance|data-tab="performance"' main.go README.md CHANGELOG.md index.html`

Expected: all version and Performance contracts are present.

- [ ] **Step 3: Build the production architecture**

Run: `GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 go build -o /tmp/tart-oven-1.30 . && file /tmp/tart-oven-1.30`

Expected: a Mach-O 64-bit arm64 executable.

- [ ] **Step 4: Build and inspect the unsigned installer**

Run: `printf 'n\n' | ./packaging/build-pkg.sh && pkgutil --payload-files TartOven-1.30.pkg | rg 'Tart Oven/tart-oven|com.tartoven.agent.plist'`

Expected: `TartOven-1.30.pkg` exists and contains the binary and LaunchAgent plist.

- [ ] **Step 5: Review diff and repository state**

Run: `git diff --check origin/main...HEAD && git status --short && git log --oneline --decorate origin/main..HEAD`

Expected: no whitespace errors; only the untracked package may remain; commits are scoped to performance monitoring.

- [ ] **Step 6: Commit verification corrections only when needed**

If verification exposed a tracked-file defect, reproduce it with the narrowest command, fix only that defect, rerun Steps 1-5, and commit the exact changed files with `git commit -m "fix: complete 1.30 release verification"`. If no tracked correction was needed, do not create an empty commit.
