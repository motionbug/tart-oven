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

Tart Oven maintains a strict separation between immutable OCI registry base images and mutable, runnable local virtual machines.

### Curated macOS Base Images

| Operating System | Recommended OCI Image Reference | Default Specs | Agent |
|---|---|---|---|
| **macOS 26 (Tahoe)** | `ghcr.io/cirruslabs/macos-tahoe-base:latest` | 4 vCPU, 4 GB RAM | Preinstalled |
| **macOS 15 (Sequoia)** | `ghcr.io/cirruslabs/macos-sequoia-base:latest` | 4 vCPU, 4 GB RAM | Preinstalled |
| **macOS 14 (Sonoma)** | `ghcr.io/cirruslabs/macos-sonoma-base:latest` | 4 vCPU, 4 GB RAM | Preinstalled |

### Guest Command Execution & Agent Architecture

Tart Oven communicates with guest virtual machines using a robust dual-path subsystem:

1. **Primary — Tart Guest Agent (`virtio-vsock`)**:
   - Official Cirrus Labs base images come with `tart-guest-agent` pre-installed and running out of the box.
   - Communicates directly over high-speed virtual sockets (`virtio-vsock`), requiring zero network IP configuration, zero SSH credentials, and no SSH daemon startup delays.
   - Powers instant **Get Info**, hostname/serial queries, and automated shell command execution.
2. **Fallback — Guest SSH Subsystem**:
   - For custom IPSW installations, older macOS releases, or Linux distributions lacking the guest agent, Tart Oven automatically falls back to SSH.
   - Uses configured credentials (default user: `admin`, password: `admin`, key: `~/.ssh/tart-oven` or `~/.ssh/id_ed25519`).
   - Operators can install the official guest agent onto any custom VM in one click using Tart Oven's **Install Guest Agent** button in VM Management.

### The Golden Master Template Pattern (Tart Oven Workflow)

To maximize provisioning speed, preserve disk space, and streamline MDM testing across your fleet:

```
┌─────────────────────────┐       ┌─────────────────────────┐       ┌─────────────────────────┐
│ 1. Pull OCI Base Image  │ ───►  │ 2. Create Master Clone  │ ───►  │ 3. Pre-Configure & Stage│
│    Tahoe / Sequoia /    │       │    Clone base into      │       │    Setup SSH keys, apps,│
│    Sonoma via Web UI    │       │    Golden Master VM     │       │    stage MDM profile    │
└─────────────────────────┘       └─────────────────────────┘       └─────────────────────────┘
                                                                                 │
                                                                                 │ (Do NOT enroll template!)
                                                                                 ▼
┌─────────────────────────┐       ┌─────────────────────────┐       ┌─────────────────────────┐
│ 5. Distinct MDM Records │ ◄───  │ 5. Boot Clones & Enroll │ ◄───  │ 4. Fast APFS Batch Clone│
│    Each VM registers    │       │    Run staged profile;  │       │    Clone to N fleet VMs │
│    as a unique device   │       │    unique Serial & MAC  │       │    with Random Serial   │
└─────────────────────────┘       └─────────────────────────┘       │    & MAC flags enabled  │
                                                                    └─────────────────────────┘
```

#### Step-by-Step Golden Master Workflow:

1. **Pull Base Image via Tart Oven**:
   - In the **Dashboard** or **VM Management**, click **`[ ⇩ Pull OCI Image ]`** (or select a preset chip: Tahoe 26, Sequoia 15, Sonoma 14). Tart Oven downloads and extracts the image asynchronously with live SSE progress logs.
2. **Clone into Master Template**:
   - In Tart Oven's VM table, click **Clone** next to the base image to create your local template VM (e.g. `tmpl-sequoia-master` or `tmpl-jamf-master`).
3. **Pre-Configure Credentials & Stage MDM Enrollment Profile**:
   - Boot the template VM via the Tart Oven dashboard and connect via **Screen Sharing** (`vnc://admin@<vm-ip>`) or SSH.
   - Verify SSH access and credentials (`admin` user).
   - Use Tart Oven's built-in **Jamf Pro / MDM Staging Tool** (`VM Management ➜ Prepare base VM for Jamf`) to generate and upload `mdm_enroll.mobileconfig` directly onto the template Desktop via SFTP.
   - Install required developer tools, SDKs, or root certificates.
   - **Crucial**: **Do NOT enroll the master template.** Shut down the template VM cleanly.
4. **Fast-Clone Fleet Instances with Hardware Randomization**:
   - In Tart Oven, clone the template into multiple operational fleet VMs (`runner-01`, `runner-02`, etc.) using APFS copy-on-write cloning.
   - Ensure **Randomize Serial Number** (`randomSerial: true`) and **Randomize MAC Address** (`randomMac: true`) are checked. Tart Oven orchestrates both cloning and hardware randomization in one operation.
5. **Instant Independent Enrollment on Boot**:
   - As each cloned instance boots, it possesses a completely unique hardware serial number and MAC address.
   - Run the staged enrollment profile. Each clone registers in Jamf Pro / MDM as a completely distinct device record with zero collisions.

#### CLI Equivalence (for Terminal Automation Scripts):

```sh
# 1. Pull base image
tart pull ghcr.io/cirruslabs/macos-sequoia-base:latest

# 2. Clone base into golden master template
tart clone ghcr.io/cirruslabs/macos-sequoia-base:latest tmpl-sequoia-master

# 3. Boot template, pre-configure credentials & stage MDM profile, then shut down
# (Or use Tart Oven Web UI to copy profile via SFTP automatically)

# 4. Provision operational clones with hardware customization and randomization
tart clone tmpl-sequoia-master runner-01
tart set runner-01 --cpu 4 --memory 8192 --disk-size 50 --random-serial --random-mac
```

> [!TIP]
> **APFS Copy-on-Write Speed**: On Apple Silicon APFS storage, cloning a 50 GiB master template takes under 2 seconds and consumes zero initial storage, referencing shared base blocks until modified by the guest.

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

---

## Stage 5: Jamf Pro & MDM Administrator Toolkit

Tart Oven includes specialized tooling for Apple Device Management (MDM) engineers, QA teams testing Jamf Pro workflows, and Mac Admins staging enrollment profiles.

### MDM Enrollment Status Column

The Dashboard features a dedicated **MDM** status column reporting each guest's live enrollment state:
- 🟢 **Green (Enrolled)**: Displays the connected Jamf Pro / MDM server URL.
- 🔴 **Red (Unenrolled)**: Confirms the guest is running but no active MDM enrollment profile is installed.
- ⚪ **Grey (Unprobed)**: The VM has not been booted or probed yet.

### Mandatory Hardware Randomization for MDM Cloning

> [!IMPORTANT]
> ### Invariant Rule: Always Randomize Serial Number & MAC Address When Cloning for MDM
> Whenever you clone a base VM or template for Jamf Pro / MDM enrollment testing, you **MUST ALWAYS** enable `--random-serial` and `--random-mac`.
>
> #### In Tart Oven Web UI & Backend API:
> Tart Oven automates this seamlessly. In the **Clone VM** or **Batch Clone** modal, simply ensure **Randomize Serial Number** and **Randomize MAC Address** (`randomSerial: true`, `randomMac: true`) are checked. Tart Oven automatically executes the complete two-step provisioning sequence (`tart clone` followed by `tart set --random-serial --random-mac`) per cloned instance in the background.
>
> #### In the Tart CLI:
> In the CLI, cloning and randomization are performed in two separate sequential commands:
> ```sh
> # Step 1: Clone the base template
> tart clone tmpl-jamf-master enrolled-vm-01
>
> # Step 2: Randomize hardware serial number and MAC address
> tart set enrolled-vm-01 --random-serial --random-mac
> ```

#### Why Hardware Randomization is Critical for Jamf Pro & MDM

Apple MDM protocols and Jamf Pro uniquely index, bind, and track managed macOS endpoints by hardware **Serial Number**, **Hardware UUID**, and **MAC Address**:

1. **Jamf Device Record Collisions & Inventory Flapping**:
   - If multiple VMs share the same hardware serial number, Jamf Pro treats incoming check-ins from different VMs as the *exact same computer record*.
   - Each VM's inventory update overwrites the previous VM's record (swapping IP addresses, installed applications, and computer names back and forth).
2. **MDM Identity Certificate & APNs Invalidation**:
   - During enrollment, Jamf Pro issues a unique SCEP machine certificate and APNs push token tied to the hardware serial.
   - Enrolling a second VM with an identical serial immediately revokes or invalidates the APNs token and MDM identity certificates of all prior clones, breaking remote management commands (`recon`, `jamf policy`, config profile pushes).
3. **Bridge Network DHCP Lease Clashes**:
   - Duplicate MAC addresses cause DHCP IP assignment collisions and ARP poisoning on local bridge networks.

### Staging Jamf Enrollment Profiles to Base VM

Tart Oven automates staging multi-use enrollment invitation profiles directly onto template VMs:

1. In Jamf Pro, navigate to **Computers → Enrollment Invitations**, create a multi-use invitation, and copy the `INVITATION_ID`.
2. In Tart Oven under **VM Management → Prepare base VM for Jamf**, enter your Jamf Base URL (`https://tenant.jamfcloud.com`), Invitation ID, and base VM credentials.
3. Click **Copy profile to Desktop**. Tart Oven fetches `mdm_enroll.mobileconfig`, uploads it via SFTP, and validates the file signature.
4. **Do not enroll the master template.** Shut down the base VM and clone individual testing instances with **Randomize Serial Number** and **Randomize MAC Address** enabled.

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

### Hardware Tuning & Virtualization Constraints

Apple's native `Virtualization.framework` enforces a hard architectural limit of **at most 2 concurrent macOS virtual machines** simultaneously on a single macOS host, regardless of host chip tier (M1, M2, M3, M4, Max, or Ultra).

#### System Requirements
- **Host Processor**: Apple Silicon (M1 or newer)
- **Host Memory**: Minimum 8 GB Unified Memory (16 GB+ recommended for running 2 concurrent VMs smoothly)
- **Disk Storage**: APFS-formatted SSD volume with at least 25–50 GB free space for base images and copy-on-write clones
- **Max Concurrent macOS Guests**: **2 VMs simultaneously** (enforced by Apple `Virtualization.framework`)

#### Recommended Guest Sizing
- **Single Active Guest**: 4–8 vCPUs, 4–8 GB RAM
- **Two Concurrent Guests**: 2–4 vCPUs, 4–6 GB RAM per guest

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
