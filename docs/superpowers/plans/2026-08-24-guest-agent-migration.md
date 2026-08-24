# Guest Agent Migration Implementation Plan (v1.37)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Run guest commands and resolve guest IPs through the Tart guest agent, with SSH as fallback, and retire the 1.36 SSH key provisioning subsystem.

**Architecture:** One `execInGuest` chokepoint tries `tart exec` first and falls back to `sshExecContext`. `resolveVMIPRobust` gains `--resolver agent` as its first tier and drops the dead `dhcp` tier. `sshkey.go`'s provisioner is deleted; key *generation* is kept for the SSH fallback. Suspend's half-removed remnants go with it.

**Tech Stack:** Go 1.24 (single `main` package), Tart 2.35.0 CLI (`tart exec` needs Tart ≥ 2.27 and host macOS 14+; `--resolver agent` needs ≥ 2.28), vanilla JS in `index.html`, `node:test`.

**Spec:** `docs/superpowers/specs/2026-08-24-guest-agent-migration-design.md`

## Global Constraints

- Single `main` package at the repo root.
- A guest command exiting non-zero is a **successful** agent call — it must never trigger an SSH fallback. Only a failure to launch the command counts as "no agent".
- Keep the ARP resolver tier. Only the `dhcp` tier is removed.
- Do not install the guest agent into guests; detection only.
- `cfg.SSHKey` must never be blank or relative after this change.
- Keep `go build ./...`, `go vet ./...`, `go test ./...`, `go test -race ./...`, and `node --test index_ui_test.js` passing.
- Removing `autoInstallSSHKey` must not break existing `state.json` files — unknown JSON keys are ignored on load, so no migration code is needed.

---

### Task 1: Add the agent execution path behind one chokepoint

**Files:**
- Create: `guestexec.go`, `guestexec_test.go`
- Modify: `main.go` (`Manager` gains an injectable runner)

**Interfaces:**
- Consumes: `execResult` (`main.go:1768`), `tartCmdCtx` (`main.go:692`), `sshExecContext` (`main.go:1805`).
- Produces:
  - `agentExecUnavailable(err error, stderr string) bool`
  - `(*Manager).execViaAgent(ctx context.Context, name, command, sudoPassword string) (execResult, bool)` — the bool reports whether the agent handled it.
  - `(*Manager).execInGuest(ctx context.Context, name, command, sudoPassword string) execResult`

- [ ] **Step 1: Write the failing test**

Create `guestexec_test.go`:

```go
package main

import (
	"strings"
	"testing"
)

func TestAgentExecUnavailableOnlyForLaunchFailures(t *testing.T) {
	// A guest command exiting non-zero is a SUCCESSFUL agent call.
	if agentExecUnavailable(&exitCodeError{code: 7}, "") {
		t.Fatal("a non-zero guest exit must not be treated as a missing agent")
	}
	for _, stderr := range []string{
		"Error: guest agent is not running",
		"requires Tart Guest Agent running in a guest VM",
		"connection refused",
	} {
		if !agentExecUnavailable(&exitCodeError{code: 1}, stderr) {
			t.Fatalf("stderr %q should indicate a missing agent", stderr)
		}
	}
}

func TestSudoRewriteMatchesTheSSHPath(t *testing.T) {
	got := rewriteSudoForStdin("sudo softwareupdate -l; echo done")
	if !strings.Contains(got, "sudo -S -p ''") {
		t.Fatalf("rewritten = %q", got)
	}
	if strings.Contains(rewriteSudoForStdin("echo pseudonym"), "-S") {
		t.Fatal("must not rewrite the substring 'sudo' inside another word")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./... -run 'TestAgentExec|TestSudoRewrite' -v`
Expected: FAIL — `undefined: agentExecUnavailable`, `undefined: exitCodeError`, `undefined: rewriteSudoForStdin`.

- [ ] **Step 3: Extract the sudo rewrite so both paths share it**

`sshExecContext` currently inlines the rewrite (`main.go:1862-1871`). Move it into `guestexec.go` and call it from both:

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

var sudoPattern = regexp.MustCompile(`(^|[\s;&|(])sudo(\s)`)

// rewriteSudoForStdin makes every sudo in the command read its password from
// stdin instead of a TTY, so a non-interactive session can run it.
func rewriteSudoForStdin(command string) string {
	return sudoPattern.ReplaceAllString(command, `${1}sudo -S -p ''${2}`)
}

// exitCodeError carries a guest command's exit status.
type exitCodeError struct{ code int }

func (e *exitCodeError) Error() string { return fmt.Sprintf("exit status %d", e.code) }
func (e *exitCodeError) ExitCode() int { return e.code }

// agentExecUnavailable reports whether a `tart exec` failure means the guest agent
// is not usable, as opposed to the guest command itself failing. A guest command
// that exits non-zero is a successful agent call and must NOT fall back to SSH.
func agentExecUnavailable(err error, stderr string) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(stderr)
	for _, marker := range []string{
		"guest agent",
		"connection refused",
		"no such file or directory",
		"is only available on macos",
		"failed to connect",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}
```

In `sshExecContext`, replace the inline rewrite with `remoteCmd = rewriteSudoForStdin(command)` and delete the now-unused local `sudoRe`.

- [ ] **Step 4: Run the tests**

Run: `go test ./... -run 'TestAgentExec|TestSudoRewrite' -v && go test ./...`
Expected: PASS.

- [ ] **Step 5: Implement execViaAgent and execInGuest**

Append to `guestexec.go`:

```go
// execViaAgent runs command inside the guest through the Tart guest agent. The
// second return value reports whether the agent handled the call at all; when it
// is false the caller should fall back to SSH.
func (m *Manager) execViaAgent(ctx context.Context, name, command, sudoPassword string) (execResult, bool) {
	m.mu.Lock()
	home := m.cfg.VMStoragePath
	m.mu.Unlock()

	remote := command
	if sudoPassword != "" {
		remote = rewriteSudoForStdin(command)
	}

	cmd := m.tartCmdCtx(ctx, home, "exec", name, "/bin/sh", "-c", remote)
	if sudoPassword != "" {
		cmd.Stdin = strings.NewReader(sudoPassword + "\n")
	}
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	res := execResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if err == nil {
		return res, true
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		// The command ran and returned a status: a successful agent call.
		if !agentExecUnavailable(err, res.Stderr) {
			res.ExitCode = exitErr.ExitCode()
			return res, true
		}
		return execResult{}, false
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		res.Error = ctxErr.Error()
		return res, true
	}
	return execResult{}, false
}

// execInGuest is the single way to run a command inside a guest. It prefers the
// Tart guest agent (no SSH, no key, no guest network) and falls back to SSH for
// images that do not ship the agent.
func (m *Manager) execInGuest(ctx context.Context, name, command, sudoPassword string) execResult {
	if res, handled := m.execViaAgent(ctx, name, command, sudoPassword); handled {
		m.setAgentOK(name, true)
		return res
	}
	m.setAgentOK(name, false)
	return m.sshExecContext(ctx, name, command, sudoPassword)
}

// setAgentOK records whether the guest agent answered, for display only.
func (m *Manager) setAgentOK(name string, ok bool) {
	m.mu.Lock()
	if vm := m.vms[name]; vm != nil {
		vm.AgentOK = ok
	}
	m.mu.Unlock()
}
```

Add `"strings"` to the imports if not already present.

- [ ] **Step 6: Add the AgentOK field**

In `main.go`, in the computed-for-UI block of `VM` (next to `Template`, `Excluded`, `Busy`):

```go
	AgentOK bool `json:"agentOk"` // guest agent answered the last command
```

- [ ] **Step 7: Run everything and commit**

Run: `go build ./... && go vet ./... && go test ./... && go test -race ./...`

```bash
git add guestexec.go guestexec_test.go main.go
git commit -m "feat(agent): run guest commands through the Tart guest agent with SSH fallback"
```

---

### Task 2: Route the three command callers through execInGuest

**Files:**
- Modify: `main.go` — `sshExec` (`:1795`), the boot probe (`:1456`), `/api/exec` (`:2700`), `/api/info` (`:2712`)

**Interfaces:**
- Consumes: `execInGuest` from Task 1.

- [ ] **Step 1: Redefine sshExec as the agent-first entry point**

`sshExec` is called by the boot probe, `/api/exec`, and `/api/info`. Change its body so every caller gets the agent path without touching the call sites:

```go
// sshExec runs command in the guest, preferring the Tart guest agent and falling
// back to SSH. The name is retained because it is the established entry point;
// the transport is chosen per call by execInGuest.
func (m *Manager) sshExec(name, command, sudoPassword string) execResult {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	return m.execInGuest(ctx, name, command, sudoPassword)
}
```

- [ ] **Step 2: Verify the callers are unchanged**

Run: `grep -n "m.sshExec(" main.go`
Expected: three call sites (boot probe, `/api/exec`, `/api/info`), all still compiling unchanged.

- [ ] **Step 3: Run the suites**

Run: `go build ./... && go test ./... && go test -race ./...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add main.go
git commit -m "feat(agent): route Get info, Send command, and the boot probe through the agent"
```

---

### Task 3: Prefer the agent resolver and drop the dead dhcp tier

**Files:**
- Modify: `main.go` — `resolveVMIPRobust` (`~:2560`)
- Test: `boot_test.go`

**Interfaces:**
- Unchanged signature: `resolveVMIPRobust(ctx, home, name string, waitSec int) (string, error)`.

- [ ] **Step 1: Replace the tier order**

```go
// resolveVMIPRobust resolves a guest IP, preferring the Tart guest agent, which
// Tart documents as the only resolver that works reliably in all cases and which
// needs no guest network traffic. The host ARP match is kept for guests without
// the agent. The `dhcp` resolver is deliberately absent: it only works for VMs
// that are NOT bridged, and Tart Oven always runs bridged.
func (m *Manager) resolveVMIPRobust(ctx context.Context, home, name string, waitSec int) (string, error) {
	wait := strconv.Itoa(waitSec)
	if out, err := m.tartCmdCtx(ctx, home, "ip", name, "--wait", wait, "--resolver", "agent").Output(); err == nil {
		if ip := strings.TrimSpace(string(out)); ip != "" {
			return ip, nil
		}
	}
	if ip, err := resolveVMIP(home, name, hostARPNeighbors); err == nil && strings.TrimSpace(ip) != "" {
		return strings.TrimSpace(ip), nil
	}
	if out, err := m.tartCmdCtx(ctx, home, "ip", name, "--wait", wait, "--resolver", "arp").Output(); err == nil {
		if ip := strings.TrimSpace(string(out)); ip != "" {
			return ip, nil
		}
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return "", ctxErr
	}
	return "", errors.New("could not resolve VM IP")
}
```

- [ ] **Step 2: Run the suites and commit**

Run: `go build ./... && go test ./... && go test -race ./...`

```bash
git add main.go
git commit -m "feat(agent): resolve guest IPs via the agent resolver and drop the unusable dhcp tier"
```

---

### Task 4: Retire the 1.36 key provisioner, keep key generation

**Files:**
- Delete: the provisioner half of `sshkey.go` and its tests in `sshkey_test.go`
- Modify: `main.go` (`Config.AutoInstallSSHKey`, `VM.SSHKeyInstalledAt`, `VM.SSHKeyError`, `Manager.keyDialer`, monitor loop), `index.html` (toggle, error surfacing)

**Interfaces:**
- Kept: `ensureSSHKeyPair(path string) ([]byte, error)` — now called once at startup for the SSH fallback.
- Removed: `provisionSSHKeys`, `provisionWithRetry`, `provisionVMKey`, `recordKeyError`, `eligibleForKeyProvisioning`, `installAuthorizedKey`, `sshKeyAuthRejected`, `keyProvisionInFlight`, `authorizedKeysPath`, the backoff constants.

- [ ] **Step 1: Delete the provisioner**

From `sshkey.go` remove everything except `ensureSSHKeyPair` and its imports. From `sshkey_test.go` keep only the two `TestEnsureSSHKeyPair*` tests; delete `fakeGuestFS`, `fakeGuestDialer`, and the install/eligibility/provision tests.

- [ ] **Step 2: Remove the fields and the hook**

In `main.go` delete:

```go
	AutoInstallSSHKey       bool          `json:"autoInstallSSHKey"` // push the SSH public key to guests automatically
```
```go
	SSHKeyInstalledAt time.Time `json:"sshKeyInstalledAt,omitempty"`
	SSHKeyError       string    `json:"sshKeyError,omitempty"`
```
```go
	keyDialer            remoteProfileDialer // password-auth SFTP dialer for SSH key provisioning
```

the `autoInstallSSHKey` merge block in `handleConfig`, and the `m.provisionSSHKeys()` line in the monitor loop.

- [ ] **Step 3: Generate the fallback key once at startup**

In `main()`, after config load and before the monitor goroutine starts:

```go
	// The SSH fallback needs a usable identity; nothing is pushed to guests.
	if _, err := ensureSSHKeyPair(m.cfg.SSHKey); err != nil {
		log.Printf("ssh identity unavailable, SSH fallback disabled: %v", err)
	}
```

- [ ] **Step 4: Remove the UI toggle and error surfacing**

In `index.html` delete the `autoInstallSSHKey` field, its `chk(...)` line in `fillConfig`, its entry in `readConfig`, and revert `checkErrors` to `const e = vm.lastError || "";`.

- [ ] **Step 5: Run everything**

Run: `go build ./... && go vet ./... && go test ./... && go test -race ./... && node --test index_ui_test.js`
Expected: PASS. `grep -rn "autoInstallSSHKey\|SSHKeyInstalledAt\|SSHKeyError\|provisionSSHKeys" *.go index.html` returns nothing.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "refactor(ssh): retire automatic key provisioning in favor of the guest agent"
```

---

### Task 5: Make the SSH fallback's identity path trustworthy

**Files:**
- Modify: `main.go` (`load()` repair, `handleConfig` merge, `expandHome` callers), `index.html` (label, tooltip)
- Test: `config_test.go`

**Interfaces:**
- Produces: `validSSHKeyPath(path string) bool` — rejects blank and relative paths.

- [ ] **Step 1: Write the failing test**

```go
func TestSSHKeyPathRejectsBlankAndRelative(t *testing.T) {
	for _, bad := range []string{"", "   ", "tart-oven", "./keys/id", "keys/id"} {
		if validSSHKeyPath(bad) {
			t.Fatalf("%q must be rejected", bad)
		}
	}
	for _, good := range []string{"~/.ssh/tart-oven", "/Users/rob/.ssh/id_ed25519"} {
		if !validSSHKeyPath(good) {
			t.Fatalf("%q must be accepted", good)
		}
	}
}

func TestConfigMergeRejectsARelativeSSHKey(t *testing.T) {
	m := &Manager{cfg: defaultConfig(), vms: map[string]*VM{}, busy: map[string]bool{},
		statePath: filepath.Join(t.TempDir(), "state.json"), reload: make(chan struct{}, 1)}
	m.handleConfig(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodPost, "/api/config", strings.NewReader(`{"sshKey":"tart-oven"}`)))
	if m.cfg.SSHKey != "~/.ssh/tart-oven" {
		t.Fatalf("sshKey = %q, want the default preserved", m.cfg.SSHKey)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./... -run 'TestSSHKeyPath|TestConfigMergeRejectsARelative' -v`
Expected: FAIL — `undefined: validSSHKeyPath`, and the merge currently accepts any string.

- [ ] **Step 3: Implement**

In `main.go` next to `expandHome`:

```go
// validSSHKeyPath rejects blank and relative identity paths. A relative path would
// resolve against the server's working directory — "/" under the LaunchAgent — so
// the key would be written and read somewhere the operator never intended.
func validSSHKeyPath(path string) bool {
	path = strings.TrimSpace(path)
	return strings.HasPrefix(path, "~/") || strings.HasPrefix(path, "/")
}
```

In `handleConfig`, replace the `sshKey` merge with:

```go
	if raw, ok := fields["sshKey"]; ok {
		var v string
		if json.Unmarshal(raw, &v) == nil && validSSHKeyPath(v) {
			m.cfg.SSHKey = strings.TrimSpace(v)
		}
	}
```

In `load()`, after the `StatusCommand` repair:

```go
	if !validSSHKeyPath(m.cfg.SSHKey) {
		m.cfg.SSHKey = d.SSHKey
	}
```

- [ ] **Step 4: Fix the two UI lies**

In `index.html`, change the label from `SSH identity file (optional)` to `SSH identity file` and add `<small>Used only when a guest has no Tart guest agent</small>`. Fix the red-bubble tooltip so it names the right tab:

```js
: '<span class="ssh-dot red" title="SSH failed — see the SSH setup guide in VM Management"></span>';
```

- [ ] **Step 5: Run everything and commit**

Run: `go test ./... && node --test index_ui_test.js`

```bash
git add main.go config_test.go index.html
git commit -m "fix(ssh): require an absolute identity path and correct the guide pointer"
```

---

### Task 6: Fix the per-VM SSH user wipe

**Files:**
- Modify: `main.go` (`/api/vm/notes`, `~:2934`)
- Test: `config_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestNotesUpdateKeepsPerVMSSHUserWhenOmitted(t *testing.T) {
	m := &Manager{cfg: defaultConfig(), busy: map[string]bool{},
		vms:       map[string]*VM{"vm1": {Name: "vm1", SSHUser: "tester"}},
		statePath: filepath.Join(t.TempDir(), "state.json")}
	req := httptest.NewRequest(http.MethodPost, "/api/vm/notes",
		strings.NewReader(`{"name":"vm1","notes":"a note","tags":[]}`))
	m.routes().ServeHTTP(httptest.NewRecorder(), req)
	if got := m.vms["vm1"].SSHUser; got != "tester" {
		t.Fatalf("sshUser = %q, want it preserved", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./... -run TestNotesUpdateKeepsPerVMSSHUser -v`
Expected: FAIL — `sshUser = ""`.

- [ ] **Step 3: Guard the assignment like the password below it**

```go
		if user := strings.TrimSpace(b.SSHUser); user != "" {
			vm.SSHUser = user
		}
```

- [ ] **Step 4: Run and commit**

```bash
git add main.go config_test.go
git commit -m "fix(vm): stop a notes update from clearing a VM's SSH username"
```

---

### Task 7: Finish removing Suspend and unbrick the stuck VM

**Files:**
- Modify: `main.go` (`doSuspend`, `/api/suspend`, `runTartOperation`, `isActive`, `stopAllowedForState` caller), `memory_safeguards.go`, `index.html`, `README.md`
- Test: `memory_safeguards_test.go`, `index_test.go`

- [ ] **Step 1: Delete the backend remnants**

Remove `doSuspend`, the `/api/suspend` route, `runTartOperation` and the `tartOperation` field if `doSuspend` was its only consumer, `stopAllowedForState` (`memory_safeguards.go`), the `!stopAllowedForState(vm.State)` guard in `doStop`, and the `suspending`/`suspended` cases in `isActive`.

- [ ] **Step 2: Let a stuck VM recover**

With `stopAllowedForState` gone, `doStop` no longer refuses a `suspended` VM, so `falconF9EA0714` can be stopped from the UI. Update the `VM` doc comment to list only `stopped | starting | running | stopping`.

- [ ] **Step 3: Delete the frontend remnants**

Remove the `.s-suspending` / `.s-suspended` CSS and the `stopDisabled = vm.state === "suspended"` branch in the row renderer.

- [ ] **Step 4: Update the tests that pin removed behavior**

Delete the suspend cases in `memory_safeguards_test.go` and the `index_test.go` assertion pinning the exact `stopDisabled` JS string.

- [ ] **Step 5: Update the README**

Remove Suspend from the lifecycle list in §1, the "Suspend vs. Fast Hard Stop" section in §5, and the Suspend troubleshooting entry in §10. Also correct §7's stale `TartOven-1.34.pkg` reference and §4's claim that the theme toggle lives in Configuration.

- [ ] **Step 6: Run everything and commit**

Run: `go build ./... && go vet ./... && go test ./... && go test -race ./... && node --test index_ui_test.js`

```bash
git add -A
git commit -m "refactor(vm): finish removing Suspend so no VM can get stuck in it"
```

---

### Task 8: Surface agent status and release 1.37

**Files:**
- Modify: `index.html` (agent indicator, guide reframing), `main.go` (version), `README.md`, `CHANGELOG.md`, `version_test.go`, `tart-oven`

- [ ] **Step 1: Show agent status per VM**

In the row renderer, render `vm.agentOk` next to the SSH bubble as a small label: `agent` when true, `ssh` when false and SSH succeeded, nothing when unknown.

- [ ] **Step 2: Reframe the SSH setup guide**

Change the guide's intro to state that it is only needed for guests without the Tart guest agent, and note that the official `ghcr.io/cirruslabs/macos-*-base` images ship it preinstalled while `vanilla-*` images do not. Remove the "Or turn on Install the SSH key on new VMs automatically" paragraph added in 1.36.

- [ ] **Step 3: Bump the version**

Set `const version = "1.37"` in `main.go`, `Current release: **1.37**` in `README.md`, and update the three assertions plus both test names in `version_test.go`.

- [ ] **Step 4: Write the changelog**

Add a `## 1.37 — <date>` section covering: guest-agent-first command execution and IP resolution with SSH fallback; removal of automatic SSH key provisioning; the identity-path fix; the notes/SSH-user fix; the completed Suspend removal; and the `dhcp` resolver removal.

- [ ] **Step 5: Rebuild the tracked binary and verify**

```bash
CGO_ENABLED=1 go build -trimpath -buildvcs=false -o tart-oven . && ./tart-oven -version
go build ./... && go vet ./... && go test ./... && go test -race ./... && node --test index_ui_test.js
```

- [ ] **Step 6: Live-verify against a real VM**

With a VM running, confirm Get info returns output with `sshKey` still blank — proving the agent path works without any SSH identity — and that the dashboard shows the agent indicator.

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "feat: release v1.37 with guest-agent-first execution and IP resolution"
```

---

## Self-Review

**Spec coverage:** chokepoint with fallback (Tasks 1–2); agent resolver first, `dhcp` removed, ARP kept (Task 3); provisioner retired, key generation kept (Task 4); identity path blank/relative fixes and UI corrections (Task 5); notes wipe (Task 6); Suspend completion and unbricking (Task 7); agent status, guide reframing, release (Task 8). All spec sections covered.

**The critical invariant** — a non-zero guest exit must not trigger an SSH fallback — is tested first, in Task 1 Step 1, before any routing changes land.

**Type consistency:** `execInGuest`/`execViaAgent`/`agentExecUnavailable`/`rewriteSudoForStdin` are defined in Task 1 and consumed in Tasks 1–2. `VM.AgentOK` is added in Task 1 Step 6 and read in Task 8 Step 1. `validSSHKeyPath` is defined in Task 5 Step 3 before its uses in the same step.

**Deliberately out of scope:** deleting the ARP resolver (kept as the non-agent fallback and as a hedge until the agent path is proven), and every unused-feature removal except Suspend, per explicit decision.
