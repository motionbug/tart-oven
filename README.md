# Tart Oven - All-in-One macOS & Linux VM Orchestration Platform

A high-performance Go daemon and web orchestration platform for managing, monitoring, and scheduling macOS and Linux virtual machines on Apple Silicon using [Tart](https://tart.run) and Apple's native `Virtualization.framework`.

Current release: **1.50**. [View Changelog](CHANGELOG.md) for full release notes and update details.

---

## Table of Contents

- [Stage 1: Welcome & Value Proposition](#stage-1-welcome--value-proposition)
- [Stage 2: Quickstart 5-Minute Onboarding Guide](#stage-2-quickstart-5-minute-onboarding-guide)
- [Stage 3: Base Image Management & OCI Registry Workflow](#stage-3-base-image-management--oci-registry-workflow)
- [Stage 4: Daily Fleet Operations, Screen Sharing & Automation Scheduler](#stage-4-daily-fleet-operations-screen-sharing--automation-scheduler)
- [Stage 5: Jamf Pro & MDM Administrator Toolkit](#stage-5-jamf-pro--mdm-administrator-toolkit)
- [Stage 6: Host Performance, Kernel Safeguards & Hardware Tuning](#stage-6-host-performance-kernel-safeguards--hardware-tuning)
- [Stage 7: Automation & REST / SSE API Reference](#stage-7-automation--rest--sse-api-reference)
- [Stage 8: Diagnostic Runbooks & Troubleshooting FAQ](#stage-8-diagnostic-runbooks--troubleshooting-faq)

---

## Stage 1: Welcome & Value Proposition

Tart Oven transforms standalone Apple Silicon Macs into robust, self-healing virtualization hosts. By combining a lightweight Go background daemon with an interactive, responsive web console, Tart Oven eliminates the friction of managing virtual machine fleets for CI/CD runners, Mac management testing, and ad-hoc development.

### Core Architecture & Highlights

```
┌──────────────────────────────────────────────────────────────────────────────────────────┐
│                                    Web UI & REST API                                     │
│  ┌───────────────────────┬─────────────────────────┬──────────────────────────────────┐  │
│  │   Fleet Management    │   Automated Scheduler   │   Live Host Telemetry & Charts   │  │
│  └───────────────────────┴─────────────────────────┴──────────────────────────────────┘  │
└────────────────────────────────────────────┬─────────────────────────────────────────────┘
                                             │ HTTP / SSE / REST
┌────────────────────────────────────────────▼─────────────────────────────────────────────┐
│                               Tart Oven Go Daemon (main)                                 │
│  ┌───────────────────────┬─────────────────────────┬──────────────────────────────────┐  │
│  │  Darwin Kernel Gauges │   Tart Process Engine   │   Guest Agent & SSH Subsystem    │  │
│  └───────────────────────┴─────────────────────────┴──────────────────────────────────┘  │
└────────────────────────────────────────────┬─────────────────────────────────────────────┘
                                             │
┌────────────────────────────────────────────▼─────────────────────────────────────────────┐
│                          Apple Silicon Virtualization.framework                          │
│  ┌────────────────────────┐  ┌────────────────────────┐  ┌────────────────────────┐      │
│  │  macOS Tahoe (v26) VM  │  │ macOS Sequoia (v15) VM │  │  macOS Sonoma (v14) VM │      │
│  └────────────────────────┘  └────────────────────────┘  └────────────────────────┘      │
└──────────────────────────────────────────────────────────────────────────────────────────┘
```

- **All-in-One Persistent Daemon**: Single compiled binary embedding the Web UI, API, and static assets. Runs silently as a macOS `LaunchAgent` or interactive server.
- **Automated Fleet Scheduler**: Orchestrates VM lifecycles using configurable working hours windows and rotation policies (*Sequential* round-robin or *Random*), auto-terminating idle instances.
- **Native Host Telemetry & Memory Safeguards**: Gathers host CPU, physical RAM, Darwin kernel memory pressure (`kern.memorystatus_vm_pressure_level`), disk space, and I/O throughput every 60s without process forks. Automatically defers new VM launches under critical memory pressure to guarantee host stability.
- **Guest Agent First with SSH Fallback**: Executes guest commands and gathers reachability metadata via the high-speed Tart guest agent over virtio vsock sockets, seamlessly falling back to SSH with private key authentication when needed.
- **OCI Base Image & Local VM Separation**: Explicit distinction between immutable container registry base images and mutable, runnable local clones.
- **Jamf Pro & MDM Management Suite**: Built-in MDM enrollment status indicators, automated enrollment profile generation, SFTP staging, and hardware identifier randomization.

---

## Stage 2: Quickstart 5-Minute Onboarding Guide

Get Tart Oven up and running with your first bootable macOS virtual machine in less than 5 minutes.

### Step 1: Install Tart Oven

You can install Tart Oven via the prebuilt signed macOS installer package or compile directly from source:

#### Option A: macOS Installer Package (.pkg)
Download `TartOven-1.50.pkg` and install via Terminal or Finder:

```sh
sudo installer -pkg TartOven-1.50.pkg -target /
```

This installs the binary to `/Library/Application Support/Tart Oven/tart-oven` and registers a user `LaunchAgent` at `~/Library/LaunchAgents/io.github.motionbug.tart-oven.plist`.

#### Option B: Build from Source
Ensure Go 1.24+ is installed:

```sh
# Clone repository and build
git clone https://github.com/motionbug/tart-oven.git
cd tart-oven
go build -o tart-oven .

# Run directly
./tart-oven -listen 127.0.0.1:9000
```

### Step 2: Open the Web Console
Navigate to `http://127.0.0.1:9000` in your web browser. If launching for the first time, Tart Oven detects whether Tart CLI is present and guides you through initial storage configuration.

### Step 3: Pull a macOS Base Image
1. In the **OCI Images** panel on the Dashboard, click **⇩ Pull OCI Image**.
2. Select a curated preset (such as **🍏 macOS 15 (Sequoia)** or `ghcr.io/cirruslabs/macos-sequoia-base:latest`).
3. Click **Pull Image**. The live streaming log will track layer downloads.

### Step 4: Clone to a Local VM
1. Once the pull completes, click **Clone** next to the OCI image.
2. Enter a VM name (e.g. `sequoia-runner-01`), choose CPU cores (e.g. `4`), RAM (e.g. `8 GiB`), and disk size (e.g. `50 GiB`).
3. Click **Create Local VM**.

### Step 5: Start & Screen Share
1. On the **Dashboard**, find `sequoia-runner-01` in the **Local VMs** table and click **▶ Run**.
2. Once the VM reports an IP address and green status bubble, click **Screen** to open native macOS Screen Sharing (`vnc://admin@<vm-ip>`).

---

## Stage 3: Base Image Management & OCI Registry Workflow

Tart Oven maintains a strict boundary between cached OCI registry base templates and runnable local virtual machines.

### Curated macOS Base Images

| Operating System | Recommended OCI Image Reference | Default Specs | Agent |
|---|---|---|---|
| **macOS 26 (Tahoe)** | `ghcr.io/cirruslabs/macos-tahoe-base:latest` | 4 vCPU, 8 GB RAM | Preinstalled |
| **macOS 15 (Sequoia)** | `ghcr.io/cirruslabs/macos-sequoia-base:latest` | 4 vCPU, 8 GB RAM | Preinstalled |
| **macOS 14 (Sonoma)** | `ghcr.io/cirruslabs/macos-sonoma-base:latest` | 4 vCPU, 8 GB RAM | Preinstalled |

### The Golden Master Template Pattern

To minimize disk usage and provisioning overhead across your fleet:

1. **Pull Base Image**: Pull a pristine base image from GHCR or your internal OCI registry.
2. **Create Master Template Clone**: Clone the OCI base into a local template VM (e.g. `tmpl-sequoia-qa`).
3. **Customize & Pre-configure**: Boot `tmpl-sequoia-qa`, install Xcode command line tools, developer SDKs, certificates, or tools, and shut it down.
4. **Fast-Clone Fleet Instances**: Clone `tmpl-sequoia-qa` into multiple operational VMs (`qa-01`, `qa-02`, etc.) using APFS copy-on-write cloning.

```sh
# Pull base image via Tart CLI or Web UI
tart pull ghcr.io/cirruslabs/macos-sequoia-base:latest

# Clone base into golden master template
tart clone ghcr.io/cirruslabs/macos-sequoia-base:latest tmpl-sequoia-qa

# Provision operational instance with hardware configuration
tart clone tmpl-sequoia-qa runner-01
tart set runner-01 --cpu 6 --memory 12288 --disk-size 60
```

> [!TIP]
> **APFS Instant Cloning**: On macOS APFS volumes, cloning a 50 GiB VM template takes under 2 seconds and consumes zero initial storage, referencing shared base data until blocks are modified.

---

## Stage 4: Daily Fleet Operations, Screen Sharing & Automation Scheduler

Tart Oven provides granular manual controls alongside an automated background scheduler designed for unattended CI/CD Mac mini and Mac Studio racks.

### Automated Fleet Scheduler

The scheduler runs in a dedicated background goroutine and continuously reconciles desired fleet state:

- **Daily Working Hours Window**: Define active operational hours (e.g. `08:00 - 18:00`). Outside of this window, all running scheduled VMs are automatically shut down to preserve host thermals and power.
- **Run Window & Interval**: Specify maximum VM run duration (e.g. 60 minutes) and cool-down intervals.
- **Selection Modes**:
  - **Sequential (Round-Robin)**: Cycles through the pool of stopped local VMs in orderly rotation.
  - **Random**: Randomly selects a stopped local VM from the eligible pool.
- **OCI Exclude Guard**: `Exclude OCI images from scheduler` is enabled by default to prevent pristine base images from accidentally booting into scheduler rotation.

### Headless Operation & Audio Suppression

For high-density CI runners where GUI rendering is unnecessary:
- **Headless Mode (`--no-graphics`)**: Runs the VM without allocating host display server resources.
- **Audio Suppression (`--no-audio`)**: Disables virtual sound devices to eliminate host CoreAudio daemon overhead.

### Native macOS Screen Sharing

Clicking **Screen** on any running VM opens macOS native Screen Sharing via the `vnc://` URL scheme:
- Default credentials for Cirrus Labs base images: User: `admin`, Password: `admin`.
- Operates over the local bridge network between host and guest.

### Guest Command Execution & Agent Architecture

Tart Oven executes guest operations using a dual-path engine:
1. **Primary: Tart Guest Agent**: Communicates directly through a virtual socket (`virtio-vsock`). High-speed, zero-network dependency, no SSH daemon or SSH credentials required.
2. **Fallback: Guest SSH**: For VMs lacking the agent (e.g. custom IPSW installs), commands execute over SSH using the configured SSH user, password, and private key identity file (`~/.ssh/id_ed25519`).

---

## Stage 5: Jamf Pro & MDM Administrator Toolkit

Tart Oven includes specialized tooling for Apple Device Management (MDM) engineers, QA teams testing Jamf Pro workflows, and Mac Admins staging enrollment profiles.

### MDM Enrollment Status Column

The Dashboard features a dedicated **MDM** status column reporting each guest's live enrollment state:
- 🟢 **Green (Enrolled)**: Displays the connected Jamf Pro / MDM server URL.
- 🔴 **Red (Unenrolled)**: Confirms the guest is running but no active MDM enrollment profile is installed.
- ⚪ **Grey (Unprobed)**: The VM has not been booted or probed yet.

### The Invariant Rule: Mandatory Hardware Randomization for MDM Cloning

> [!IMPORTANT]
> **MANDATORY MDM CLONING RULE**:
> Whenever you clone a base VM or template for Jamf Pro / MDM enrollment testing, you **MUST ALWAYS** enable `--random-serial` and `--random-mac`.
>
> In the CLI:
> ```sh
> # Step 1: Clone the base template
> tart clone base-jamf-template enrolled-vm-01
>
> # Step 2: Randomize hardware serial number and MAC address
> tart set enrolled-vm-01 --random-serial --random-mac
> ```
>
> *Note: In Tart CLI, cloning and randomization are performed in two commands (`tart clone` followed by `tart set --random-serial --random-mac`). In the Tart Oven Web UI and backend API (`POST /api/vm/create`), both steps are performed automatically in one click whenever **Randomize Serial Number** and **Randomize MAC Address** (`randomSerial: true`, `randomMac: true`) are selected.*

#### Why Hardware Randomization is Critical for MDM

Apple MDM frameworks and Jamf Pro uniquely index, bind, and track managed endpoints by hardware **Serial Number** and **MAC Address**:
1. **Serial Number Collision**: If multiple VMs share the same hardware serial number, Jamf Pro treats incoming check-ins as the *same device*. Each check-in overwrites previous inventory records, creating inventory flapping and ghost status reports.
2. **MDM Identity Certificate Collisions**: Enrolling a duplicate serial number revokes or invalidates the APNs push token and SCEP machine certificates of earlier clones.
3. **Network DHCP Lease Clashes**: Duplicate MAC addresses cause DHCP IP assignment collisions on local bridge networks.

### Staging Jamf Enrollment Profiles to Base VM

Tart Oven can automatically stage an enrollment invitation profile onto a base template Desktop:

```
┌────────────────────────┐      ┌────────────────────────┐      ┌────────────────────────┐
│  1. Jamf Pro Server    │ ───► │  2. Tart Oven Daemon   │ ───► │ 3. Base VM Desktop     │
│  Create Multi-Use      │      │  Fetch & Generate      │      │ Upload mdm_enroll      │
│  Enrollment Invitation │      │  mobileconfig Payload  │      │ via SFTP (Do NOT run)  │
└────────────────────────┘      └────────────────────────┘      └────────────────────────┘
                                                                             │
                                                                             ▼
                                                                ┌────────────────────────┐
                                                                │ 4. Clone for Users     │
                                                                │ tart clone             │
                                                                │ tart set               │
                                                                │  --random-serial       │
                                                                │  --random-mac          │
                                                                └────────────────────────┘
```

1. In Jamf Pro, go to **Computers → Enrollment Invitations**, create a multi-use invitation, and copy the `INVITATION_ID`.
2. In Tart Oven under **VM Management → Prepare base VM for Jamf**, enter your Jamf Base URL (`https://tenant.jamfcloud.com`), Invitation ID, and base VM credentials.
3. Click **Copy profile to Desktop**. Tart Oven generates `mdm_enroll.mobileconfig`, uploads it via SFTP, and validates the file signature.
4. **Do not enroll the base template.** Shut down the base VM and clone individual testing instances with `--random-serial` and `--random-mac`.

---

## Stage 6: Host Performance, Kernel Safeguards & Hardware Tuning

Tart Oven is engineered for continuous 24/7 background operation on Apple Silicon host hardware without degrading macOS host stability.

### Real-Time In-Process Telemetry

Host metrics are sampled every 60 seconds using direct Darwin kernel APIs and Cgo system calls, completely bypassing shell sub-processes:
- **Host CPU Utilization**: Direct `host_processor_info()` Mach kernel sampling.
- **Physical Memory & Kernel Pressure**: Evaluates `kern.memorystatus_vm_pressure_level` (Normal, Warning, Critical) and active/wired memory pages.
- **Storage Capacity**: Filesystem `statfs` queries on root `/` and Tart storage volume.
- **Disk I/O Throughput**: Dynamic delta tracking of aggregate disk read/write bytes per second.
- **Telemetry Charts**: 24-hour in-memory rolling time series visualized with high-contrast, theme-aware SVG rendering.

### Darwin Kernel Critical Memory Pressure Start Deferral

When host physical memory is exhausted and macOS triggers **Critical** memory pressure:
1. **New Start Deferral**: Tart Oven immediately blocks all manual and scheduled VM starts.
2. **Clear Feedback**: Attempts to start a VM return HTTP `503 Service Unavailable` with message: `host is under critical memory pressure`.
3. **Non-Disruptive**: Existing running VMs continue operating without interruption.
4. **Auto-Recovery**: As soon as memory pressure subsides to Warning or Normal, the launch gate re-opens automatically.
5. **Memory Scavenging**: Periodically invokes Go runtime memory scavenging (`debug.FreeOSMemory()`) to return unused runtime heap pages back to macOS.

### Hardware Tuning Guidelines for Apple Silicon

| Host Hardware | Max Recommended Concurrent macOS VMs | RAM Allocation per Guest | CPU Allocation per Guest |
|---|---|---|---|
| **Apple M1 / M2 / M3 (16 GB)** | 1 - 2 VMs | 4 GB - 6 GB | 2 - 4 vCPUs |
| **Apple M1 / M2 / M3 Pro (32 GB)** | 2 - 4 VMs | 6 GB - 8 GB | 4 vCPUs |
| **Apple M1 / M2 / M3 / M4 Max (64-128 GB)** | 4 - 8 VMs | 8 GB - 16 GB | 4 - 6 vCPUs |
| **Apple M1 / M2 / M3 Ultra (128-192 GB)** | 8 - 16 VMs | 8 GB - 16 GB | 4 - 8 vCPUs |

---

## Stage 7: Automation & REST / SSE API Reference

Tart Oven exposes a comprehensive JSON REST API and real-time Server-Sent Events (SSE) stream for integration with CI/CD runners, Ansible, Terraform, and custom monitoring scripts.

### Endpoints Overview

| Method | Endpoint | Description | Request Payload | Response Schema |
|---|---|---|---|---|
| `GET` | `/api/vms` | Retrieve full daemon state, VM metadata, and status | None | `{"vms": [...], "ociImages": [...], "tasks": [...]}` |
| `POST` | `/api/run` | Start a stopped virtual machine | `{"name": "vm-name"}` | `{"ok": true}` |
| `POST` | `/api/stop` | Immediate hard stop (`tart stop -t 5`) | `{"name": "vm-name"}` | `{"ok": true}` |
| `POST` | `/api/restart` | Stop and restart a virtual machine | `{"name": "vm-name"}` | `{"ok": true}` |
| `POST` | `/api/exec` | Execute command in guest (agent first, SSH fallback) | `{"name": "vm-name", "command": "uname -a", "sudoPassword": ""}` | `{"stdout": "...", "stderr": "...", "exitCode": 0, "durationMs": 120}` |
| `GET` | `/api/info` | Query guest reachability and hardware details | `?name=vm-name` | `{"stdout": "...", "stderr": "...", "exitCode": 0, "durationMs": 120}` |
| `GET` | `/api/vm/get` | Query VM configuration directly from Tart | `?name=vm-name` | Tart JSON (`{"os": "...", "cpu": 4, "memory": 8192, ...}`) |
| `POST` | `/api/oci/pull` | Pull an OCI base image asynchronously | `{"image": "ghcr.io/...", "insecure": false}` | `{"ok": true, "taskId": "...", "image": "..."}` |
| `POST` | `/api/task/cancel` | Cancel an in-flight background task | `{"id": "task-uuid"}` | `{"ok": true}` |
| `POST` | `/api/vm/create` | Provision new VM from IPSW or clone template | `{"name": "...", "source": "...", "cpu": 4, "memory": 8192, "disk": 50, "randomSerial": true, "randomMac": true}` | `{"ok": true}` |
| `POST` | `/api/vm/set` | Reconfigure VM hardware settings | `{"name": "...", "cpu": 6, "memory": 12288, "disk": 60, "display": "1920x1080", "randomSerial": true, "randomMac": true}` | `{"ok": true}` |
| `POST` | `/api/vm/rename` | Rename a stopped virtual machine | `{"name": "old-name", "newName": "new-name"}` | `{"ok": true}` |
| `POST` | `/api/vm/delete` | Delete a stopped virtual machine | `{"name": "vm-name"}` | `{"ok": true}` |
| `POST` | `/api/vm/notes` | Update VM notes, tags, and SSH overrides | `{"name": "vm-name", "notes": "...", "tags": ["ci"], "sshUser": "admin", "sshPassword": ""}` | `{"ok": true}` |
| `POST` | `/api/vm/install-agent` | Install Tart guest agent into a running VM | `{"name": "vm-name"}` | `{"ok": true}` |
| `POST` | `/api/vm/mdm-profile` | Stage Jamf enrollment profile to guest Desktop | `{"name": "vm-name"}` | `{"ok": true}` |
| `POST` | `/api/vm/clear-boot-failure` | Clear boot failure error flag | `{"name": "vm-name"}` | `{"ok": true}` |
| `POST` | `/api/install-tart` | Download and install Tart CLI on host | None | `{"ok": true}` |
| `POST` | `/api/refresh` | Force immediate reconcile against Tart storage | None | `{"ok": true}` |
| `GET` | `/api/performance` | Query latest host metrics and 24h telemetry history | None | `{"current": {...}, "history": [...]}` |
| `GET` | `/api/history` | Retrieve rolling execution and audit log entries | None | `[{"time": "...", "vm": "...", "action": "..."}]` |
| `GET` | `/api/config` | Read current daemon configuration | None | `{Config Object}` |
| `POST` | `/api/config` | Update daemon configuration | `{Partial Config Object}` | `{"ok": true}` |
| `GET` | `/api/server/launchagent` | Check status of user LaunchAgent | None | `{"installed": true, "path": "..."}` |
| `POST` | `/api/server/launchagent` | Install or uninstall user LaunchAgent | `{"action": "install"}` | `{"ok": true}` |
| `POST` | `/api/server/restart` | Gracefully restart Tart Oven server process | None | `{"ok": true}` |
| `POST` | `/api/server/stop` | Gracefully stop Tart Oven server process | None | `{"ok": true}` |
| `GET` | `/api/readme` | Fetch raw embedded documentation markdown | None | `text/markdown` |
| `GET` | `/api/changelog` | Fetch raw embedded changelog markdown | None | `text/markdown` |
| `GET` | `/events` | Real-time Server-Sent Events (SSE) event stream | None | `text/event-stream` |

### API Usage Examples

#### 1. Start a Virtual Machine
```sh
curl -X POST http://127.0.0.1:9000/api/run \
  -H "Content-Type: application/json" \
  -d '{"name": "sequoia-runner-01"}'
```

#### 2. Execute a Command in Guest
```sh
curl -X POST http://127.0.0.1:9000/api/exec \
  -H "Content-Type: application/json" \
  -d '{"name": "sequoia-runner-01", "command": "sw_vers"}'
```

#### 3. Pull an OCI Base Image
```sh
curl -X POST http://127.0.0.1:9000/api/oci/pull \
  -H "Content-Type: application/json" \
  -d '{"image": "ghcr.io/cirruslabs/macos-sequoia-base:latest", "insecure": false}'
```

#### 4. Stream Real-Time Events
```sh
curl -N http://127.0.0.1:9000/events
```

---

## Stage 8: Diagnostic Runbooks & Troubleshooting FAQ

Step-by-step diagnostic runbooks for resolving common virtualization, network, and management anomalies.

### Runbook 1: Jamf Device Record Collision & Serial Duplication Triage

#### Symptom & Root Cause
Multiple test VMs report the same Jamf Pro Computer Record ID, inventory check-ins overwrite existing device entries, or MDM configuration profiles flap continuously.
- **Root Cause**: Clones were created from a base image without randomizing hardware identifiers, causing duplicate Serial Numbers and MAC addresses in Jamf Pro inventory.

#### Resolution Steps
1. **Identify Duplicate Serial Numbers**:
   In Tart Oven or Terminal, inspect the serial number and hardware details of affected VMs:
   ```sh
   tart get <vm-name>
   # Or view structured JSON details:
   tart get <vm-name> --format json
   ```
2. **Purge Duplicate Computer Records in Jamf Pro**:
   - In Jamf Pro Web Console, navigate to **Computers → Search Inventory**.
   - Search for the duplicate serial number.
   - Delete the colliding computer record to clear stale SCEP certificates and APNs tokens.
3. **Re-clone with Mandatory Randomization Flags**:
   Delete the un-randomized clone and recreate it enforcing `--random-serial` and `--random-mac`:
   ```sh
   tart delete <vm-name>
   tart clone <base-template> <new-vm-name>
   tart set <new-vm-name> --random-serial --random-mac
   ```
4. **Boot and Re-enroll**:
   Start the new VM, verify its randomized serial number (`tart get <new-vm-name>` or in the Tart Oven Web UI), and proceed with MDM enrollment.

---

### Runbook 2: VM Reports "No IP address after 60s" / Bridge DHCP Timeout

#### Symptom
The VM starts in Virtualization.framework, but the Dashboard displays a warning: `No IP address after 60s`.

#### Diagnostic Steps & Resolution
1. **Verify Host Bridge Interface**:
   In **Configuration → Tart Paths & Network**, check the **Bridge Network Interface** setting. Ensure the specified host interface (e.g. `en0` for Wi-Fi or Ethernet) is active and connected to a network with an operational DHCP server.
2. **Check DHCP Pool Capacity**:
   Ensure the local router or DHCP server has available IP leases in the subnet pool.
3. **Adjust Boot Timeout**:
   If guest macOS boots slowly on heavy host workloads, increase **Boot timeout (seconds)** in **Configuration** to `120` or `180`.
4. **Inspect Packet Filter (`pf`) Rules**:
   Ensure host firewall or third-party endpoint security tools (e.g. Little Snitch, LuLu) are not blocking DHCP (`UDP 67/68`) or ARP traffic on virtual bridge interfaces.

---

### Runbook 3: Guest Agent Reachability vs. SSH Fallback Failures

#### Symptom
Status bubble shows Red, or **Send Command** / **Get Info** fails with `guest unreachable`.

#### Diagnostic Steps & Resolution
1. **Determine Execution Path**:
   Check whether the VM row indicates `agent` or `ssh`.
2. **If Using Guest Agent**:
   - Verify that the Tart guest agent is running inside the guest.
   - For custom or vanilla images missing the agent, click **Install Agent** on the VM row or run the installer script via SSH.
3. **If Using Guest SSH Fallback**:
   - Ensure **Remote Login (SSH)** is enabled in the guest under **System Settings → General → Sharing**.
   - Verify that the SSH user (default `admin`) and **SSH Identity File** (e.g. `~/.ssh/id_ed25519`) are configured in **Configuration → SSH & Commands**.
   - Test manual SSH connectivity: `ssh -i ~/.ssh/id_ed25519 admin@<vm-ip>`.

---

### Runbook 4: Screen Sharing (VNC) Connection Errors

#### Symptom
Clicking **Screen** fails to establish a VNC connection or prompts with `Connection failed`.

#### Diagnostic Steps & Resolution
1. **Validate Network Reachability**:
   Ensure the client machine can route to the guest VM's IP address. If accessing Tart Oven from another computer on the LAN, ensure the host daemon's listen address is set to `0.0.0.0:9000` in **Configuration**.
2. **Verify Guest Screen Sharing Service**:
   In the guest VM, ensure **Screen Sharing** or **Remote Management** is enabled in **System Settings → General → Sharing**.
3. **Default Credentials**:
   Standard Cirrus Labs base images use username `admin` and password `admin`.

---

### Runbook 5: LaunchAgent Daemon Management & Permissions

#### Symptom
The Tart Oven daemon does not start automatically on login, or cannot access Tart storage.

#### Diagnostic Steps & Resolution
1. **Inspect LaunchAgent Status**:
   ```sh
   launchctl list | grep tart-oven
   ```
2. **Reload LaunchAgent**:
   ```sh
   launchctl unload ~/Library/LaunchAgents/io.github.motionbug.tart-oven.plist
   launchctl load ~/Library/LaunchAgents/io.github.motionbug.tart-oven.plist
   ```
3. **Review Daemon Logs**:
   Inspect stdout and stderr logs located at:
   ```sh
   tail -n 100 ~/.tart-oven/tart-oven.log
   ```

---

### Runbook 6: Critical Memory Pressure Start Deferral

#### Symptom
Attempting to start a VM fails with: `Deferred: host is under critical memory pressure`.

#### Diagnostic Steps & Resolution
1. **Inspect Performance Tab**:
   Navigate to the **Performance** tab and observe the **Kernel Memory Pressure** card and chart.
2. **Stop Idle VMs**:
   Shut down unused or idle virtual machines to release wired physical memory back to the host.
3. **Adjust VM Memory Allocation**:
   Reduce guest RAM allocations in **VM Management** (e.g. adjust from 16 GB to 8 GB or 4 GB).
4. **Automatic Clearance**:
   Once host memory pressure returns to `Normal` or `Warning`, Tart Oven automatically clears the start deferral gate.
