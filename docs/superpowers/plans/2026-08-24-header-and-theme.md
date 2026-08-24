# Header & Theme Rework Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the light/dark toggle into the header as a button, show CPU/RAM/pressure instead of CPU/RAM/disk, and make the latest `PerformanceSample` the single source of truth for host metrics.

**Architecture:** Delete the `HostStats` struct and expose the newest `PerformanceSample` on `stateSnapshot` as `performance`. The dashboard header renders from that sample, reusing the existing `performanceColour("pressure", …)` for the pressure value. The Performance tab stops refetching its full history on every SSE push and refetches only when the sample timestamp advances.

**Tech Stack:** Go 1.24 (single `main` package, `go test`), vanilla JS embedded in `index.html`, `node:test` for UI tests.

**Spec:** `docs/superpowers/specs/2026-08-24-header-and-theme-design.md`

## Global Constraints

- Go module is a single `main` package at the repo root; all Go files live there.
- `index.html` is one embedded file — markup, CSS, and JS in the same file; JS is a single top-level `<script>` whose last statement is `connect()`.
- Bind DOM events only through the guarded `el(id)` / `q(sel)` accessors, never `document.getElementById(...).onclick` at top level.
- Every change must keep `go build ./...`, `go vet ./...`, `go test ./...`, `go test -race ./...`, and `node --test index_ui_test.js` passing.
- Do not rename existing JSON fields other than `hostStats` → `performance`.
- Header metrics are CPU, RAM, memory pressure, and the version/VM-count meta. VM disk does **not** appear in the header.

---

### Task 1: Expose the latest performance sample on the state snapshot

**Files:**
- Modify: `main.go` (delete `HostStats` struct ~line 458; `Manager.hostStats` field ~line 474; `stateSnapshot.HostStats` line 519; `snapshot()` line 2324)
- Modify: `performance.go:74-84` (remove the `hostStats` assignment, remove the now-unused `math` import)
- Test: `performance_test.go`

**Interfaces:**
- Consumes: `PerformanceSample` (already defined in `performance.go:22`), `m.performanceHistory []PerformanceSample`.
- Produces: `stateSnapshot.Performance PerformanceSample` with JSON tag `performance`. The dashboard reads `state.performance.{cpuPercent,memoryUsedBytes,memoryTotalBytes,memoryPressure,cpuAvailable,memoryAvailable,pressureAvailable,timestamp}`.

- [ ] **Step 1: Write the failing tests**

Add to `performance_test.go`:

```go
func TestSnapshotExposesLatestPerformanceSample(t *testing.T) {
	m := &Manager{
		vms:  map[string]*VM{},
		busy: map[string]bool{},
		performanceHistory: []PerformanceSample{
			{Timestamp: time.Unix(100, 0), CPUPercent: 12},
			{Timestamp: time.Unix(160, 0), CPUPercent: 34, MemoryPressure: "normal", CPUAvailable: true},
		},
	}
	snap := m.snapshot()
	if snap.Performance.CPUPercent != 34 || snap.Performance.MemoryPressure != "normal" {
		t.Fatalf("performance = %+v", snap.Performance)
	}
}

func TestSnapshotHandlesEmptyPerformanceHistory(t *testing.T) {
	m := &Manager{vms: map[string]*VM{}, busy: map[string]bool{}}
	if got := m.snapshot().Performance.Timestamp; !got.IsZero() {
		t.Fatalf("timestamp = %v, want zero", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./... -run TestSnapshot -v`
Expected: FAIL — `snap.Performance` undefined (`stateSnapshot` has no field `Performance`).

- [ ] **Step 3: Replace HostStats with the performance sample**

In `main.go`, delete the entire `HostStats` struct declaration and its doc comment:

```go
// HostStats is a lightweight compatibility snapshot of Mac mini health,
// refreshed from the latest native performance sample about once a minute.
type HostStats struct {
	CPUPercent  int       `json:"cpuPercent"` // current CPU usage, rounded to the nearest integer
	MemUsedMB   int64     `json:"memUsedMB"`
	MemTotalMB  int64     `json:"memTotalMB"`
	DiskUsedGB  int64     `json:"diskUsedGB"`
	DiskTotalGB int64     `json:"diskTotalGB"`
	UpdatedAt   time.Time `json:"updatedAt,omitempty"`
}
```

In the `Manager` struct, delete this field:

```go
	hostStats            HostStats   // refreshed ~once a minute
```

In `stateSnapshot`, replace:

```go
	HostStats      HostStats  `json:"hostStats"`
```

with:

```go
	Performance    PerformanceSample `json:"performance"`
```

In `snapshot()`, add this just above the `return stateSnapshot{` line (it runs under the already-held `m.mu`):

```go
	var latestSample PerformanceSample
	if n := len(m.performanceHistory); n > 0 {
		latestSample = m.performanceHistory[n-1]
	}
```

and replace the struct field `HostStats:      m.hostStats,` with:

```go
		Performance:    latestSample,
```

- [ ] **Step 4: Remove the derived assignment in performance.go**

In `performance.go`, `updatePerformance` becomes:

```go
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
```

`math` was used only by the deleted `int(math.Round(...))`. Remove `"math"` from the import block in `performance.go` or the build fails with "imported and not used".

- [ ] **Step 5: Update the existing header assertion**

In `performance_test.go`, `TestManagerUpdatePerformanceAppendsAndUpdatesHeader` asserts on `m.hostStats`. Replace its assertion block with one against the recorded sample, and rename the test:

```go
func TestManagerUpdatePerformanceAppendsSample(t *testing.T) {
	source := &fakePerformanceSource{cpu: 55.6, memoryUsed: 2 << 30, memoryTotal: 8 << 30, vmUsed: 20 << 30, vmTotal: 80 << 30}
	m := &Manager{cfg: Config{VMStoragePath: "/vm"}, performanceCollector: &performanceCollector{source: source}}
	m.updatePerformance(time.Unix(100, 0))
	if len(m.performanceHistory) != 1 {
		t.Fatalf("history = %d", len(m.performanceHistory))
	}
	sample := m.performanceHistory[0]
	if sample.CPUPercent != 55.6 || sample.MemoryUsedBytes != 2<<30 || sample.VMDiskTotalBytes != 80<<30 {
		t.Fatalf("sample = %+v", sample)
	}
}
```

- [ ] **Step 6: Run the full Go suite**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: PASS. Any remaining `hostStats` reference is a compile error — grep with `grep -rn "hostStats\|HostStats" *.go` and remove stragglers.

- [ ] **Step 7: Commit**

```bash
git add main.go performance.go performance_test.go
git commit -m "refactor(perf): expose the latest performance sample instead of a duplicate HostStats"
```

---

### Task 2: Swap the header markup to pressure and add the theme button

**Files:**
- Modify: `index.html` (CSS near line 35; header markup lines 269-275)
- Test: `index_test.go` (existing DOM-id test covers new ids automatically)

**Interfaces:**
- Produces: DOM ids `statPressure` (replaces `statDisk`) and `themeToggle`, both consumed by Task 3 and Task 4.

- [ ] **Step 1: Add the button style**

In the `<style>` block, immediately after the `.stat-val` rule, add:

```css
  .theme-toggle { background: var(--panel2); border: 1px solid var(--border); border-radius: 8px; color: var(--text); cursor: pointer; font-size: 15px; line-height: 1; padding: 6px 9px; }
  .theme-toggle:hover { border-color: var(--cyan); }
```

- [ ] **Step 2: Replace the hostbar markup**

Replace the `VM disk` stat with `Pressure` and append the button:

```html
  <div class="hostbar">
    <span id="serverLabelDisplay" class="server-label"></span>
    <div class="stat"><span class="stat-label">CPU</span><span id="statCpu" class="stat-val">—</span></div>
    <div class="stat"><span class="stat-label">RAM</span><span id="statRam" class="stat-val">—</span></div>
    <div class="stat"><span class="stat-label">Pressure</span><span id="statPressure" class="stat-val">—</span></div>
    <div class="meta" id="hmeta">connecting…</div>
    <button id="themeToggle" class="theme-toggle" type="button" aria-pressed="false" title="Switch to light mode">🌙</button>
  </div>
```

- [ ] **Step 3: Verify no JS still references the removed id**

Run: `grep -n "statDisk" index.html`
Expected: only the `headerStatColour` branch and `render()` usage remain — both are removed in Task 3. Do not remove them here.

- [ ] **Step 4: Commit**

```bash
git add index.html
git commit -m "feat(ui): show memory pressure and a theme button in the dashboard header"
```

---

### Task 3: Render header metrics from the performance sample

**Files:**
- Modify: `index.html` (`headerStatColour` line ~1404, `setHeaderStat` line ~1414, `render()` lines ~1424-1433)
- Test: `index_ui_test.js`

**Interfaces:**
- Consumes: `stateSnapshot.Performance` from Task 1 (JSON key `performance`), DOM ids from Task 2, and the existing `performanceColour(kind, value)` defined at `index.html:764`.
- Produces: `renderHeaderStats(sample)` — takes the raw sample object, writes `statCpu`, `statRam`, `statPressure`.

- [ ] **Step 1: Write the failing test**

In `index_ui_test.js`, replace the test named `"compact header keeps CPU and disk thresholds metric-specific while preserving RAM"` with:

```js
test("header thresholds stay metric-specific for CPU and RAM", () => {
  const elements = new Map();
  const setHeaderStat = evaluateFunctions(["headerStatColour", "setHeaderStat"], "setHeaderStat", {
    document: {
      getElementById(id) {
        if (!elements.has(id)) elements.set(id, { textContent: "", style: {} });
        return elements.get(id);
      },
    },
  });

  for (const [id, ratio, expected] of [
    ["statCpu", 0.80, "var(--text)"], ["statCpu", 0.81, "var(--amber)"],
    ["statCpu", 0.95, "var(--amber)"], ["statCpu", 0.96, "var(--red)"],
    ["statRam", 0.74, "var(--text)"], ["statRam", 0.75, "var(--amber)"],
    ["statRam", 0.90, "var(--red)"],
  ]) {
    setHeaderStat(id, "value", ratio);
    assert.equal(elements.get(id).style.color, expected, id + " at " + ratio);
  }
});

test("header renders each metric independently when a source is unavailable", () => {
  const elements = new Map();
  const renderHeaderStats = evaluateFunctions(
    ["headerStatColour", "setHeaderStat", "performanceColour", "renderHeaderStats"],
    "renderHeaderStats",
    {
      Math,
      document: {
        getElementById(id) {
          if (!elements.has(id)) elements.set(id, { textContent: "", style: {} });
          return elements.get(id);
        },
      },
    },
  );

  renderHeaderStats({
    cpuAvailable: true, cpuPercent: 34,
    memoryAvailable: false,
    pressureAvailable: true, memoryPressure: "critical",
  });

  assert.equal(elements.get("statCpu").textContent, "34%");
  assert.equal(elements.get("statRam").textContent, "—");
  assert.equal(elements.get("statPressure").textContent, "critical");
  assert.equal(elements.get("statPressure").style.color, "var(--red)");
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `node --test index_ui_test.js`
Expected: FAIL — `missing function renderHeaderStats`.

- [ ] **Step 3: Drop the dead disk branch and guard setHeaderStat**

Replace `headerStatColour` and `setHeaderStat` with:

```js
function headerStatColour(id, ratio) {
  if (id === "statCpu") {
    return ratio > 0.95 ? "var(--red)" : (ratio > 0.80 ? "var(--amber)" : "var(--text)");
  }
  return ratio >= 0.90 ? "var(--red)" : (ratio >= 0.75 ? "var(--amber)" : "var(--text)");
}

function setHeaderStat(id, text, ratio) {
  const element = document.getElementById(id);
  if (!element) return;
  element.textContent = text;
  element.style.color = headerStatColour(id, ratio);
}
```

- [ ] **Step 4: Add renderHeaderStats**

Insert immediately after `setHeaderStat`:

```js
// Renders header host metrics from the newest performance sample. Each metric falls
// back to "—" on its own so one unavailable source never blanks the others.
function renderHeaderStats(sample) {
  const gb = (bytes) => ((bytes || 0) / (1 << 30)).toFixed(1);
  if (sample.cpuAvailable) {
    const percent = Math.round(sample.cpuPercent || 0);
    setHeaderStat("statCpu", percent + "%", percent / 100);
  } else {
    setHeaderStat("statCpu", "—", 0);
  }
  if (sample.memoryAvailable && sample.memoryTotalBytes) {
    setHeaderStat("statRam", gb(sample.memoryUsedBytes) + "/" + gb(sample.memoryTotalBytes) + " GB",
      sample.memoryUsedBytes / sample.memoryTotalBytes);
  } else {
    setHeaderStat("statRam", "—", 0);
  }
  const pressure = document.getElementById("statPressure");
  if (pressure) {
    const available = !!(sample.pressureAvailable && sample.memoryPressure);
    pressure.textContent = available ? sample.memoryPressure : "—";
    pressure.style.color = available ? performanceColour("pressure", sample.memoryPressure) : "var(--text)";
  }
}
```

- [ ] **Step 5: Call it from render()**

Replace the `hmeta` assignment and the whole `const hs = state.hostStats || {};` block with:

```js
  el("hmeta").textContent = "v" + state.version + " · " + state.vms.length + " VMs";

  renderHeaderStats(state.performance || {});
```

- [ ] **Step 6: Run the tests**

Run: `node --test index_ui_test.js && go test ./...`
Expected: PASS. Then `grep -n "hostStats\|statDisk" index.html` must return nothing.

- [ ] **Step 7: Commit**

```bash
git add index.html index_ui_test.js
git commit -m "feat(ui): render header metrics from the single performance sample"
```

---

### Task 4: Move the theme control into the header

**Files:**
- Modify: `index.html` (`setTheme` line ~690, Configuration field line ~601, bindings line ~2071)
- Test: `index_ui_test.js`

**Interfaces:**
- Consumes: `themeToggle` from Task 2.
- Produces: no new functions. `setTheme(light)` keeps its signature and gains button-face updating.

- [ ] **Step 1: Update the existing setTheme test so it tolerates the button lookup**

The current mock has no `getElementById`, so it will throw once `setTheme` looks the button up. In `index_ui_test.js`, in the test `"setTheme immediately redraws a cached performance snapshot"`, change the `document` mock to:

```js
    document: { documentElement: { setAttribute() {} }, getElementById() { return null; } },
```

Then add a test that the button face follows the theme:

```js
test("setTheme updates the header toggle face and pressed state", () => {
  const toggle = { textContent: "", title: "", attrs: {}, setAttribute(k, v) { this.attrs[k] = v; } };
  const setTheme = evaluateFunction("setTheme", {
    document: { documentElement: { setAttribute() {} }, getElementById: () => toggle },
    localStorage: { setItem() {} },
    latestPerformanceSnapshot: null,
  });
  setTheme(true);
  assert.equal(toggle.textContent, "☀️");
  assert.equal(toggle.attrs["aria-pressed"], "true");
  setTheme(false);
  assert.equal(toggle.textContent, "🌙");
  assert.equal(toggle.attrs["aria-pressed"], "false");
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `node --test index_ui_test.js`
Expected: FAIL — `toggle.textContent` is `""`, `setTheme` does not touch the button yet.

- [ ] **Step 3: Update setTheme**

```js
function setTheme(light) {
  document.documentElement.setAttribute("data-theme", light ? "light" : "dark");
  try { localStorage.setItem("theme", light ? "light" : "dark"); } catch (e) {}
  const toggle = document.getElementById("themeToggle");
  if (toggle) {
    toggle.textContent = light ? "☀️" : "🌙";
    toggle.setAttribute("aria-pressed", light ? "true" : "false");
    toggle.title = light ? "Switch to dark mode" : "Switch to light mode";
  }
  if (latestPerformanceSnapshot) renderPerformance(latestPerformanceSnapshot);
}
```

Keep the early `if (getTheme() === "light") document.documentElement.setAttribute("data-theme", "light");` line — it prevents a flash before the body renders.

- [ ] **Step 4: Remove the Configuration field**

Delete this entire line from the Configuration panel:

```html
        <div class="field"><label>&nbsp;</label><label class="radio"><span class="toggle"><input type="checkbox" id="lightMode"><span class="slider"></span></span> Light mode</label></div>
```

- [ ] **Step 5: Rebind to the header button**

Replace:

```js
el("lightMode").checked = getTheme() === "light";
el("lightMode").onchange = (e) => setTheme(e.target.checked);
```

with:

```js
setTheme(getTheme() === "light"); // sync the header button with the stored preference
el("themeToggle").onclick = () =>
  setTheme(document.documentElement.getAttribute("data-theme") !== "light");
```

- [ ] **Step 6: Run the tests**

Run: `node --test index_ui_test.js && go test ./...`
Expected: PASS. `grep -n "lightMode" index.html` must return nothing.

- [ ] **Step 7: Commit**

```bash
git add index.html index_ui_test.js
git commit -m "feat(ui): move the light/dark toggle from Configuration into the header"
```

---

### Task 5: Refetch performance history only when the sample advances

**Files:**
- Modify: `index.html` (state declarations line ~709, `render()` line ~1474)

**Interfaces:**
- Consumes: `state.performance.timestamp` from Task 1.
- Produces: module-level `lastPerformanceSampleAt` string.

- [ ] **Step 1: Add the tracking variable**

Next to the other module-level `let` declarations (near `let latestPerformanceSnapshot = null;`), add:

```js
let lastPerformanceSampleAt = ""; // newest sample already fetched into the charts
```

- [ ] **Step 2: Gate the refetch**

Replace in `render()`:

```js
  if (activeTab === "performance") loadPerformance();
```

with:

```js
  // The server produces one sample a minute; SSE pushes arrive far more often. Only
  // refetch the history when there is genuinely a new sample to draw.
  const sampleAt = (state.performance && state.performance.timestamp) || "";
  if (activeTab === "performance" && sampleAt !== lastPerformanceSampleAt) {
    lastPerformanceSampleAt = sampleAt;
    loadPerformance();
  }
```

Leave the `showTab` call (`if (id === "performance") loadPerformance();`) and the refresh button unchanged — both are explicit user actions that should always load.

- [ ] **Step 3: Verify the syntax and suites**

Run: `sed -n '/^<script>$/,/^<\/script>$/p' index.html | sed '1d' | node --check && node --test index_ui_test.js && go test ./...`
Expected: PASS.

- [ ] **Step 4: Manual check**

Run the server, open the Performance tab, and confirm charts still populate and update about once a minute rather than continuously.

```bash
go run . -version
```

- [ ] **Step 5: Commit**

```bash
git add index.html
git commit -m "perf(ui): refetch performance history only when a new sample arrives"
```

---

## Self-Review

**Spec coverage:** theme button (Tasks 2, 4); header shows CPU/RAM/pressure + VM count (Tasks 2, 3); `HostStats` deleted and `performance` exposed (Task 1); pressure reuses `performanceColour` (Task 3); dead `statDisk` branch removed (Task 3); refetch gating (Task 5); availability fallbacks (Task 3). All covered.

**Known coupling traps handled:** the `math` import must leave `performance.go` with the `HostStats` block (Task 1 Step 4); the existing `setTheme` test mock lacks `getElementById` and is updated before `setTheme` changes (Task 4 Step 1); `index_test.go`'s DOM-id test picks up `statPressure`/`themeToggle` with no edit needed.

**Type consistency:** `stateSnapshot.Performance` (Go) ↔ `state.performance` (JS); `renderHeaderStats` is defined in Task 3 and called only there; `lastPerformanceSampleAt` is declared in Task 5 Step 1 before its use in Step 2.
