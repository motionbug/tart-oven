# Changelog

This file records user-visible changes to Tart Oven. Version 1.39 fixes the guest
agent installer against a real agentless VM. Version 1.38 makes the SSH
fallback optional and adds a guest agent installer. Version 1.37 runs guest commands
through the Tart guest agent instead of SSH, retires the automatic SSH key provisioning
introduced one release earlier, and finishes removing Suspend. Version 1.36 moves the theme
control into the header, simplifies host metrics onto a single source of truth, and
adds opt-in automatic SSH key provisioning for guest VMs. Version 1.35 hardens the
local API against cross-site requests, fixes several VM-lifecycle and Jamf/MDM
correctness bugs, and makes the dashboard more resilient. Version 1.34 streamlines
VM teardown by eliminating slow SSH graceful shutdown in favor of fast, direct
hard stops for ephemeral testing farm workflows. Version 1.33 introduces
robustness hardening, non-STW memory metrics, and process lifecycle safeguards.
Version 1.32 separates OCI images from runnable local VMs and excludes them from
scheduling by default.

## 1.39 — 2026-08-24

### Guest Agent Installer Fixes

Validated end-to-end against a genuinely agentless VM, which surfaced two defects in
the installer shipped in 1.38.

- **The installer no longer reports "Homebrew is not installed" on guests that have
  it**: a non-interactive SSH session gets `PATH=/usr/bin:/bin:/usr/sbin:/sbin`, which
  contains no Homebrew prefix, so the check failed on every real guest. The script now
  repairs `PATH` before looking for `brew`.
- **The installed launchd jobs now match the official images exactly**: the generated
  property lists were missing `PATH`, the agent's `WorkingDirectory`, and the log
  paths. `PATH` in particular is load-bearing — commands run through `tart exec`
  inherit the agent's environment, so without it anything resolved by name could fail.
  Verified byte-for-byte against the plists the Cirrus base images ship.

## 1.38 — 2026-08-24

### SSH Is Now Optional

- **Allow SSH fallback for guest commands**: A new setting under **Configuration → SSH
  & Commands**, on by default. Turn it off and Tart Oven talks to guests **only** through
  the Tart guest agent — if a guest does not answer, you get a clear message naming that
  instead of a silent SSH attempt.
- **The SSH setup guide disappears when it cannot apply**: With the fallback off, the
  **SSH setup guide** panel and the **SSH identity file** field are hidden. Previously
  the guide was always on screen in VM Management even for fleets where every VM uses
  the agent and none of it applied.
- **Deploy MDM Profile is unaffected**: Jamf enrollment copies a file over SFTP, which
  the guest agent cannot do, so that path keeps using SSH regardless of this setting.
  The default SSH username and password stay visible because it still needs them.

### Install the Guest Agent From the Dashboard

- **Install agent**: A running VM whose commands fell back to SSH now offers an **Install
  agent** action. It installs `tart-guest-agent` through Homebrew inside the guest, sets
  up its two launchd jobs, and then **verifies the agent actually answers** before
  reporting success — rather than trusting the install output.
- Progress streams to the **Activity** panel. The action needs the guest's SSH password
  and sudo, because the agent is exactly what is missing and so cannot install itself.
  It is intended for preparing a base image before cloning.

## 1.37 — 2026-08-24

### Guest Commands Run Through the Tart Guest Agent

- **No SSH Required**: **Get info** and **Send command** now run through the Tart guest
  agent over a virtual socket — no SSH, no key, no password, no `authorized_keys`, and no
  guest networking. Exit codes, stdout/stderr, and `sudo` all behave as before. The
  official `ghcr.io/cirruslabs/macos-*-base` images ship the agent preinstalled.
- **SSH Fallback Retained**: Guests without the agent (for example `vanilla-*` images)
  still work over SSH exactly as before. Each VM row shows whether **agent** or **ssh**
  answered, so the active path is visible at a glance.
- **More Reliable Boot IP Resolution**: VM IPs are now resolved with Tart's `agent`
  resolver first — the one Tart documents as working reliably in all cases, and which
  needs no guest network traffic — before the existing host ARP matching.
- **Removed an Unusable Resolver Tier**: The `dhcp` resolver only works for VMs that are
  *not* bridged, and Tart Oven always runs bridged, so that tier could never succeed. It
  has been dropped from the chain.

### Automatic SSH Key Provisioning Removed

The feature added in 1.36 has been withdrawn. It existed to work around Tart Oven's own
key-only SSH setting, which the guest agent makes unnecessary. It also had two defects
worth naming: it re-provisioned every running VM roughly every 40 seconds instead of once,
and it skipped `TEMPLATE` VMs — the exact VMs its own setup guide told you to use.

The **Install the SSH key on new VMs automatically** setting is gone. Tart Oven still
generates an SSH key when one is missing, for the SSH fallback, but never installs it into
a guest by itself.

### Fixes

- **SSH Identity File**: No longer labeled "(optional)" — it is required whenever the SSH
  fallback is used. A blank value is now repaired from the default on load, and a relative
  path such as `tart-oven` is rejected instead of being silently resolved against the
  server's working directory (`/` under the LaunchAgent), where it would have written a
  private key.
- **Editing Notes No Longer Clears a VM's SSH Username**: Saving notes from the dashboard
  row previously wiped any custom per-VM SSH username, because the editor omits that
  field. A blank submission now means "leave unchanged", matching the password field.
- **Correct Guide Pointer**: The red status indicator pointed at Configuration; the SSH
  setup guide is in VM Management.

### Suspend Fully Removed

The Suspend button was removed in 1.34, but the endpoint, the `suspending`/`suspended`
states, and the guards around them remained — so a VM suspended by any means became
unusable: no Resume, Stop disabled, and Edit, Rename, and Delete all refused. The endpoint,
`doSuspend`, both states, and every guard are now gone, and any VM already stuck in that
state can be stopped and reused normally.

## 1.36 — 2026-08-24

### Automatic SSH Key Provisioning (Opt-In)

- **Hands-Off Guest Access**: New **Install the SSH key on new VMs automatically**
  setting under **Configuration → SSH**. When enabled, Tart Oven installs its public
  key into each running VM's `~/.ssh/authorized_keys` about a minute after boot, so
  **Send command** and **Get info** work without running `ssh-copy-id` per VM.
- **Key Generation**: If the configured SSH identity file does not exist, Tart Oven
  generates an ed25519 keypair in place (private key `0600`, public key `0644`). An
  existing key is never overwritten.
- **Never Repeats Itself**: Provisioning is gated on whether key authentication
  already works, so a VM that already accepts the key — including clones that
  inherited it from a TEMPLATE — is never touched. A guest that loses the key gets it
  back on its next boot, with no per-VM bookkeeping to go stale.
- **Boot-Aware Retries**: Waits 30 seconds for the guest to finish booting, then
  retries with backoff up to 10 minutes. A guest that rejects the stored credentials
  stops immediately and reports the failure instead of retrying.
- **Safe by Default**: Ships disabled. Honors the scheduler exclude list, skips
  TEMPLATE VMs and OCI images, appends to `authorized_keys` without disturbing keys
  the guest already trusts, and never transmits the private key.

### Header & Theme

- **Theme Toggle in the Header**: Light/dark mode moved out of Configuration into a
  single button in the top header, where it belongs — it was always a per-browser
  display preference, never a server setting.
- **Memory Pressure at a Glance**: The header now reports CPU, RAM, and macOS memory
  pressure. VM disk capacity remains on the Performance tab, where its chart gives it
  context.
- **Single Source of Truth for Host Metrics**: Removed the duplicated `HostStats`
  structure. The dashboard state now carries the latest performance sample directly,
  so header and charts can no longer disagree. Each metric falls back independently
  when its source is unavailable.
- **Fewer Redundant Fetches**: The Performance tab re-downloads its 24-hour history
  only when a new sample actually exists, instead of on every live update.

> **API note:** `/api/vms` and the live event stream replace the `hostStats` object
> with `performance`, which carries the full sample. The dashboard is the only
> consumer of this field.

## 1.35 — 2026-08-24

### Security Hardening

- **Cross-Origin Request Protection**: All state-changing endpoints now reject
  requests carrying a cross-origin `Origin` header. This blocks a malicious web
  page open in the operator's browser from silently driving the local API
  (running commands on guests, deleting VMs, or rewriting config) via forged
  cross-site POSTs. Same-origin dashboard requests and non-browser clients are
  unaffected.
- **Safe Tart Install/Update Path**: The install/update flow now only replaces a
  derived directory when it is an actual `tart.app` bundle, so a crafted
  `tartAppPath` can no longer make the updater delete an arbitrary directory.

### VM Lifecycle & SSH Fixes

- **Rename Preserves Per-VM State**: Renaming a VM now carries over its
  server-side notes, tags, and (write-only) SSH credentials to the new name
  instead of silently dropping them, so later SSH and MDM operations keep working.
- **Long SSH Commands No Longer Cut Off**: The **Send command** and **Get info**
  panels no longer inherit the SSH *connect* timeout as an overall deadline, so a
  legitimately long guest command (e.g. `softwareupdate`) runs to completion. A
  dead connection is still dropped quickly by SSH keepalives.
- **Robust On-Demand IP Resolution**: **Get info**, **Send command**, and **Deploy
  MDM Profile** now use the same multi-tier IP resolver as VM boot (host ARP-table
  match plus Tart resolvers), so they succeed after a restart or for VMs started
  outside Tart Oven, not just at boot.

### Jamf / MDM Correctness

- **Unknown Profile Selection Errors Safely**: Deploying with a Jamf profile that
  no longer exists (e.g. a stale dropdown after the profile was deleted in another
  tab) now returns a clear error instead of silently enrolling the VM into the
  legacy default server.
- **Legacy Invitation Code Preserved**: Saving the Jamf profiles list no longer
  drops a legacy single-server invitation code when the migrated "default" profile
  is saved without re-entering it.

### Dashboard Reliability

- **Crash-Resistant Control Bindings**: Dashboard event bindings are now guarded,
  so a removed or renamed UI element can no longer throw during startup and blank
  the entire dashboard (loss of live updates, tabs, and buttons).
- **Filters No Longer Commit Unsaved Settings**: Toggling **Show running only** or
  **Pause** now sends only the field it changes, instead of committing every
  half-edited Settings field, and the Pause control no longer risks unpausing the
  scheduler before the first live update arrives.
- **Removed Dead Shutdown Settings**: The **Shutdown command (SSH)** and **Shutdown
  wait timeout** settings, which no longer had any effect after 1.34's fast-stop
  change, were removed so the UI no longer advertises behavior that does not run.
- **SSE Snapshot Data Race Fixed**: The dashboard state snapshot now copies task
  and log data before serializing it, eliminating a data race with in-flight
  create/clone task output.

## 1.34 — 2026-08-24

### Multi-Server Jamf Pro Profiles & Targeted MDM Deployment

- **Multiple Named Jamf Server Profiles**: Manage and persist multiple Jamf Pro
  environments (e.g. Production, Staging, Sandbox) in a dedicated configuration list
  with custom server names, base URLs, and securely masked invitation codes.
- **Dedicated Deploy MDM Profile Panel**: Select any running base VM, choose the target
  Jamf server profile from a dropdown, and optionally override guest SSH credentials
  to push the generated `.mobileconfig` directly to `~/Desktop/mdm_enroll.mobileconfig`.
- **Safe Partial Configuration Updates**: Fixed a bug where updating Jamf or SSH settings
  would reset unsubmitted configuration fields and trigger unintended scheduler ticks.
  Configuration updates now safely decode via `map[string]json.RawMessage` and merge
  only explicitly submitted fields while preserving existing secrets.

### Darwin Bridged VM Boot IP Resolution Fix

- **Robust ARP Resolution with `parseFlexMAC`**: Fixed VM boot IP resolution failures
  on macOS where Darwin ARP output (`/usr/sbin/arp -an`) uses single-digit hex octets
  (e.g., `74:ac:b9:46:6b:4`). Added `parseFlexMAC` normalization and multi-tier IP
  probing (`hostARPNeighbors`, `tart ip --resolver arp`, and direct Tart fallback).

### UI Streamlining & Dashboard Hardening

- **Removed Suspend Action**: Streamlined the Dashboard table row actions to focus
  on essential controls: **Run**, **Stop**, **Get info**, **Screen**, and **Edit**.
- **DOM Null-Safety Hardening**: Added null-safety across `fillConfig`, `readConfig`,
  and secret placeholder setters to ensure the live dashboard and scheduler controls
  hydrate immediately without client-side script interruptions.
- **Fast Stop (`tart stop -t 5`)**: Standard stop action terminates VMs rapidly via
  `tart stop -t 5` with immediate process kill fallback for ephemeral VM test farms.
- **Interactive Changelog Popup Modal**: View changelogs in-place via a responsive
  popup modal without page navigation.
- **Signed Installer Package (`~/Downloads`)**: `./packaging/build-pkg.sh` signs the
  app binary and installer with Developer ID certificates and exports directly to
  `~/Downloads/TartOven-1.34.pkg`.

## 1.33 — 2026-08-24

### Robustness & Process Safety

- **SSH Execution Deadlines**: `sshExec` now enforces an explicit context deadline
  derived from the configured `SSHTimeoutSec` (with a 15-second buffer, defaulting to
  120s). This prevents the scheduler and monitor loops from hanging indefinitely when
  guest VMs stall, crash, or drop network connectivity during boot probes or commands.
- **SSH Argument Delimiter**: `sshExecContext` now injects the standard POSIX `--`
  argument delimiter before positional `user@ip` operands to guard against option injection.
- **Process Handle Reaper Race Guard**: The asynchronous `cmd.Wait()` reaper closure
  pins the specific `*exec.Cmd` instance, ensuring rapid VM restarts do not inadvertently
  delete newly spawned process handles from the manager's live command map.
- **Log Rotation Error Handling**: `rotatingWriter` now safely checks file descriptors
  and captures errors when opening rotated `.1` log files.
- **MDM SFTP Path Containment**: Added path sanitization and `../` traversal checks
  in the remote SFTP file management operations.
- **Panic-Free MDM Profile Escaping**: Replaced panicking XML escaping with proper
  error propagation in MDM profile XML synthesis.

### Performance & Resource Optimization

- **Non-STW Memory Safeguard Metrics**: Replaced `runtime.ReadMemStats` with lock-free
  atomic `runtime/metrics.Read` for `/memory/classes/heap/idle:bytes` and
  `/memory/classes/heap/released:bytes`, eliminating global Stop-The-World (STW) pauses
  during periodic memory safeguard evaluations.
- **Struct Alignment Optimization**: Reordered struct fields in `PerformanceSample`
  by descending byte size to eliminate CPU alignment padding across the 1,440-entry
  telemetry history buffer while preserving exact JSON schema compatibility.

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
