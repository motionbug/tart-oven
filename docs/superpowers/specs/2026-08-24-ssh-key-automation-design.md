# Automatic SSH Key Provisioning — Design (v1.36)

**Status:** Approved 2026-08-24
**Scope:** New opt-in subsystem. No change to existing manual SSH behavior when disabled.

## Goal

Get Tart Oven's SSH public key onto guest VMs automatically, so **Send command** and
**Get info** work on a freshly booted or freshly cloned VM without an operator running
`ssh-copy-id` by hand for each one.

## Problem

`sshExecContext` connects with `BatchMode=yes` — key authentication only, never a
password prompt. That is correct for a headless daemon, but it means Tart Oven cannot
bootstrap its own access: until the public key is in a guest's `~/.ssh/authorized_keys`,
every SSH feature fails for that VM.

Today the Helper Guide hands the operator three manual steps per VM (generate a key,
pipe it over `ssh` with a password, point the config at the key). In an ephemeral farm
where VMs are created and destroyed constantly, that is repeated toil.

## Key insight

**`vm.SSHOK` already answers "does key auth work on this VM?"**

`doRun` sets `SSHOK = false` immediately after boot, then runs the status command over
SSH — a key-auth connection — and stores the outcome (`main.go:1451`, `1462-1466`).

So the provisioner needs no "has been provisioned" flag as its gate. The probe *is* the
gate, which makes the whole subsystem idempotent and self-healing by construction:

- A clone that inherited the key from its TEMPLATE probes green and is never touched.
- A VM whose key was removed in-guest gets it reinstalled on its next boot.
- A VM name that is deleted and recreated carries no stale "done" marker.

A persisted `SSHKeyInstalledAt` is still recorded, but purely as an observability record
for the UI — never as the decision input.

## Design

### Configuration

One new field, **off by default** so existing installs upgrade with no behavior change:

```go
AutoInstallSSHKey bool `json:"autoInstallSSHKey"` // push the SSH public key to guests automatically
```

Enabling it requires a configured SSH identity path plus a username and password; with
any of those missing the provisioner stays idle and reports why in the UI.

### Key generation

If `cfg.SSHKey` points at a path with no file, Tart Oven generates an ed25519 keypair
there: private key in OpenSSH format at `0600`, public key at `<path>.pub` at `0644`.
Generation happens in-process via `crypto/ed25519` and `golang.org/x/crypto/ssh` — no
shelling out to `ssh-keygen`. Existing keys are never overwritten.

### Eligibility

A VM is a provisioning candidate when **all** hold:

- `State == "running"` and `IP != ""`
- not busy (no lifecycle operation in flight)
- `SSHOK == false` (the gate — key auth does not currently work)
- not in `cfg.Excluded`
- name does not contain the `TEMPLATE` marker
- source is not `OCI`

Excluding TEMPLATE VMs is deliberate: a base image should receive the key through the
operator's explicit, reviewed action, and every clone then inherits it.

### Provisioning flow

Per candidate, in its own goroutine, bounded by a **10-minute deadline**:

1. Wait **30 seconds** after the VM first reports an IP — sshd is not up the instant
   Tart resolves an address.
2. **Probe key auth.** If it succeeds, record success and stop. *The guest is never
   touched in this case.*
3. **Install.** Dial with password authentication and:
   - read `.ssh/authorized_keys` (absent is not an error — it is the normal first run)
   - if the public key is already present, skip the write — the operation is idempotent
   - otherwise append the key on its own line, preserving existing content
   - write it back at mode `0600`

   The existing `WriteFile` already creates the parent `.ssh` directory and applies the
   file mode, so no separate mkdir or chmod call is needed. The default directory mode
   satisfies sshd's `StrictModes`, which rejects only group- or world-**writable** paths.
4. **Verify** by re-probing key auth. Only a successful probe marks the VM provisioned.
5. On failure, classify and act:
   - **connection refused / timeout** — the guest is still booting. Back off and retry.
   - **authentication rejected** — the stored password is wrong. Stop immediately and
     surface the error; retrying cannot help and would hammer the guest.
   - **deadline exceeded** — give up, record the last error.

Backoff is exponential from 30s, capped at 2 minutes per attempt, until the deadline.

### Reuse, not reimplementation

`mdm_transfer.go` already contains exactly the client this needs: a password-authenticated
SSH + SFTP session with connection deadlines, context cancellation, path-traversal
guards, and staged error reporting. Its `Dial` returns a handle exposing `mkdirAll`,
`openFile`, `readFile`, and `chmod` — a general remote filesystem.

The provisioner **reuses that dialer and its `remoteProfileFS` handle directly** — the
three-method interface (`WriteFile`, `ReadFile`, `Close`) is sufficient, so no interface
change, no new dialer, and no rename of the MDM types is required. The dialer is injected
through a `keyDialer` field on `Manager` so tests substitute a fake filesystem and never
open a socket.

SFTP sessions start in the user's home directory — the MDM path
`Desktop/mdm_enroll.mobileconfig` is already home-relative — so `.ssh/authorized_keys`
needs no absolute-path or `~` expansion handling.

### State

Two new persisted `VM` fields, both records rather than gates:

```go
SSHKeyInstalledAt time.Time `json:"sshKeyInstalledAt,omitempty"`
SSHKeyError       string    `json:"sshKeyError,omitempty"`
```

`SSHKeyError` is cleared on success and shown in the dashboard so a wrong password is
visible rather than silent.

### Trigger points

The existing 10-second monitor loop scans for candidates and launches provisioning
goroutines. A per-VM in-flight set prevents a second attempt while one is running — the
same pattern as `m.busy`, kept separate so key provisioning never blocks Run/Stop.

This covers both VMs that Tart Oven starts and VMs discovered through `reconcile()` that
were started externally, without adding a hook to `doRun`.

## Security considerations

- The private key never leaves the host; only the `.pub` file is transmitted.
- Passwords are already write-only in API responses and are not logged. Provisioning
  errors are surfaced by **stage**, not with credential values.
- The feature is opt-in, so no existing install starts pushing keys into guests on upgrade.
- `authorized_keys` is appended to, never truncated: a guest's own keys survive.

## Testing

- Key generation: creates a valid ed25519 pair with correct modes; never overwrites.
- Eligibility: covers each exclusion (busy, excluded, TEMPLATE, OCI, `SSHOK` already true,
  no IP).
- Installer: creates `.ssh` when absent; appends without clobbering existing keys; is a
  no-op when the key is already present; sets 0700/0600.
- Failure classification: auth rejection stops; connection refused retries.
- Disabled by default: with the toggle off, no provisioning is attempted.

All guest interaction is behind the injectable dialer, so tests use a fake filesystem and
never open a network connection — the pattern `mdm_transfer_test.go` already establishes.

## Out of scope

Rotating or removing keys from guests. Host-key pinning (guests are ephemeral and
intentionally unpinned). Per-VM opt-in flags — the exclude list plus TEMPLATE marker
cover the fleet model. Changing `sshExecContext` to fall back to password auth: keeping
the daemon key-only is the property that makes provisioning observable.
