# MDM Enrollment Column Implementation Plan (v1.40)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the misleading `(no Jamf user)` line with an MDM column showing a green/red enrollment indicator and the Jamf Pro URL when enrolled.

**Architecture:** A fixed `profiles status -type enrollment` probe runs through `execInGuest` at the two places Info is already collected. A pure parser turns its output into three persisted `VM` fields, and the dashboard renders them in the column vacated by Notes.

**Tech Stack:** Go 1.24 (single `main` package), vanilla JS in `index.html`, `node:test`.

**Spec:** `docs/superpowers/specs/2026-08-24-mdm-enrollment-column-design.md`

## Global Constraints

- Single `main` package at the repo root.
- The probe command is **built in, not configurable**, and must not be derived from `statusCommand`.
- A failed or unrecognised probe is **unknown** (grey), never "not enrolled" (red).
- The `statusCommand` migration replaces only a byte-exact match of the old default; a customised command is never touched.
- Notes data and its editor stay; only the table column is reassigned.
- Keep `go build ./...`, `go vet ./...`, `go test ./...`, `go test -race ./...`, and `node --test index_ui_test.js` passing.

---

### Task 1: Parse the enrollment probe output

**Files:**
- Create: `mdmstatus.go`, `mdmstatus_test.go`

**Interfaces:**
- Produces: `type mdmStatus struct { Enrolled bool; Server string; Detail string }` and
  `parseEnrollmentStatus(output string) (mdmStatus, bool)` — the bool reports whether the
  output was recognisable at all. `jamfConsoleURL(raw string) string` trims the check-in suffix.

- [ ] **Step 1: Write the failing test**

Create `mdmstatus_test.go`:

```go
package main

import "testing"

func TestParseEnrollmentStatus(t *testing.T) {
	enrolled := "Enrolled via DEP: No\nMDM enrollment: Yes (User Approved)\nMDM server: https://emeia.jamfce.com/mdm/ServerURL\n"
	got, ok := parseEnrollmentStatus(enrolled)
	if !ok || !got.Enrolled {
		t.Fatalf("enrolled parse = %+v ok=%v", got, ok)
	}
	if got.Server != "https://emeia.jamfce.com/mdm/ServerURL" {
		t.Fatalf("server = %q", got.Server)
	}
	if got.Detail != "Yes (User Approved)" {
		t.Fatalf("detail = %q", got.Detail)
	}

	plain, ok := parseEnrollmentStatus("Enrolled via DEP: No\nMDM enrollment: Yes\n")
	if !ok || !plain.Enrolled || plain.Server != "" {
		t.Fatalf("enrolled-without-server parse = %+v ok=%v", plain, ok)
	}

	no, ok := parseEnrollmentStatus("Enrolled via DEP: No\nMDM enrollment: No\n")
	if !ok || no.Enrolled {
		t.Fatalf("unenrolled parse = %+v ok=%v", no, ok)
	}

	// Unrecognisable output must report not-ok so the UI can show "unknown"
	// rather than claiming the VM is unenrolled.
	for _, bad := range []string{"", "   ", "command not found", "zsh: permission denied"} {
		if _, ok := parseEnrollmentStatus(bad); ok {
			t.Fatalf("%q should not parse as a known status", bad)
		}
	}
}

func TestJamfConsoleURL(t *testing.T) {
	for raw, want := range map[string]string{
		"https://emeia.jamfce.com/mdm/ServerURL":  "https://emeia.jamfce.com",
		"https://emeia.jamfce.com/mdm/ServerURL/": "https://emeia.jamfce.com",
		"https://tenant.jamfcloud.com":            "https://tenant.jamfcloud.com",
		"":                                        "",
	} {
		if got := jamfConsoleURL(raw); got != want {
			t.Fatalf("jamfConsoleURL(%q) = %q, want %q", raw, got, want)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./... -run 'TestParseEnrollmentStatus|TestJamfConsoleURL' -v`
Expected: FAIL — `undefined: parseEnrollmentStatus`, `undefined: jamfConsoleURL`.

- [ ] **Step 3: Implement the parser**

Create `mdmstatus.go`:

```go
package main

import "strings"

// mdmEnrollmentProbe is fixed rather than configurable: its output shape is what the
// parser depends on, and statusCommand is a free-text box the operator can edit.
const mdmEnrollmentProbe = "/usr/bin/profiles status -type enrollment"

type mdmStatus struct {
	Enrolled bool
	Server   string // raw MDM check-in URL as reported by the guest
	Detail   string // e.g. "Yes (User Approved)", kept for the tooltip
}

// parseEnrollmentStatus reads `profiles status -type enrollment` output. The second
// return value reports whether the output was recognisable; unrecognised output must
// be surfaced as unknown, never as "not enrolled".
func parseEnrollmentStatus(output string) (mdmStatus, bool) {
	var status mdmStatus
	recognised := false
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if value, found := strings.CutPrefix(line, "MDM enrollment:"); found {
			status.Detail = strings.TrimSpace(value)
			status.Enrolled = strings.HasPrefix(status.Detail, "Yes")
			recognised = true
			continue
		}
		if value, found := strings.CutPrefix(line, "MDM server:"); found {
			status.Server = strings.TrimSpace(value)
		}
	}
	return status, recognised
}

// jamfConsoleURL turns the MDM check-in endpoint into the console URL an operator
// would actually open. A value that does not carry the suffix is returned unchanged.
func jamfConsoleURL(raw string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	if base, found := strings.CutSuffix(trimmed, "/mdm/ServerURL"); found {
		return base
	}
	return trimmed
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./... -run 'TestParseEnrollmentStatus|TestJamfConsoleURL' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add mdmstatus.go mdmstatus_test.go
git commit -m "feat(mdm): parse guest MDM enrollment status"
```

---

### Task 2: Store the status on the VM and probe it where Info is collected

**Files:**
- Modify: `main.go` (`VM` struct; `doRun` post-boot probe ~line 1470; `/api/info` handler ~line 2668)
- Modify: `mdmstatus.go`
- Test: `mdmstatus_test.go`

**Interfaces:**
- Consumes: `parseEnrollmentStatus`, `execInGuest` (`guestexec.go`).
- Produces: `VM.MDMEnrolled bool`, `VM.MDMServer string`, `VM.MDMCheckedAt time.Time` (JSON `mdmEnrolled`, `mdmServer`, `mdmCheckedAt`), and `(*Manager).refreshMDMStatus(name string)`.

- [ ] **Step 1: Add the fields**

In `main.go`, in `VM` after `InfoAt`:

```go
	MDMEnrolled  bool      `json:"mdmEnrolled,omitempty"`  // guest reports an active MDM enrollment
	MDMServer    string    `json:"mdmServer,omitempty"`    // raw MDM check-in URL from the guest
	MDMCheckedAt time.Time `json:"mdmCheckedAt,omitempty"` // zero means never probed, which renders as unknown
```

- [ ] **Step 2: Write the failing test**

Add to `mdmstatus_test.go`:

```go
func TestRefreshMDMStatusLeavesTheVMUnknownWhenTheProbeFails(t *testing.T) {
	m := newTestManager(t)
	m.cfg.TartAppPath = "/nonexistent/tart"
	m.cfg.SSHFallbackEnabled = false
	m.vms["vm1"] = &VM{Name: "vm1", State: "running", IP: "10.0.0.9"}

	m.refreshMDMStatus("vm1")

	vm := m.vms["vm1"]
	if vm.MDMEnrolled {
		t.Fatal("a failed probe must not report the VM as enrolled")
	}
	if !vm.MDMCheckedAt.IsZero() {
		t.Fatal("a failed probe must leave MDMCheckedAt zero so the UI shows unknown, not red")
	}
}
```

Add `"testing"` and any missing imports.

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./... -run TestRefreshMDMStatus -v`
Expected: FAIL — `undefined: m.refreshMDMStatus`.

- [ ] **Step 4: Implement the refresh**

Append to `mdmstatus.go` (add `"context"`, `"time"` imports):

```go
// refreshMDMStatus probes a running guest for its MDM enrollment and records the
// result. An unrecognised or failed probe deliberately leaves MDMCheckedAt zero, so
// the dashboard shows "unknown" instead of asserting the VM is unenrolled.
func (m *Manager) refreshMDMStatus(name string) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	res := m.execInGuest(ctx, name, mdmEnrollmentProbe, "")
	if res.Error != "" || res.ExitCode != 0 {
		return
	}
	status, recognised := parseEnrollmentStatus(res.Stdout)
	if !recognised {
		return
	}

	m.mu.Lock()
	if vm := m.vms[name]; vm != nil {
		vm.MDMEnrolled = status.Enrolled
		vm.MDMServer = status.Server
		vm.MDMCheckedAt = time.Now()
	}
	m.save()
	m.mu.Unlock()
}
```

- [ ] **Step 5: Call it from both Info sites**

In `doRun`, immediately after the existing `m.logln("info %s: ok=%v", name, ok)`:

```go
	m.refreshMDMStatus(name)
	m.broadcast()
```

In the `/api/info` handler, after `m.mu.Unlock()` and before `m.broadcast()`:

```go
		m.refreshMDMStatus(name)
```

- [ ] **Step 6: Run everything**

Run: `go build ./... && go vet ./... && go test ./... && go test -race ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add main.go mdmstatus.go mdmstatus_test.go
git commit -m "feat(mdm): record guest MDM enrollment alongside Get info"
```

---

### Task 3: Drop the Jamf user lookup from the status command

**Files:**
- Modify: `main.go` (`defaultConfig` ~line 347; `load()` repair block)
- Test: `config_test.go`

**Interfaces:**
- Produces: `legacyJamfUserStatusCommand` constant holding the exact superseded default.

- [ ] **Step 1: Write the failing test**

Add to `config_test.go`:

```go
func TestLoadRetiresTheLegacyJamfUserStatusCommand(t *testing.T) {
	m := newTestManager(t)
	body := `{"config":{"listen":"127.0.0.1:9000","statusCommand":` +
		strconv.Quote(legacyJamfUserStatusCommand) + `},"vms":{},"history":[]}`
	if err := os.WriteFile(m.statePath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	m.load()
	if strings.Contains(m.cfg.StatusCommand, "no Jamf user") {
		t.Fatalf("legacy status command survived load: %q", m.cfg.StatusCommand)
	}
	if m.cfg.StatusCommand != defaultConfig().StatusCommand {
		t.Fatalf("status command = %q, want the new default", m.cfg.StatusCommand)
	}
}

func TestLoadPreservesACustomisedStatusCommand(t *testing.T) {
	m := newTestManager(t)
	custom := `hostname; echo mine`
	body := `{"config":{"listen":"127.0.0.1:9000","statusCommand":` +
		strconv.Quote(custom) + `},"vms":{},"history":[]}`
	if err := os.WriteFile(m.statePath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	m.load()
	if m.cfg.StatusCommand != custom {
		t.Fatalf("customised status command was overwritten: %q", m.cfg.StatusCommand)
	}
}
```

Ensure `strconv`, `os`, and `strings` are imported.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./... -run 'TestLoadRetiresTheLegacy|TestLoadPreservesACustomised' -v`
Expected: FAIL — `undefined: legacyJamfUserStatusCommand`.

- [ ] **Step 3: Change the default and add the constant**

In `main.go`, replace the `StatusCommand:` line in `defaultConfig` with:

```go
		StatusCommand:           `hostname; ioreg -c IOPlatformExpertDevice -d 2 | awk -F \" '/IOPlatformSerialNumber/{print $(NF-1)}'; sw_vers -productVersion`,
```

And near it:

```go
// legacyJamfUserStatusCommand is the status command shipped before the MDM column
// existed. Its trailing lookup printed "(no Jamf user)" whenever the read failed for
// any reason, including on VMs that were enrolled — the enrollment column answers
// that question properly now. Replaced on load only when it matches byte-for-byte.
const legacyJamfUserStatusCommand = `hostname; ioreg -c IOPlatformExpertDevice -d 2 | awk -F \" '/IOPlatformSerialNumber/{print $(NF-1)}'; sw_vers -productVersion; defaults read /Library/Managed\ Preferences/com.jamf.usernamevariable.plist jamfProUsername 2>/dev/null || echo "(no Jamf user)"`
```

- [ ] **Step 4: Migrate on load**

In `load()`, immediately after the existing `StatusCommand == ""` repair:

```go
	if m.cfg.StatusCommand == legacyJamfUserStatusCommand {
		m.cfg.StatusCommand = d.StatusCommand
	}
```

- [ ] **Step 5: Run the tests**

Run: `go test ./... && go test -race ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add main.go config_test.go
git commit -m "feat(mdm): retire the Jamf username lookup from the status command"
```

---

### Task 4: Render the MDM column in place of Notes

**Files:**
- Modify: `index.html` (table header line 312; `renderTable` ~lines 1631 and 1639)
- Test: `index_ui_test.js`

**Interfaces:**
- Consumes: `vm.mdmEnrolled`, `vm.mdmServer`, `vm.mdmCheckedAt` from Task 2; `jamfConsoleURL` equivalent in JS.
- Produces: `mdmCell(vm)`.

- [ ] **Step 1: Write the failing test**

Add to `index_ui_test.js`:

```js
test("mdmCell distinguishes enrolled, unenrolled and never-probed VMs", () => {
  const mdmCell = evaluateFunctions(["consoleURL", "mdmCell"], "mdmCell", {
    esc: (s) => String(s),
  });

  const enrolled = mdmCell({
    mdmCheckedAt: "2026-08-24T12:00:00Z", mdmEnrolled: true,
    mdmServer: "https://emeia.jamfce.com/mdm/ServerURL",
  });
  assert.match(enrolled, /ssh-dot green/);
  assert.match(enrolled, /https:\/\/emeia\.jamfce\.com</,
    "should show the console URL, not the check-in endpoint");
  assert.ok(!/mdm\/ServerURL</.test(enrolled), "raw endpoint must not be the visible text");

  const not = mdmCell({ mdmCheckedAt: "2026-08-24T12:00:00Z", mdmEnrolled: false });
  assert.match(not, /ssh-dot red/);

  // Never probed must be grey, not red: a fresh VM has not been asked yet.
  for (const vm of [{}, { mdmCheckedAt: "0001-01-01T00:00:00Z" }]) {
    const unknown = mdmCell(vm);
    assert.match(unknown, /ssh-dot grey/);
    assert.ok(!/ssh-dot red/.test(unknown), "unknown must not render as not-enrolled");
  }
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `node --test index_ui_test.js`
Expected: FAIL — `missing function mdmCell`.

- [ ] **Step 3: Implement the cell**

In `index.html`, next to `infoCell`:

```js
// Trims the MDM check-in endpoint down to the console URL an operator would open.
function consoleURL(raw) {
  const trimmed = String(raw || "").trim().replace(/\/+$/, "");
  return trimmed.endsWith("/mdm/ServerURL") ? trimmed.slice(0, -"/mdm/ServerURL".length) : trimmed;
}

// Enrollment state for a guest. Never probed renders grey rather than red, so a VM
// that has not been asked yet is not reported as unenrolled.
function mdmCell(vm) {
  const checked = vm.mdmCheckedAt && !vm.mdmCheckedAt.startsWith("0001");
  if (!checked) {
    return '<span class="ssh-dot grey" title="MDM enrollment not checked yet"></span>';
  }
  if (!vm.mdmEnrolled) {
    return '<span class="ssh-dot red" title="no MDM enrollment reported"></span>' +
      '<span class="mdm-label">Not enrolled</span>';
  }
  const url = consoleURL(vm.mdmServer);
  const server = url ? '<div class="mdm-url" title="' + esc(vm.mdmServer || "") + '">' + esc(url) + '</div>' : "";
  return '<span class="ssh-dot green" title="enrolled in MDM"></span>' +
    '<span class="mdm-label">Enrolled</span>' + server;
}
```

- [ ] **Step 4: Swap the column**

Change the header at line 312 from `<th>Notes</th>` to `<th>MDM</th>`.

Replace the Notes cell in `renderTable`:

```js
      '<td class="mdm">' + mdmCell(vm) + '</td>' +
```

and delete the now-unused `notesPreview` line above it. The Notes field and its editor
stay; only the column is reassigned.

- [ ] **Step 5: Add the styles**

Next to the `.ssh-dot` rules:

```css
  .mdm-label { font-size: 11px; color: var(--muted); margin-left: 4px; vertical-align: middle; }
  .mdm-url { font-size: 10px; color: var(--muted); font-family: ui-monospace, Menlo, monospace; overflow: hidden; text-overflow: ellipsis; max-width: 200px; }
```

- [ ] **Step 6: Run everything**

Run: `node --test index_ui_test.js && go test ./...`
Expected: PASS. Then check the script still parses:
`awk '/<script>/{f=1;next} /<\/script>/{f=0} f' index.html | node --check -`

- [ ] **Step 7: Commit**

```bash
git add index.html index_ui_test.js
git commit -m "feat(mdm): show enrollment and the Jamf Pro URL in the dashboard"
```

---

### Task 5: Documentation and release

**Files:**
- Modify: `main.go` (version), `README.md`, `CHANGELOG.md`, `version_test.go`, `tart-oven`

- [ ] **Step 1: Bump the version**

`const version = "1.40"` in `main.go`, `Current release: **1.40**` and the install
references in `README.md`, and the three assertions plus both test names in
`version_test.go`.

- [ ] **Step 2: Document the column**

In `README.md`, add MDM to the Dashboard column description and explain the three states,
noting that grey means not yet probed. Remove any reference to a Notes column.

- [ ] **Step 3: Write the changelog**

A `## 1.40` section covering: the new MDM column and what its three states mean; the Jamf
Pro URL shown in place of the raw check-in endpoint; removal of the `(no Jamf user)` line
and the automatic migration of the old default; and the note that Notes remains editable
but no longer has a column.

- [ ] **Step 4: Rebuild the embedded artifacts**

README and CHANGELOG are `go:embed`-ed and rendered by the in-app Helper Guide, so the
binary must be rebuilt **after** the docs are final:

```bash
CGO_ENABLED=1 go build -trimpath -buildvcs=false -o tart-oven . && ./tart-oven -version
```

- [ ] **Step 5: Verify everything**

```bash
go build ./... && go vet ./... && go test ./... && go test -race ./... && node --test index_ui_test.js
```

- [ ] **Step 6: Live-check against a real VM**

With a VM running, click **Get info** and confirm the MDM column shows a green dot with
the console URL, and that the Info box no longer ends in `(no Jamf user)`.

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "feat: release v1.40 with the MDM enrollment column"
```

---

## Self-Review

**Spec coverage:** fixed probe and parser (Task 1); persisted fields and both collection sites (Task 2); status-command change plus exact-match migration (Task 3); column replacing Notes, three states, URL trimming (Task 4); docs and release (Task 5). All spec sections covered.

**The key invariant** — an unrecognised or failed probe must render grey, not red — is asserted on both sides: in Go (`TestRefreshMDMStatusLeavesTheVMUnknownWhenTheProbeFails`) and in JS (`mdmCell` unknown cases), because getting it wrong would label healthy VMs as unmanaged.

**Type consistency:** `mdmStatus`/`parseEnrollmentStatus`/`jamfConsoleURL` are defined in Task 1 and consumed in Task 2. `VM.MDM*` fields are added in Task 2 Step 1 before use in Step 4 and in Task 4. `consoleURL` (JS) mirrors `jamfConsoleURL` (Go) and is defined before `mdmCell` uses it.

**Deliberately out of scope:** making the URL a clickable link, DEP status, and any change to the MDM profile deployment path.
