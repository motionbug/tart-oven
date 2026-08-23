# Changelog

This file records user-visible changes to Tart Oven. Version 1.32 separates OCI
images from runnable local VMs and excludes them from scheduling by default.
Version 1.31 adds memory safeguards and recovery controls. Version 1.30 adds
native host performance monitoring. Version 1.29 contains the Jamf base-image
preparation work and VM boot reliability fixes developed on the Motionbug fork
after the v1.27 baseline.

## 1.32 — 2026-08-23

### Local VM and OCI image separation

- Tart Oven now preserves Tart's `Source`, `Disk`, `Size`, and `Accessed`
  metadata from both JSON list output and the older table fallback.
- The Dashboard separates **Local VMs** from **OCI Images**. Local VMs retain
  all existing lifecycle, SSH, notes, and recovery actions.
- OCI rows show the complete registry reference, cached size, virtual-disk
  size, and last-accessed value. Their only action is **Clone**.
- The OCI Clone action opens VM Management in clone mode and selects the exact
  tag or digest reported by Tart.
- Search filters both sections. **Show running only** hides OCI Images and
  shows only active local VMs.
- Edit, delete, Jamf preparation, and other local-only selectors exclude OCI
  entries. The clone-source selector includes both local VMs and OCI images.

### Scheduler behavior and upgrades

- Added `excludeOciFromScheduler`, enabled by default for new installations and
  existing state files that predate the setting.
- With the setting enabled, the scheduler skips entries whose Tart source is
  `OCI`, case-insensitively. Local and unknown legacy sources retain their
  previous behavior, and existing per-name/template exclusions still apply.
- An explicit disabled value is preserved across restarts, allowing an
  operator to opt OCI entries back into scheduling when desired.
- Tart reports a clone as a local VM and does not expose its original OCI
  source in `tart list`; Tart Oven therefore does not claim unavailable clone
  provenance.

### API and testing

- `/api/vms` now exposes `source`, `disk`, `size`, and `accessed` for entries
  discovered through Tart.
- Added tests for JSON and table metadata, reconciliation, upgrade-safe config
  migration, scheduler eligibility, Dashboard grouping, clone-only OCI rows,
  running-filter behavior, and local-only management selectors.

## 1.31 — 2026-08-23

### Memory-pressure safeguards

- New VM starts are deferred while the latest available macOS kernel-pressure
  sample is Critical. The gate covers scheduled and manual starts, leaves
  running VMs untouched, and clears when a later sample reports Warning or
  Normal.
- An unavailable pressure sample keeps the last available state instead of
  clearing a Critical gate after a transient collection failure.
- The Performance pressure card explains when start deferral is active.
- Tart Oven never invokes macOS `purge`; active VM memory is not treated as a
  file-cache cleanup problem.
- After create and clone tasks, Tart Oven measures its own idle Go heap and
  calls `debug.FreeOSMemory()` only when at least 64 MiB is eligible to be
  returned to macOS. Go's normal background scavenging handles smaller values.

### VM recovery controls

- Added per-running-VM **Suspend** and **Graceful shutdown** actions.
- Suspend uses `tart suspend` and reports unsupported or failed snapshots on
  the VM without falling back to Stop.
- Graceful shutdown sends the configured SSH shutdown command, waits for the
  guest to stop, and leaves it running with a visible error if shutdown cannot
  be confirmed. Its timeout covers on-demand IP resolution and SSH execution.
  It never calls `tart stop` or kills the Tart process.
- The existing Stop path is unchanged: it retains its current SSH-first flow,
  Tart fallback, scheduler behavior, and final force-kill fallback.
- Suspended VMs remain protected from configuration, rename, and delete
  operations until they are resumed and stopped normally.
- The stopped-VM editor now suggests lowering VM memory in small tested steps
  when host pressure warrants it. Tart Oven never changes VM memory
  automatically; changes apply on the next boot.

### API and testing

- Added `POST /api/suspend` and `POST /api/graceful-shutdown`.
- Added tests for measured Go-heap release, critical-pressure start deferral,
  suspend/graceful command isolation, route registration, suspended-state
  safety, running-only UI controls, and release consistency.

## 1.30 — 2026-08-22

### Host performance monitoring

- Added the **Performance** tab with current-value cards and retained charts
  for actual host CPU utilization, physical memory use, macOS kernel memory
  pressure, system-disk capacity, Tart VM-storage capacity, aggregate disk
  read/write throughput, uptime, and the latest sample time.
- Samples are taken at startup and then every 60 seconds. The in-memory history
  holds at most 1,440 samples (up to 24 hours) and resets when Tart Oven
  restarts; performance samples are not saved to `state.json`.
- Added `GET /api/performance`, returning the latest sample and a copy of the
  retained performance history for the dashboard.
- Replaced the load-average CPU display with actual current CPU utilization.
- Replaced shell-based host-stat collection with native in-process collection.
  Performance monitoring requires no external runtime service, agent, or
  dashboard asset.
- Individual unavailable metrics are shown as **Unavailable** without hiding
  independently collected metrics. Green, amber, and red status colours are
  visual-only and do not trigger alerts, scheduling, or VM actions.

## 1.29 — 2026-08-21

### Jamf base-image preparation

- Added **VM Management → Prepare base VM for Jamf**.
- Added saved settings for the Jamf Pro base URL, invitation ID, default SSH
  username, and default SSH password.
- Added a target selector that shows running VMs only. The UI explains the
  intended workflow: prepare one running base image, install and configure its
  profile, stop it, and create later VMs by cloning that base.
- Added password-field saved-state indicators. Invitation IDs and passwords
  are cleared from the input fields after saving, while masked dots indicate
  that a value is already stored.
- Added global SSH credentials, defaulting to `admin` / `admin`. Existing
  per-VM SSH credentials take precedence when configured.

### Profile generation and transfer

- Added generation of `mdm_enroll.mobileconfig` using the saved Jamf base URL
  and invitation ID. The enrollment endpoint is
  `<jamf-base-url>/enroll/profile` and each profile receives a cryptographically
  random payload UUID.
- Added structural validation of the generated plist before transfer,
  including required payload keys, enrollment values, UUIDs, and XML escaping.
- Added an in-process SSH/SFTP client using Go modules. Tart Oven does not call
  external `ssh` or `scp` binaries, so behavior does not depend on shell paths
  or locally installed command versions.
- Added password-authenticated transfer to
  `~/Desktop/mdm_enroll.mobileconfig` with file mode `0600`.
- Added post-transfer verification: Tart Oven reads the remote file back and
  checks that it is byte-for-byte identical and contains the generated UUID.
- Added cancellation and a shared transfer deadline across TCP connection,
  SSH authentication, SFTP setup, upload, and verification.
- Added clear failure stages for configuration, VM selection, IP resolution,
  SSH authentication, SFTP upload, and remote verification.
- Added `POST /api/vm/mdm-profile` with body `{ "name": "<running-vm>" }`.
  Successful responses include the VM name, destination path, and payload UUID.
- Prevented duplicate profile-copy requests from the dashboard while a copy is
  already in progress.

### Configuration and secret handling

- Jamf invitation IDs and SSH passwords are write-only in client-facing API
  responses and VM snapshots.
- Blank password or invitation-code submissions preserve the previously saved
  value instead of erasing it.
- Stored-secret flags let the dashboard show that a value exists without
  returning the value itself.
- The state file remains local at `~/.tart-oven/state.json` and is written with
  owner-only permissions. These credentials are intended for ephemeral VM
  workflows and are not placed in logs or error responses.
- Jamf base URLs are trimmed and validated as HTTP or HTTPS URLs with a real
  hostname. The dashboard displays backend validation errors safely as text.

### VM boot reliability

- Fixed bridged VMs being stopped almost immediately with
  `boot failure: no IP after 60s`, even though `tart run` had started correctly.
- Root cause: when Tart was launched below the Go LaunchAgent on the affected
  macOS host, its `arp -an` subprocess returned empty output. Tart treated that
  parser error as final even when `tart ip --wait 60` was requested. Tart Oven
  then interpreted the early command failure as a full boot timeout and killed
  the running VM.
- Tart Oven now owns the boot deadline, reads macOS's native routing information
  base through `golang.org/x/net/route`, and matches the VM MAC address from
  Tart's `config.json` to the host neighbor table.
- IP discovery is retried until an address appears or the configured boot
  deadline expires. Context cancellation is checked before and after every
  probe.
- VM names used for config lookup are restricted to a single safe path
  component, including explicit rejection of `..` traversal.

### Testing and verification

- Added tests for Jamf URL validation, write-only secrets, saved-value behavior,
  UI controls, profile generation and validation, endpoint safety, credential
  precedence, staged errors, cancellation, SSH/SFTP cleanup, upload
  verification, VM-name validation, retry deadlines, and native route parsing.
- Isolated configuration tests so they cannot accidentally wake the real VM
  scheduler or start a host VM.
- Verified with `go test ./...`, `go test -race ./...`, `go vet ./...`, and an
  arm64 Go 1.24.3 build.
- Live-tested the automatic scheduler with bridged networking: Tart Oven
  selected and started a stopped VM, Tart reported it running, and Tart Oven
  resolved its bridged IP through the native neighbor table.

### Operational boundaries

- Tart Oven copies the profile but does not install it inside the guest.
- The intended base remains unenrolled: copy the profile to its Desktop, stop
  it, and clone it so each new VM receives the file without inheriting an
  already-enrolled device identity.
- Tart Oven does not stop the prepared base VM or automatically create clones
  after the copy. Those remain explicit operator actions.
- The guest must be running, reachable over bridged networking, and have Remote
  Login enabled before profile transfer.
- SSH host keys are not persisted or pinned because the target machines are
  ephemeral and may be recreated with new host keys.
