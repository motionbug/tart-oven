# Guest Agent Migration — Design (v1.37)

**Status:** Approved 2026-08-24
**Supersedes:** the automatic SSH key provisioning shipped in 1.36

## Goal

Use the Tart guest agent as the primary channel for running commands in guests and for
resolving guest IPs, keeping SSH as a fallback for images that lack the agent. Retire the
1.36 SSH key provisioning subsystem, which solved a self-inflicted problem.

## Why 1.36 was wrong

`sshExecContext` passes `-o BatchMode=yes` — key auth only, never prompt. That single
option is the entire reason Send command and Get info require a distributed key, which is
why 1.36 added `sshkey.go`, a config toggle, two `VM` fields, and a monitor-loop hook.

Tart already solves this. Verified on this host (Tart 2.35.0) against a running VM:

```
tart exec sequoia-18F083F5 /usr/bin/sw_vers -productVersion   →  15.7.7
tart exec sequoia-18F083F5 /bin/sh -c 'exit 7'                →  host exit status 7
tart exec … 'echo OUT; echo ERR 1>&2'                         →  stdout/stderr separated
tart exec sequoia-18F083F5 /usr/bin/whoami                    →  admin
tart exec … 'echo admin | sudo -S -p "" /usr/bin/id -u'       →  0   (sudo works)
tart ip  sequoia-18F083F5 --resolver agent                    →  192.168.1.27
tart ip  sequoia-18F083F5            (dhcp, our tier 3)       →  no IP address found
```

The agent transport is a UNIX control socket in the VM directory proxied over AF_VSOCK by
the `tart run` process Tart Oven already owns. It needs no guest network, no credentials,
no `authorized_keys`, and no host keys. Exit codes, stdout/stderr separation, the guest
user, and sudo-over-stdin all behave the same as the SSH path.

The official `ghcr.io/cirruslabs/macos-*-base` images — which this fleet is cloned from —
ship the agent preinstalled. `vanilla-*` images do not.

## Defects being retired or fixed

These are the concrete problems this release closes.

1. **The 1.36 provisioner never terminates.** `eligibleForKeyProvisioning` gates on
   `vm.SSHOK`, but `provisionVMKey` never sets it — `SSHOK` is written only by the boot
   probe and by `/api/info`. After a successful install the VM stays eligible, so the
   10-second monitor re-queues it indefinitely: a password SSH+SFTP handshake and a full
   `state.json` rewrite roughly every 40 seconds per VM, forever. Dormant only because the
   feature ships disabled.
2. **The guide and the automation contradict each other.** The guide's headline advice is
   to put the key on the `TEMPLATE` VM so clones inherit it; the automation explicitly
   skips `TEMPLATE` VMs.
3. **`cfg.SSHKey` is labeled "(optional)" but is mandatory**, is the only SSH field never
   repaired on load, and is the only one the API lets you blank. Blank is the current
   stored value on this host, which is why Send command and Get info do not work here.
4. **A relative identity path escapes.** `expandHome` only handles a leading `~/`, so a
   value like `tart-oven` resolves against the server's working directory — `/` under the
   LaunchAgent. Empirically confirmed to write a private key there.
5. **The red SSH bubble points at the wrong tab** ("see SSH setup guide in Configuration";
   the guide is in VM Management).
6. **Saving a note from the dashboard row wipes that VM's per-VM SSH user.** The row modal
   omits `sshUser`, and the handler assigns it unconditionally while guarding the password
   directly below.
7. **Suspend is half-removed.** The button went in 1.34; the endpoint, `doSuspend`, both
   states, `stopAllowedForState`, and the `isActive` clause remain. `falconF9EA0714` is in
   `suspended` on this host with no Resume, Stop disabled, and Edit/Rename/Delete refused.

## Design

### One execution chokepoint

A single `execInGuest(ctx, name, command, sudoPassword) execResult` becomes the only way
to run a command in a guest. It tries the agent first and falls back to SSH:

1. If the agent is known-absent for this VM, go straight to SSH.
2. Otherwise run `tart exec <name> /bin/sh -c <command>`, feeding `sudoPassword` on stdin
   when present and rewriting `sudo` → `sudo -S -p ''` exactly as the SSH path already
   does. Map the result into the existing `execResult` (stdout, stderr, exit code).
3. If the agent is unavailable — the command fails in a way that indicates no agent rather
   than a failing guest command — record that and fall back to `sshExecContext`.

Distinguishing "no agent" from "the command failed" matters: a guest command exiting
non-zero is a *successful* agent call and must not trigger a fallback. Tart reports a
missing agent as a launch failure before the guest command runs, so the discriminator is
whether `tart exec` itself failed to start the command versus the command returning a
status. Callers (`/api/exec`, `/api/info`, and the boot probe) change only to call
`execInGuest` instead of `sshExec`.

`statusCommand` is a shell string, so it is always wrapped in `/bin/sh -c`.

### IP resolution

`resolveVMIPRobust` gains `--resolver agent` as its **first** tier, ahead of the host ARP
match and the existing `tart ip` fallbacks. The `dhcp` tier is removed: Tart documents it
as working only for VMs *not* using bridged networking, and Tart Oven always passes
`--net-bridged`, so it can never succeed. Verified: it returns "no IP address found" for a
running VM on this host.

The ARP tier is **kept**, not deleted. It currently works on this host, and keeping it
preserves support for guests without the agent. Removing the ~160 lines of ARP/RIB parsing
is a follow-up once the agent path has proven itself in production, not part of this
change.

### Agent status, detection only

Each VM gains a derived, non-persisted `AgentOK` flag for the dashboard, set from whether
the last `execInGuest` used the agent. The UI shows present / absent / unknown. Tart Oven
does **not** install the agent: the fleet's images already ship it, and an installer would
be mostly unused code that needs guest sudo and writes launchd plists. The Helper Guide
documents the one-time `brew install openai/tools/tart-guest-agent` install for anyone
preparing a template from a vanilla image.

### Retiring the 1.36 provisioner

`sshkey.go`, `sshkey_test.go`, `Config.AutoInstallSSHKey`, `VM.SSHKeyInstalledAt`,
`VM.SSHKeyError`, the Configuration toggle, and the monitor-loop hook are removed. The
stored `autoInstallSSHKey` key in existing `state.json` files becomes an ignored unknown
field, so downgrade and upgrade are both non-events.

`ensureSSHKeyPair` is **kept** in a reduced form, because the SSH fallback still needs a
usable identity: it generates the keypair when missing and is called once at startup
rather than per VM. Nothing is pushed into guests automatically any more.

### SSH fallback stays honest

SSH remains for guests without the agent and for the Jamf SFTP transfer, which has no
agent equivalent (`tart cp` does not exist; the guest agent's file-transfer PR is
unmerged). Accordingly:

- `cfg.SSHKey` is repaired on load like every other SSH field, rejected when blank by the
  API, relabeled without "(optional)", and **required to be an absolute or `~/`-rooted
  path** — a relative value is rejected rather than silently resolved against the server's
  CWD.
- The SSH setup guide is kept but demoted and reframed: it is the procedure for guests
  that lack the agent, not the default path.
- The red-bubble tooltip points at VM Management, where the guide actually lives.

## Out of scope

Deleting the ARP resolver (follow-up once the agent path is proven). Removing notes, tags,
per-VM SSH credentials, Jamf multi-server profiles, `jamfRecon`, or random scheduler mode
— all flagged as unused, all deferred by explicit decision. Installing the guest agent
into guests. Replacing the Jamf SFTP transfer. API authentication, which remains the
largest open risk and is tracked separately.
