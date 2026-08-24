# Automatic SSH Key Provisioning Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Automatically install Tart Oven's SSH public key into running guest VMs so Send command and Get info work without manual per-VM `ssh-copy-id`.

**Architecture:** A new `sshkey.go` generates the host keypair when missing and installs the public key into a guest's `~/.ssh/authorized_keys` over the **existing** password-authenticated SFTP client from `mdm_transfer.go`. `vm.SSHOK` — already maintained by `doRun`'s post-boot key-auth probe — is the gate, so provisioning is idempotent and self-healing with no "done" flag. The 10-second monitor loop scans for candidates and provisions them in bounded goroutines.

**Tech Stack:** Go 1.24 (single `main` package), `golang.org/x/crypto/ssh` v0.41.0 (`MarshalPrivateKey`, `MarshalAuthorizedKey`, `NewPublicKey`), `github.com/pkg/sftp` v1.13.10 via the existing dialer.

**Spec:** `docs/superpowers/specs/2026-08-24-ssh-key-automation-design.md`

## Global Constraints

- Single `main` package at the repo root; new code goes in `sshkey.go` and `sshkey_test.go`.
- Feature is **off by default** (`AutoInstallSSHKey` zero value `false`). Existing installs must upgrade with no behavior change.
- Never overwrite an existing private key.
- The private key never leaves the host; only `<key>.pub` contents are transmitted.
- Never log passwords or key material. Report failures by stage.
- Reuse `remoteProfileFS` / `mdmTransferTarget` / `sshSFTPProfileDialer` from `mdm_transfer.go`. Do **not** write a second SSH client, and do **not** rename the MDM types.
- All guest interaction must go through an injectable interface so tests never open a socket.
- Must keep `go build ./...`, `go vet ./...`, `go test ./...`, and `go test -race ./...` passing.

---

### Task 1: Add the config toggle and per-VM record fields

**Files:**
- Modify: `main.go` (`Config` struct ~line 249, `handleConfig` merge block ~line 3205, `VM` struct ~line 418)
- Test: `config_test.go`

**Interfaces:**
- Produces: `Config.AutoInstallSSHKey bool` (JSON `autoInstallSSHKey`); `VM.SSHKeyInstalledAt time.Time` (JSON `sshKeyInstalledAt`), `VM.SSHKeyError string` (JSON `sshKeyError`). Tasks 3, 5, and 6 read these.

- [ ] **Step 1: Write the failing test**

Add to `config_test.go`:

```go
func TestConfigMergeAcceptsAutoInstallSSHKey(t *testing.T) {
	m := &Manager{cfg: defaultConfig(), vms: map[string]*VM{}, busy: map[string]bool{},
		statePath: filepath.Join(t.TempDir(), "state.json"), reload: make(chan struct{}, 1)}
	if m.cfg.AutoInstallSSHKey {
		t.Fatal("auto key install must default to off")
	}
	request := httptest.NewRequest(http.MethodPost, "/api/config",
		strings.NewReader(`{"autoInstallSSHKey":true}`))
	m.handleConfig(httptest.NewRecorder(), request)
	if !m.cfg.AutoInstallSSHKey {
		t.Fatal("autoInstallSSHKey was not merged")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./... -run TestConfigMergeAcceptsAutoInstallSSHKey -v`
Expected: FAIL — `m.cfg.AutoInstallSSHKey` undefined.

- [ ] **Step 3: Add the fields**

In `Config`, immediately after the `SSHKey` line:

```go
	AutoInstallSSHKey       bool          `json:"autoInstallSSHKey"` // push the SSH public key to guests automatically
```

In `VM`, immediately after `SSHPassword`:

```go
	SSHKeyInstalledAt time.Time `json:"sshKeyInstalledAt,omitempty"` // when the public key was last installed
	SSHKeyError       string    `json:"sshKeyError,omitempty"`       // last provisioning failure, cleared on success
```

In `handleConfig`, next to the `sshKey` merge block:

```go
	if raw, ok := fields["autoInstallSSHKey"]; ok {
		var v bool
		if json.Unmarshal(raw, &v) == nil {
			m.cfg.AutoInstallSSHKey = v
		}
	}
```

Do **not** add an entry to `defaultConfig()` — the zero value `false` is the intended default.

- [ ] **Step 4: Run the tests**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add main.go config_test.go
git commit -m "feat(ssh): add the auto key install toggle and per-VM provisioning record"
```

---

### Task 2: Generate the host keypair when missing

**Files:**
- Create: `sshkey.go`
- Test: `sshkey_test.go`

**Interfaces:**
- Consumes: `expandHome(path string) string` (`main.go:203`).
- Produces: `ensureSSHKeyPair(path string) ([]byte, error)` — returns the **public key bytes** (a single `ssh-ed25519 …` line, newline-trimmed). Creates the pair only when the private key is absent. Task 5 calls it.

- [ ] **Step 1: Write the failing test**

Create `sshkey_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestEnsureSSHKeyPairGeneratesWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tart-oven")
	pub, err := ensureSSHKeyPair(path)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !strings.HasPrefix(string(pub), "ssh-ed25519 ") {
		t.Fatalf("public key = %q", pub)
	}
	if _, _, _, _, err := ssh.ParseAuthorizedKey(pub); err != nil {
		t.Fatalf("public key does not parse: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read private key: %v", err)
	}
	if _, err := ssh.ParsePrivateKey(raw); err != nil {
		t.Fatalf("private key does not parse: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("private key mode = %o, want 600", mode)
	}
}

func TestEnsureSSHKeyPairNeverOverwritesAnExistingKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tart-oven")
	first, err := ensureSSHKeyPair(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ensureSSHKeyPair(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("existing key was regenerated")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./... -run TestEnsureSSHKeyPair -v`
Expected: FAIL — `undefined: ensureSSHKeyPair`.

- [ ] **Step 3: Implement key generation**

Create `sshkey.go`:

```go
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

// ensureSSHKeyPair returns the public key for the identity at path, generating an
// ed25519 pair when the private key does not exist yet. An existing key is never
// overwritten: the public half is re-derived from it if the .pub file is missing.
func ensureSSHKeyPair(path string) ([]byte, error) {
	path = expandHome(strings.TrimSpace(path))
	if path == "" {
		return nil, errors.New("no SSH identity file configured")
	}
	publicPath := path + ".pub"

	if raw, err := os.ReadFile(path); err == nil {
		if pub, err := os.ReadFile(publicPath); err == nil && len(bytes.TrimSpace(pub)) > 0 {
			return bytes.TrimSpace(pub), nil
		}
		signer, err := ssh.ParsePrivateKey(raw)
		if err != nil {
			return nil, fmt.Errorf("parse existing SSH private key: %w", err)
		}
		pub := bytes.TrimSpace(ssh.MarshalAuthorizedKey(signer.PublicKey()))
		if err := os.WriteFile(publicPath, append(pub, '\n'), 0o644); err != nil {
			return nil, fmt.Errorf("write SSH public key: %w", err)
		}
		return pub, nil
	}

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate SSH key: %w", err)
	}
	block, err := ssh.MarshalPrivateKey(privateKey, "tart-oven")
	if err != nil {
		return nil, fmt.Errorf("encode SSH private key: %w", err)
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create SSH key directory: %w", err)
		}
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		return nil, fmt.Errorf("write SSH private key: %w", err)
	}
	sshPublic, err := ssh.NewPublicKey(publicKey)
	if err != nil {
		return nil, fmt.Errorf("derive SSH public key: %w", err)
	}
	pub := bytes.TrimSpace(ssh.MarshalAuthorizedKey(sshPublic))
	if err := os.WriteFile(publicPath, append(pub, '\n'), 0o644); err != nil {
		return nil, fmt.Errorf("write SSH public key: %w", err)
	}
	return pub, nil
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./... -run TestEnsureSSHKeyPair -v && go vet ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add sshkey.go sshkey_test.go
git commit -m "feat(ssh): generate an ed25519 identity when none exists"
```

---

### Task 3: Decide which VMs are provisioning candidates

**Files:**
- Modify: `sshkey.go`
- Test: `sshkey_test.go`

**Interfaces:**
- Consumes: `VM` (with `SSHOK`, `State`, `IP`, `Source`, `Name`), `templateMarker` (`main.go:66`).
- Produces: `eligibleForKeyProvisioning(vm *VM, excluded map[string]bool, busy bool) bool`. Task 5 calls it.

- [ ] **Step 1: Write the failing test**

Add to `sshkey_test.go`:

```go
func TestEligibleForKeyProvisioning(t *testing.T) {
	base := func() *VM {
		return &VM{Name: "runner1", State: "running", IP: "192.168.1.10"}
	}
	for _, tc := range []struct {
		name     string
		mutate   func(*VM)
		excluded map[string]bool
		busy     bool
		want     bool
	}{
		{name: "healthy candidate", mutate: func(*VM) {}, want: true},
		{name: "key already works", mutate: func(v *VM) { v.SSHOK = true }},
		{name: "not running", mutate: func(v *VM) { v.State = "stopped" }},
		{name: "no ip", mutate: func(v *VM) { v.IP = "" }},
		{name: "busy", mutate: func(*VM) {}, busy: true},
		{name: "excluded", mutate: func(*VM) {}, excluded: map[string]bool{"runner1": true}},
		{name: "template", mutate: func(v *VM) { v.Name = "base-TEMPLATE" }},
		{name: "oci image", mutate: func(v *VM) { v.Source = "OCI" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vm := base()
			tc.mutate(vm)
			if got := eligibleForKeyProvisioning(vm, tc.excluded, tc.busy); got != tc.want {
				t.Fatalf("eligible = %v, want %v", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./... -run TestEligibleForKeyProvisioning -v`
Expected: FAIL — `undefined: eligibleForKeyProvisioning`.

- [ ] **Step 3: Implement the predicate**

Append to `sshkey.go`:

```go
// eligibleForKeyProvisioning reports whether a VM should receive the public key.
// SSHOK is the gate: it records whether key authentication already works, so a guest
// that is reachable by key is never touched.
func eligibleForKeyProvisioning(vm *VM, excluded map[string]bool, busy bool) bool {
	if vm == nil || busy || vm.SSHOK {
		return false
	}
	if vm.State != "running" || strings.TrimSpace(vm.IP) == "" {
		return false
	}
	if excluded[vm.Name] {
		return false
	}
	if strings.Contains(vm.Name, templateMarker) {
		return false
	}
	return !strings.EqualFold(strings.TrimSpace(vm.Source), "OCI")
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./... -run TestEligibleForKeyProvisioning -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add sshkey.go sshkey_test.go
git commit -m "feat(ssh): add the key provisioning eligibility rule"
```

---

### Task 4: Install the key into a guest, idempotently

**Files:**
- Modify: `sshkey.go`
- Test: `sshkey_test.go`

**Interfaces:**
- Consumes: `remoteProfileFS` (`mdm_transfer.go:51`) — `WriteFile(path, data, perm) error`, `ReadFile(path) ([]byte, error)`, `Close() error`. `WriteFile` already creates the parent directory and applies the mode, so `.ssh` needs no separate call.
- Produces: `installAuthorizedKey(remote remoteProfileFS, publicKey []byte) (bool, error)` — reports whether it wrote (false = already present).

- [ ] **Step 1: Write the failing test**

Add to `sshkey_test.go`:

```go
type fakeGuestFS struct {
	files  map[string][]byte
	modes  map[string]os.FileMode
	closed bool
}

func newFakeGuestFS() *fakeGuestFS {
	return &fakeGuestFS{files: map[string][]byte{}, modes: map[string]os.FileMode{}}
}

func (f *fakeGuestFS) WriteFile(path string, data []byte, perm os.FileMode) error {
	f.files[path] = append([]byte(nil), data...)
	f.modes[path] = perm
	return nil
}

func (f *fakeGuestFS) ReadFile(path string) ([]byte, error) {
	data, ok := f.files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), data...), nil
}

func (f *fakeGuestFS) Close() error { f.closed = true; return nil }

func TestInstallAuthorizedKeyCreatesTheFileWhenAbsent(t *testing.T) {
	remote := newFakeGuestFS()
	wrote, err := installAuthorizedKey(remote, []byte("ssh-ed25519 AAAAKEY tart-oven"))
	if err != nil || !wrote {
		t.Fatalf("wrote = %v, err = %v", wrote, err)
	}
	if got := string(remote.files[".ssh/authorized_keys"]); got != "ssh-ed25519 AAAAKEY tart-oven\n" {
		t.Fatalf("authorized_keys = %q", got)
	}
	if mode := remote.modes[".ssh/authorized_keys"]; mode != 0o600 {
		t.Fatalf("mode = %o, want 600", mode)
	}
}

func TestInstallAuthorizedKeyPreservesExistingKeys(t *testing.T) {
	remote := newFakeGuestFS()
	remote.files[".ssh/authorized_keys"] = []byte("ssh-rsa SOMEONEELSE laptop")
	if _, err := installAuthorizedKey(remote, []byte("ssh-ed25519 AAAAKEY tart-oven")); err != nil {
		t.Fatal(err)
	}
	got := string(remote.files[".ssh/authorized_keys"])
	want := "ssh-rsa SOMEONEELSE laptop\nssh-ed25519 AAAAKEY tart-oven\n"
	if got != want {
		t.Fatalf("authorized_keys = %q, want %q", got, want)
	}
}

func TestInstallAuthorizedKeyIsANoOpWhenAlreadyPresent(t *testing.T) {
	remote := newFakeGuestFS()
	remote.files[".ssh/authorized_keys"] = []byte("ssh-ed25519 AAAAKEY tart-oven\n")
	wrote, err := installAuthorizedKey(remote, []byte("ssh-ed25519 AAAAKEY tart-oven"))
	if err != nil {
		t.Fatal(err)
	}
	if wrote {
		t.Fatal("rewrote authorized_keys even though the key was present")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./... -run TestInstallAuthorizedKey -v`
Expected: FAIL — `undefined: installAuthorizedKey`.

- [ ] **Step 3: Implement the installer**

Append to `sshkey.go`:

```go
// authorizedKeysPath is home-relative: SFTP sessions start in the user's home
// directory, the same assumption the MDM profile copy already relies on.
const authorizedKeysPath = ".ssh/authorized_keys"

// installAuthorizedKey appends publicKey to the guest's authorized_keys unless it is
// already there, preserving any keys the guest already trusts. It reports whether a
// write happened. WriteFile creates .ssh itself; the default directory mode satisfies
// sshd StrictModes, which only rejects group- or world-writable paths.
func installAuthorizedKey(remote remoteProfileFS, publicKey []byte) (bool, error) {
	key := bytes.TrimSpace(publicKey)
	if len(key) == 0 {
		return false, errors.New("public key is empty")
	}
	existing, err := remote.ReadFile(authorizedKeysPath)
	if err != nil {
		existing = nil // an absent authorized_keys is the normal first-run case
	}
	for _, line := range bytes.Split(existing, []byte("\n")) {
		if bytes.Equal(bytes.TrimSpace(line), key) {
			return false, nil
		}
	}
	merged := bytes.TrimRight(existing, "\n")
	if len(merged) > 0 {
		merged = append(merged, '\n')
	}
	merged = append(merged, key...)
	merged = append(merged, '\n')
	if err := remote.WriteFile(authorizedKeysPath, merged, 0o600); err != nil {
		return false, fmt.Errorf("write authorized_keys: %w", err)
	}
	return true, nil
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./... -run TestInstallAuthorizedKey -v && go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add sshkey.go sshkey_test.go
git commit -m "feat(ssh): install the public key into a guest idempotently"
```

---

### Task 5: Classify failures and orchestrate one VM's provisioning

**Files:**
- Modify: `sshkey.go`
- Test: `sshkey_test.go`

**Interfaces:**
- Consumes: `installAuthorizedKey` (Task 4), `remoteProfileDialer` (`mdm_transfer.go:57`), `mdmTransferTarget`.
- Produces:
  - `sshKeyAuthRejected(err error) bool`
  - `(*Manager).provisionVMKey(ctx context.Context, name string, target mdmTransferTarget, publicKey []byte) error` — one full attempt: dial, install, close.

- [ ] **Step 1: Write the failing test**

Add to `sshkey_test.go`:

```go
type fakeGuestDialer struct {
	fs      *fakeGuestFS
	err     error
	dialled int
}

func (d *fakeGuestDialer) Dial(context.Context, mdmTransferTarget) (remoteProfileFS, error) {
	d.dialled++
	if d.err != nil {
		return nil, d.err
	}
	return d.fs, nil
}

func TestSSHKeyAuthRejectedDistinguishesCredentialFailures(t *testing.T) {
	if !sshKeyAuthRejected(errors.New("authenticate SSH connection: ssh: unable to authenticate")) {
		t.Fatal("wrong password must be classified as an auth rejection")
	}
	if sshKeyAuthRejected(errors.New("open SSH connection: dial tcp 10.0.0.5:22: connect: connection refused")) {
		t.Fatal("connection refused must stay retryable")
	}
}

func TestProvisionVMKeyInstallsAndClosesTheSession(t *testing.T) {
	remote := newFakeGuestFS()
	dialer := &fakeGuestDialer{fs: remote}
	m := &Manager{vms: map[string]*VM{"runner1": {Name: "runner1"}}, busy: map[string]bool{},
		statePath: filepath.Join(t.TempDir(), "state.json"), keyDialer: dialer}

	err := m.provisionVMKey(context.Background(), "runner1",
		mdmTransferTarget{Address: "10.0.0.5:22", Username: "admin", Password: "admin"},
		[]byte("ssh-ed25519 AAAAKEY tart-oven"))
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if _, ok := remote.files[".ssh/authorized_keys"]; !ok {
		t.Fatal("key was not installed")
	}
	if !remote.closed {
		t.Fatal("session was not closed")
	}
}
```

Add `"context"`, `"errors"`, and `"path/filepath"` to the test imports if not already present.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./... -run 'TestSSHKeyAuthRejected|TestProvisionVMKey' -v`
Expected: FAIL — `undefined: sshKeyAuthRejected`, and `Manager` has no field `keyDialer`.

- [ ] **Step 3: Add the injectable dialer field**

In `main.go`, in the `Manager` struct next to `mdmCopier`:

```go
	keyDialer            remoteProfileDialer // password-auth SFTP dialer for SSH key provisioning
```

- [ ] **Step 4: Implement classification and the single attempt**

Append to `sshkey.go` (add `"context"` and `"time"` to its imports):

```go
// sshKeyAuthRejected reports whether err means the stored credentials were refused.
// Those failures are terminal — retrying cannot help and would hammer the guest —
// while transport errors (sshd still booting) are worth retrying.
func sshKeyAuthRejected(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "unable to authenticate") ||
		strings.Contains(text, "permission denied") ||
		strings.Contains(text, "no supported methods remain")
}

// provisionVMKey performs one provisioning attempt against a single guest.
func (m *Manager) provisionVMKey(ctx context.Context, name string, target mdmTransferTarget, publicKey []byte) error {
	m.mu.Lock()
	dialer := m.keyDialer
	m.mu.Unlock()
	if dialer == nil {
		dialer = &sshSFTPProfileDialer{}
	}

	remote, err := dialer.Dial(ctx, target)
	if err != nil {
		return err
	}
	wrote, installErr := installAuthorizedKey(remote, publicKey)
	closeErr := remote.Close()
	if installErr != nil {
		return installErr
	}
	if closeErr != nil {
		return closeErr
	}

	m.mu.Lock()
	if vm := m.vms[name]; vm != nil {
		vm.SSHKeyInstalledAt = time.Now()
		vm.SSHKeyError = ""
	}
	m.save()
	m.mu.Unlock()
	if wrote {
		m.logln("ssh key installed on %s", name)
	}
	return nil
}
```

- [ ] **Step 5: Run the tests**

Run: `go test ./... -run 'TestSSHKeyAuthRejected|TestProvisionVMKey' -v && go test ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add main.go sshkey.go sshkey_test.go
git commit -m "feat(ssh): provision one guest with failure classification"
```

---

### Task 6: Drive provisioning from the monitor loop

**Files:**
- Modify: `sshkey.go`, `main.go` (monitor loop ~line 3404)
- Test: `sshkey_test.go`

**Interfaces:**
- Consumes: `eligibleForKeyProvisioning` (Task 3), `provisionVMKey` (Task 5), `ensureSSHKeyPair` (Task 2), `effectiveSSHCredentials(cfg, vm)` (`main.go:300`).
- Produces: `(*Manager).provisionSSHKeys()` — one scan pass, launching at most one goroutine per VM.

- [ ] **Step 1: Write the failing test**

Add to `sshkey_test.go`:

```go
func TestProvisionSSHKeysDoesNothingWhenDisabled(t *testing.T) {
	dialer := &fakeGuestDialer{fs: newFakeGuestFS()}
	m := &Manager{
		cfg:       Config{AutoInstallSSHKey: false, SSHUser: "admin", SSHPassword: "admin"},
		vms:       map[string]*VM{"runner1": {Name: "runner1", State: "running", IP: "10.0.0.5"}},
		busy:      map[string]bool{},
		keyDialer: dialer,
		statePath: filepath.Join(t.TempDir(), "state.json"),
	}
	m.provisionSSHKeys()
	if dialer.dialled != 0 {
		t.Fatalf("dialled %d times while disabled", dialer.dialled)
	}
}

func TestProvisionSSHKeysSkipsGuestsThatAlreadyHaveKeyAuth(t *testing.T) {
	dialer := &fakeGuestDialer{fs: newFakeGuestFS()}
	m := &Manager{
		cfg:       Config{AutoInstallSSHKey: true, SSHUser: "admin", SSHPassword: "admin", SSHKey: filepath.Join(t.TempDir(), "id")},
		vms:       map[string]*VM{"runner1": {Name: "runner1", State: "running", IP: "10.0.0.5", SSHOK: true}},
		busy:      map[string]bool{},
		keyDialer: dialer,
		statePath: filepath.Join(t.TempDir(), "state.json"),
	}
	m.provisionSSHKeys()
	if dialer.dialled != 0 {
		t.Fatalf("dialled %d times for a guest whose key already works", dialer.dialled)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./... -run TestProvisionSSHKeys -v`
Expected: FAIL — `undefined: m.provisionSSHKeys`.

- [ ] **Step 3: Implement the scan**

Append to `sshkey.go` (add `"sync"` to its imports):

```go
const (
	sshKeyInitialDelay = 30 * time.Second
	sshKeyMaxBackoff   = 2 * time.Minute
	sshKeyDeadline     = 10 * time.Minute
)

// keyProvisionInFlight tracks VMs with a provisioning attempt running. It is kept
// separate from m.busy so key work never blocks Run/Stop.
var keyProvisionInFlight sync.Map

// provisionSSHKeys runs one scan pass: every eligible VM without working key auth is
// handed to a bounded background attempt. Called from the monitor loop.
func (m *Manager) provisionSSHKeys() {
	m.mu.Lock()
	if !m.cfg.AutoInstallSSHKey {
		m.mu.Unlock()
		return
	}
	cfg := m.cfg
	excluded := make(map[string]bool, len(cfg.Excluded))
	for _, name := range cfg.Excluded {
		excluded[strings.TrimSpace(name)] = true
	}
	type candidate struct {
		name     string
		ip       string
		username string
		password string
	}
	var candidates []candidate
	for name, vm := range m.vms {
		if !eligibleForKeyProvisioning(vm, excluded, m.busy[name]) {
			continue
		}
		username, password := effectiveSSHCredentials(cfg, vm)
		if strings.TrimSpace(username) == "" || password == "" {
			continue
		}
		candidates = append(candidates, candidate{name: name, ip: vm.IP, username: username, password: password})
	}
	m.mu.Unlock()

	for _, c := range candidates {
		if _, running := keyProvisionInFlight.LoadOrStore(c.name, true); running {
			continue
		}
		go func(c candidate) {
			defer keyProvisionInFlight.Delete(c.name)
			m.provisionWithRetry(c.name, c.ip, c.username, c.password)
		}(c)
	}
}

// provisionWithRetry waits for sshd, then retries with backoff until the key is
// installed, the credentials are refused, or the deadline passes.
func (m *Manager) provisionWithRetry(name, ip, username, password string) {
	m.mu.Lock()
	keyPath := m.cfg.SSHKey
	timeoutSec := m.cfg.SSHTimeoutSec
	m.mu.Unlock()

	publicKey, err := ensureSSHKeyPair(keyPath)
	if err != nil {
		m.recordKeyError(name, err)
		return
	}
	if timeoutSec < 1 {
		timeoutSec = 30
	}

	ctx, cancel := context.WithTimeout(context.Background(), sshKeyDeadline)
	defer cancel()

	target := mdmTransferTarget{
		Address:  net.JoinHostPort(ip, "22"),
		Username: username,
		Password: password,
		Timeout:  time.Duration(timeoutSec) * time.Second,
	}

	delay := sshKeyInitialDelay
	for {
		select {
		case <-ctx.Done():
			m.recordKeyError(name, errors.New("timed out waiting for the guest to accept the SSH key"))
			return
		case <-time.After(delay):
		}

		err := m.provisionVMKey(ctx, name, target, publicKey)
		if err == nil {
			return
		}
		if sshKeyAuthRejected(err) {
			m.recordKeyError(name, errors.New("guest rejected the configured SSH credentials"))
			return
		}
		if delay *= 2; delay > sshKeyMaxBackoff {
			delay = sshKeyMaxBackoff
		}
	}
}

// recordKeyError stores a provisioning failure for display without logging secrets.
func (m *Manager) recordKeyError(name string, err error) {
	m.mu.Lock()
	if vm := m.vms[name]; vm != nil {
		vm.SSHKeyError = err.Error()
	}
	m.save()
	m.mu.Unlock()
	m.logln("ssh key provisioning failed for %s: %v", name, err)
	m.broadcast()
}
```

Add `"net"` to the `sshkey.go` imports.

- [ ] **Step 4: Call it from the monitor loop**

In `main.go`, in the 10-second monitor goroutine, add the scan after `m.reconcile()`:

```go
		for range t.C {
			m.checkStorage()
			m.healStuck(maxOpAge)
			m.reconcile()
			m.provisionSSHKeys()
			m.broadcast()
		}
```

- [ ] **Step 5: Run the tests**

Run: `go test ./... && go test -race ./... && go vet ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add main.go sshkey.go sshkey_test.go
git commit -m "feat(ssh): provision guest keys automatically from the monitor loop"
```

---

### Task 7: Surface the toggle and provisioning status in the dashboard

**Files:**
- Modify: `index.html` (Configuration SSH panel ~line 590, `fillConfig` ~line 1030, `readConfig` ~line 1064, SSH guide text ~line 530)

**Interfaces:**
- Consumes: `config.autoInstallSSHKey` (Task 1), `vm.sshKeyError` / `vm.sshKeyInstalledAt` (Task 1).

- [ ] **Step 1: Add the toggle to the SSH settings grid**

After the `statusCommand` field:

```html
        <div class="field"><label>&nbsp;</label><label class="radio"><span class="toggle"><input type="checkbox" id="autoInstallSSHKey"><span class="slider"></span></span> Install the SSH key on new VMs automatically</label></div>
```

- [ ] **Step 2: Hydrate and submit it**

In `fillConfig`, next to the other `chk(...)` calls:

```js
  chk("autoInstallSSHKey", c.autoInstallSSHKey);
```

In `readConfig`, inside the returned object next to `sshKey`:

```js
    autoInstallSSHKey: chk("autoInstallSSHKey"),
```

- [ ] **Step 3: Note the automation in the SSH guide**

Replace the guide's "Do it once for the whole fleet" tip paragraph with:

```html
      <p class="muted">Or turn on <b>Install the SSH key on new VMs automatically</b> in Configuration → SSH: Tart Oven generates the key if it is missing and installs it on each running VM about a minute after boot, skipping any VM that already accepts it.</p>
```

- [ ] **Step 4: Show provisioning failures on the VM row**

In `renderTable`, where `lastError` is rendered, include the key error so a wrong password is visible:

```js
      const vmError = vm.lastError || vm.sshKeyError || "";
```

Use `vmError` in place of `vm.lastError` in that row's error cell only.

- [ ] **Step 5: Verify**

Run: `sed -n '/^<script>$/,/^<\/script>$/p' index.html | sed '1d' | node --check && node --test index_ui_test.js && go test ./...`
Expected: PASS. The `index_test.go` DOM-id test now covers `autoInstallSSHKey`.

- [ ] **Step 6: Commit**

```bash
git add index.html
git commit -m "feat(ssh): expose the automatic key install toggle and its status"
```

---

## Self-Review

**Spec coverage:** opt-in toggle off by default (Task 1); key generation, never overwriting (Task 2); eligibility incl. exclude list, TEMPLATE, OCI, `SSHOK` gate (Task 3); idempotent append preserving existing keys, 0600 (Task 4); auth-vs-transient classification and per-attempt session handling (Task 5); 30s initial delay, exponential backoff capped at 2 min, 10-minute deadline, in-flight guard, monitor-loop trigger (Task 6); UI surface (Task 7). All covered.

**Reuse:** no second SSH client. `remoteProfileFS`, `remoteProfileDialer`, `mdmTransferTarget`, and `sshSFTPProfileDialer` are used as-is; the spec's proposed type aliases proved unnecessary once `WriteFile` was confirmed to create parent directories, so they are deliberately omitted rather than added unused.

**Type consistency:** `keyDialer remoteProfileDialer` is declared in Task 5 Step 3 before use in Task 5 Step 4 and Task 6. `installAuthorizedKey` returns `(bool, error)` in Task 4 and is consumed that way in Task 5. `eligibleForKeyProvisioning(vm, excluded, busy)` has the same signature in Tasks 3 and 6. `ensureSSHKeyPair` returns public-key bytes in Task 2, consumed in Task 6.

**Verification note:** `ssh.MarshalPrivateKey`, `ssh.MarshalAuthorizedKey`, and `ssh.NewPublicKey` all exist in the pinned `golang.org/x/crypto v0.41.0`, and `MarshalPrivateKey` accepts an `ed25519.PrivateKey` value — confirmed against the module cache before writing this plan.
