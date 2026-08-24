# Header & Theme Rework — Design (v1.36)

**Status:** Approved 2026-08-24
**Scope:** Dashboard header only. No VM lifecycle or scheduler behavior changes.

## Goal

Move the light/dark toggle out of Configuration and into the header as a button,
simplify what the header reports, and remove the duplicated host-stats schema so
CPU/RAM/pressure have exactly one source of truth on both the server and the client.

## Problem

Three separate issues, one root cause.

1. **The theme control is in the wrong place.** The `lightMode` checkbox sits in the
   Configuration panel (`index.html:601`) as though it were a server setting. It is
   not: `setTheme()`/`getTheme()` persist to `localStorage`, and `readConfig()` never
   sends `lightMode` to the server. It is a per-browser display preference filed under
   server configuration.

2. **`HostStats` is a lossy duplicate of `PerformanceSample`.** `updatePerformance()`
   (`performance.go:66`) collects one `PerformanceSample`, then derives a second struct,
   `HostStats`, from it — rounding to whole percent, MB, and GB. Two schemas describing
   the same measurement, hand-synced in an assignment block. The host is *not* sampled
   twice; the waste is a redundant type that must be kept in step by hand.

3. **The Performance tab refetches its entire history on every SSE push.** `render()`
   calls `loadPerformance()` whenever the Performance tab is active (`index.html:1474`).
   SSE pushes arrive at least every 10 seconds, so the client re-downloads and re-parses
   up to 1,440 samples roughly six times a minute for data that only changes once a
   minute.

## Design

### Server: one source of truth

Delete `HostStats` entirely. `stateSnapshot` carries the latest `PerformanceSample`
instead:

- Remove the `HostStats` struct (`main.go`), the `hostStats` field on `Manager`, and the
  assignment block in `updatePerformance()`.
- Replace `stateSnapshot.HostStats HostStats \`json:"hostStats"\`` with
  `Performance PerformanceSample \`json:"performance"\``.
- `snapshot()` reads the last entry of `m.performanceHistory` under the existing mutex.
  Both already live under `m.mu`, so no new locking is introduced.

An empty history yields a zero-valued sample whose `timestamp` is the zero time; the
client already gates on that (see below).

**API change.** `/api/vms` and the SSE payload replace `hostStats` with `performance`.
The dashboard is the only consumer. This is a documented breaking change for 1.36.

### Header contents

The header reports **CPU, RAM, memory pressure, and VM count**. VM disk moves out.

Rationale: memory pressure is the better early-warning signal — it is the same value
that already gates VM starts under critical pressure — while disk capacity changes
slowly and reads better on the Performance tab where a chart gives it context.

Layout stays a single `.hostbar` row:

```
🖥️ Tart Oven   [server label]   CPU 34%   RAM 9/16 GB   Pressure normal   v1.36 · 7 VMs   [🌙]
```

Availability flags on the sample (`cpuAvailable`, `memoryAvailable`, `pressureAvailable`)
drive per-metric fallback: an unavailable metric renders `—` without hiding the others,
matching the behavior established in 1.30.

### Colour thresholds — reuse, do not duplicate

CPU and RAM keep `setHeaderStat(id, text, ratio)` and its numeric thresholds. Pressure is
a string (`normal` / `warning` / `critical`), so it reuses the existing, already-tested
`performanceColour("pressure", value)` rather than growing a parallel string branch inside
`headerStatColour`.

The `statDisk` branch in `headerStatColour` becomes dead when disk leaves the header and
is removed along with its test rows. (Re-adding disk later means re-adding four lines;
keeping a dead branch alive costs a permanently misleading test.)

### Theme button

A single `<button id="themeToggle">` in `.hostbar`, showing 🌙 in dark mode and ☀️ in
light mode, with `aria-pressed` reflecting state.

`setTheme()` keeps its current responsibilities — set `data-theme`, persist to
`localStorage`, redraw cached performance charts — and additionally updates the button
face. The button update is **inlined and nil-guarded inside `setTheme()`** rather than
factored into a helper, because `index_ui_test.js` extracts `setTheme` in isolation; a
call to an external helper would be a `ReferenceError` in that harness.

The Configuration field is removed outright. Two controls for one preference is exactly
the drift this release is trying to remove.

### Performance history refetch

`render()` refetches only when the sample timestamp advances:

```js
const sampleAt = (state.performance && state.performance.timestamp) || "";
if (activeTab === "performance" && sampleAt !== lastPerformanceSampleAt) {
  lastPerformanceSampleAt = sampleAt;
  loadPerformance();
}
```

Switching to the tab (`showTab`) and the explicit refresh button still force a load;
those are deliberate user actions.

## Testing

- **Go:** `snapshot()` exposes the latest sample as `performance`; an empty history is
  safe. `performance_test.go` moves off `m.hostStats` onto the history's last entry.
- **JS:** `setTheme` still redraws cached snapshots *and* now tolerates a missing button
  (the existing test's `document` mock has no `getElementById` and must gain one);
  `headerStatColour` keeps CPU and RAM thresholds after the disk branch is removed;
  pressure colours come from `performanceColour`.
- **DOM ids:** `index_test.go`'s id-reference test covers the new `themeToggle` and
  `statPressure` ids automatically.

## Out of scope

PWA support, icons, and service workers — cut from 1.36 because plain-HTTP LAN access is
not a secure context and remote access is required. Header layout beyond the metric swap.
Disk is not removed from the Performance tab.
