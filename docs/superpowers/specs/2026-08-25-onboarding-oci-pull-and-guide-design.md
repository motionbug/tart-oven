# Tart Oven Onboarding Wizard, Direct OCI Pull & Helper Guide Overhaul Specification

**Date:** 2026-08-25  
**Version Target:** Tart Oven 1.50  
**Status:** Approved Specification  

---

## 1. Executive Summary & Goals

Tart Oven 1.50 transforms the zero-to-running-VM experience from a manual CLI-dependent workflow into an integrated, guided Mac administrative platform. This specification details three core pillars:
1. **Direct OCI Image Pulling (Phase 1)**: Native in-app UI and Go daemon backend endpoint (`POST /api/oci/pull`) with preflight disk verification, live SSE progress streaming, task lifecycle management, and curated macOS base image presets.
2. **Helper Guide & In-App Documentation Overhaul (Phase 2)**: Reorganization of `README.md` and the in-app Help tab into an 8-Stage technical guide with interactive Table of Contents, instant search/filtering, 1-click snippet copying, and MDM cloning random serial/MAC requirements.
3. **Interactive First-Run Onboarding Wizard (Phase 3)**: A 5-step guided setup modal, zero-state dashboard hero cards, persistent state tracking (`FirstRunCompleted`), and role-based presets (DevOps / CI, Jamf Admin, QA Tester).

---

## 2. Global Constraints & Architecture Principles

- **Single `main` package**: Backend code resides at the repo root in Go 1.24.
- **Persistent Daemon Model**: All long-running tasks (`tart pull`, `tart clone`) execute asynchronously within the Go daemon's task manager (`m.tasks`). Background execution persists across browser refreshes, disconnects, or multi-tab usage.
- **macOS Exclusivity**: Preset catalogs strictly target Apple Silicon macOS base images. Linux/Ubuntu presets are omitted in favor of custom registry URLs.
- **MDM Randomization Invariant**: Cloned VMs intended for Jamf Pro / MDM enrollment must enforce randomized serial numbers and MAC addresses (`--random-serial`, `--random-mac`).
- **Test Integrity**: Full coverage maintained across `go test ./...` (with `-race`) and `node --test index_ui_test.js`.

---

## 3. Phase 1: Direct OCI Image Pull Subsystem

### 3.1 Backend Architecture (`ocipull.go` & `main.go`)
- **Endpoint**: `POST /api/oci/pull`
- **Request Payload**:
  ```json
  {
    "image": "ghcr.io/cirruslabs/macos-sequoia-base:latest",
    "insecure": false
  }
  ```
- **Validation & Preflight Checks**:
  1. *URI Regex Validation*: Must match `^[a-zA-Z0-9_.-]+(/[a-zA-Z0-9_.-]+)+(:[a-zA-Z0-9_.-]+)?$`. Rejects shell metacharacters (`;`, `&`, `|`, `` ` ``, `$`, `\n`).
  2. *Duplicate Guard*: Returns `409 Conflict` if a pull task for the specified image is already in `running` status.
  3. *Disk Capacity Check*: Evaluates free disk space on `m.storage()`. Returns `507 Insufficient Storage` if free space is under 25 GiB.
- **Execution Pipeline**:
  - Registers task: `t := m.newTask("pull", req.Image)`.
  - Spawns goroutine: `m.runInto(t, args...)`, where `args = ["pull", req.Image]` (appends `--insecure` if configured).
  - Stdout/stderr streams continuously via `taskWriter` to `t.Output` and is broadcast over SSE (`/events`) with 1-second update throttling.
  - On exit code 0, automatically triggers `m.reconcile()` to index the new image and notify all connected clients.
  - On cancellation (`POST /api/task/cancel`), invokes `t.cancel()`, terminating the underlying `tart pull` process.

### 3.2 Frontend UI & Modal (`index.html`)
- **Triggers**:
  - `[ ⇩ Pull OCI Image ]` button in `#ociPanel` header (Dashboard).
  - `[ ⇩ Pull OCI Image ]` action button in VM Management tab.
- **Modal Dialog (`#pullOciModal`)**:
  - **macOS Quick-Select Chips**:
    - `ghcr.io/cirruslabs/macos-tahoe-base:latest` (macOS 26 Tahoe)
    - `ghcr.io/cirruslabs/macos-sequoia-base:latest` (macOS 15 Sequoia)
    - `ghcr.io/cirruslabs/macos-sonoma-base:latest` (macOS 14 Sonoma)
  - **Custom Registry Input**: Monospaced text box with real-time format validation.
  - **Insecure Checkbox**: `Allow untrusted/HTTP registry (--insecure)`.
  - **Live Terminal View**: Dark monospace log console box displaying streaming layer downloads, with `[ Cancel Pull ]` and `[ Run in Background ]` buttons.

---

## 4. Phase 2: Helper Guide & Documentation Overhaul

### 4.1 Information Architecture (`README.md`)
The unified documentation is restructured into 8 sequential stages:
1. **Stage 1: Welcome & Value Proposition** — Apple Silicon Virtualization orchestration, headless daemon, WebUI, and memory pressure safeguards.
2. **Stage 2: 5-Minute Quickstart Guide** — Zero-to-running VM: installer `.pkg` setup, image pull, clone, and screen sharing.
3. **Stage 3: Base Images & OCI Workflow** — Pulling Tahoe 26, Sequoia 15, Sonoma 14, Golden Master template pattern, and virtual disk expansion.
4. **Stage 4: Daily Fleet Operations & Automation Scheduler** — Sequential vs. Random rotation, active working hours windows, headless mode (`--no-graphics`), and audio suppression (`--no-audio`).
5. **Stage 5: Jamf Pro & MDM Administrator Toolkit** — Multi-tenant Jamf configurations, MDM enrollment column, staging profiles, push commands, and the mandatory rule: *Always enable `--random-serial` and `--random-mac` when cloning for MDM enrollment*.
6. **Stage 6: Host Performance, Kernel Safeguards & Hardware Tuning** — 60s host telemetry, Darwin kernel memory pressure deferrals (`memorystatus_vm_pressure_level`), and Go GC memory scavenging.
7. **Stage 7: Automation REST & SSE API Reference** — Directory of all REST endpoints, request/response JSON schemas, and `/events` SSE stream protocol.
8. **Stage 8: Diagnostic Runbooks & Troubleshooting FAQ** — Runbooks for "No IP address after 60s", Guest Agent vs. SSH fallback, VNC connection setup, LaunchAgent troubleshooting, and Jamf device record collision / serial duplication triage.

### 4.2 In-App Interactive Help Viewer (`index.html`)
- **Sticky Table of Contents**: Automatically generated from markdown headings with active section highlighting on scroll.
- **Search & Filter**: Real-time text search bar filtering sections and highlighting matching search terms.
- **1-Click Copy**: Hoverable copy button on all shell blocks, curl commands, and config examples with visual `✓ Copied` feedback.
- **Backend Sync**: Dynamically loaded from `GET /api/readme`.

---

## 5. Phase 3: Interactive First-Run Onboarding Wizard

### 5.1 Configuration & State Persistence (`main.go`)
- **Extended Config Fields**:
  ```go
  type Config struct {
      // ...
      FirstRunCompleted bool   `json:"firstRunCompleted"` // persisted flag for wizard completion
      OperatorRole      string `json:"operatorRole"`      // "devops" | "jamf" | "qa" | "custom"
  }
  ```
- **Trigger Logic**:
  - Automatically pops `#onboardingModal` on initial launch if `!cfg.FirstRunCompleted` and `len(vms) == 0`.
  - Can be manually relaunched anytime via `🚀 Quickstart Wizard` button in the header navigation and Configuration tab.

### 5.2 5-Step Stepper Modal Flow (`#onboardingModal`)
1. **Step 1: Welcome & Environment**: Checks host architecture (`arm64`) and Tart CLI installation; provides 1-click `[ Install Tart CLI ]` button if Tart is missing.
2. **Step 2: Storage & Networking**: Confirms APFS storage path, verifies free disk capacity, and displays LAN binding address.
3. **Step 3: Base Image Selection**: One-click selection for Tahoe 26, Sequoia 15, Sonoma 14, or custom URL. Starts background pull.
4. **Step 4: Operator Role Presets**:
   - **🛠️ DevOps / CI Preset**: Headless enabled (`--no-graphics`), audio disabled (`--no-audio`), sequential scheduler.
   - **🍏 Jamf / Mac Admin Preset**: Random serial + random MAC enabled, Jamf Recon enabled, MDM enrollment column active.
   - **🧪 QA / Interactive Tester Preset**: GUI enabled, audio enabled, screen sharing priority.
5. **Step 5: Review & Fleet Provisioning**: Summary card with 1-click `[ Clone First VM ]` and `[ Go to Fleet Dashboard ]`. Persists `FirstRunCompleted: true`.

### 5.3 Empty-State Dashboard Hero Card
When local VM list is empty:
- Renders an empty-state hero card with quick-action buttons (`[ 🚀 Open Setup Wizard ]`, `[ ⇩ Pull Base Image ]`) and setup guidance.

---

## 6. Verification & Test Plan

1. **Go Unit Tests**:
   - `ocipull_test.go`: OCI URI validation patterns, disk capacity preflight logic, duplicate pull handling, task cancellation.
   - `config_test.go`: Serialization and backward compatibility of `FirstRunCompleted` and `OperatorRole`.
2. **Node.js Frontend Tests (`index_ui_test.js`)**:
   - DOM element validation for `#pullOciModal`, `#onboardingModal`, `#tab-help` TOC, and search bar.
   - Event simulation for preset chip selection, OCI pull modal transitions, copy button interactions, and role preset application.
3. **Packaging & Daemon Verification**:
   - Package build: `SIGN_PKG=false OUT_DIR=. ./packaging/build-pkg.sh`
   - Local upgrade: `sudo installer -pkg "./TartOven-1.40.pkg" -target /`
   - Live end-to-end verification on `http://127.0.0.1:9000/`.
