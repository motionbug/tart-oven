# Changelog

This file records user-visible changes to Tart Oven. Version 1.29 contains the
Jamf base-image preparation work and VM boot reliability fixes developed on the
Motionbug fork after the v1.27 baseline.

## 1.29 — 2026-08-21

### Jamf base-image preparation

- Added **VM Management → Prepare base VM for Jamf**.
- Added saved settings for the Jamf Pro base URL, invitation code, default SSH
  username, and default SSH password.
- Added a target selector that shows running VMs only. The UI explains the
  intended workflow: prepare one running base image, install and configure its
  profile, stop it, and create later VMs by cloning that base.
- Added password-field saved-state indicators. Invitation codes and passwords
  are cleared from the input fields after saving, while masked dots indicate
  that a value is already stored.
- Added global SSH credentials, defaulting to `admin` / `admin`. Existing
  per-VM SSH credentials take precedence when configured.

### Profile generation and transfer

- Added generation of `mdm_enroll.mobileconfig` using the saved Jamf base URL
  and invitation code. The enrollment endpoint is
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

- Jamf invitation codes and SSH passwords are write-only in client-facing API
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
- Tart Oven does not stop the prepared base VM or automatically create clones
  after the copy. Those remain explicit operator actions.
- The guest must be running, reachable over bridged networking, and have Remote
  Login enabled before profile transfer.
- SSH host keys are not persisted or pinned because the target machines are
  ephemeral and may be recreated with new host keys.

