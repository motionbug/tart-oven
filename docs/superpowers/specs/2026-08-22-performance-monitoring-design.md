# Performance Monitoring Design

## Goal

Add a dedicated Performance page to Tart Oven 1.30 that makes the host Mac's CPU, memory pressure, memory use, disk capacity, and disk throughput easy to understand without adding another service or browser dependency.

## User experience

- Keep the compact host summary in the page header.
- Add a **Performance** tab with current-value cards and 24-hour trend charts.
- Refresh host metrics once every 60 seconds.
- Retain 1,440 samples in memory. History resets when Tart Oven restarts.
- Show actual CPU utilization rather than the existing normalized five-minute load average.
- Show memory used/total and the macOS kernel memory-pressure state.
- Show capacity for both the system volume and the configured VM storage path.
- Show aggregate physical-disk read and write throughput.
- Show host uptime and the time of the most recent sample.
- Render missing metric groups as **Unavailable** without stopping other collection or adding repeated log entries.

## Status colours

Colours are informational only. They do not create alerts or log entries.

- CPU: normal through 80%, amber above 80%, red above 95%.
- Memory pressure: green for normal, amber for warning/urgent, red for critical.
- Disk capacity: normal through 80%, amber above 80%, red above 90%.

## Architecture

Create a focused Go collector around `gopsutil` for CPU, memory, disk usage, disk counters, and uptime. Read `kern.memorystatus_vm_pressure_level` directly through `golang.org/x/sys/unix`, mapping Apple's kernel levels 0/1-2/3+ to normal/warning/critical. This keeps collection inside the Go process and avoids depending on command paths or command-output formats.

The collector produces one immutable `PerformanceSample`. `Manager` stores the latest 1,440 samples in a bounded in-memory slice and derives the existing header `HostStats` snapshot from the newest sample. A dedicated `GET /api/performance` endpoint returns the history; the normal `/api/vms` and SSE payloads continue to carry only the latest header summary.

The embedded `index.html` implements responsive cards and dependency-free `canvas` charts. Opening the Performance tab loads its history. Each normal SSE state update triggers a history refresh only while the tab is visible, so the browser does not poll or transfer chart data while the user is elsewhere.

## Failure handling

Each metric group is collected independently. A failed CPU, memory, pressure, capacity, I/O, or uptime lookup marks only that group unavailable for that sample. Counter resets and the first disk-I/O observation produce zero rates, not negative or extreme throughput.

## Sources

- The collector organization is adapted from [go-resource-monitor](https://github.com/krisfur/go-resource-monitor/blob/main/metrics/collector.go).
- Apple's XNU documentation defines kernel memory-pressure levels and their values in [Memorystatus Notifications](https://github.com/apple-oss-distributions/xnu/blob/main/doc/vm/memorystatus_notify.md).

## Out of scope

- Persistent metrics storage across server restarts.
- Configurable sampling intervals or retention.
- Notifications, email, webhooks, and log-based alerts.
- Per-process metrics or per-VM guest metrics.
- A third-party JavaScript charting library.
