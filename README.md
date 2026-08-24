# Tart Oven - An all-in-one VM management and orchestration server for macOS

A single Go binary that manages, monitors, and schedules macOS and Linux virtual machines running
under [Tart](https://tart.run) on Apple Silicon Mac computers, serving a live web dashboard to
control and monitor them.

This VM orchestration server fully relies on Tart and Apple's native `Virtualization.framework`.

Current release: **1.36**. [View Changelog](CHANGELOG.md) for full release notes and update details.

---

## 1. What Tart Oven Does

- **Automated VM Scheduler** — Run virtual machines for a configurable time window according to intervals and daily working hours. Outside working hours, Tart Oven automatically shuts down running VMs. Selection modes include *Random* and *Sequential* (round-robin). Cached OCI images are excluded from scheduling by default so only runnable local clones are scheduled.
- **Full VM Lifecycle Controls** — Run, Stop (fast direct hard stop), Suspend, Restart, Send command (SSH), Get info (SSH status check), and Screen (direct macOS Screen Sharing).
- **Local VM & OCI Image Separation** — The Dashboard clearly separates runnable local VMs from cached OCI registry base images, carrying Tart's source and storage metadata through the UI and API.
- **Native Host Performance Monitoring** — Inspect real-time host CPU usage, physical RAM, Darwin kernel memory pressure, disk capacity, and I/O throughput alongside 24 hours of in-memory charts.
- **Kernel Memory Safeguards** — Automatically defer new VM starts while macOS reports critical memory pressure (`kern.memorystatus_vm_pressure_level`), preventing host instability. Periodically scavenges unused Go heap pages back to macOS.
- **VM Management** — Detects local Tart installations (with one-click auto-install if missing), creates new VMs from IPSW images or clones existing templates, and reconfigures CPU, RAM, disk, display, MAC, and serial numbers.
- **Execution & Audit History** — Every VM run, boot probe, and background task is recorded in a searchable rolling log with 60-day default retention.

---

## 2. Local VMs and OCI Base Images

Tart's storage contains two distinct kinds of items:

- **Local VMs**: Independent, bootable virtual machines stored in `TART_HOME`. You can run, stop, suspend, inspect, edit, rename, and delete these machines.
- **OCI Images**: Cached container registry base images (e.g. `ghcr.io/cirruslabs/macos-tahoe-base:latest`). Tart Oven displays their repository reference, size, virtual disk size, and last-accessed timestamp. Their primary Dashboard action is **Clone**.

### Cloning an OCI Image to a Local VM
1. In the **OCI Images** section of the Dashboard, click **Clone** next to an image.
2. Tart Oven opens **VM Management** with the image reference pre-selected.
3. Choose a name and hardware specifications (CPU, RAM, Disk) for your new local VM and click **Create**.
4. Once cloned, the new VM appears under **Local VMs** ready to be started and scheduled.

> [!TIP]
> **Exclude OCI images from scheduler** is enabled by default under **Configuration → VM Scheduler**. Keep this enabled so your base templates remain untouched while cloned local VMs are scheduled.

---

## 3. The Core Tart Oven Workflow

```
[ 1. Clone or Create VM ] ──► [ 2. Configure Hardware & SSH ] ──► [ 3. Start VM / Enable Scheduler ] ──► [ 4. Screen Share or Run Tasks ] ──► [ 5. Suspend or Stop ]
```

### 1. Provisioning a Virtual Machine
* **From an OCI Base Image**: Use the **Clone** button on any cached OCI entry or enter an OCI image reference in **VM Management**.
* **From a macOS IPSW**: In **VM Management → Create VMs**, select **Create from IPSW**, choose your macOS version ("latest" or local file path), and configure disk and memory.

### 2. Configuring Guest SSH Access
To enable automated status checks and remote commands:
1. Boot the VM and ensure **System Settings → General → Sharing → Remote Login (SSH)** is enabled in the guest.
2. In Tart Oven under **Configuration → SSH & Commands**, verify the default SSH user and password (defaults for stock Cirrus images are `admin` / `admin`).
3. When the VM boots and receives an IP, the SSH status bubble in the Dashboard turns **green** and populates the **Info** column.

### 3. Running & Scheduling VMs
* **Manual Control**: Click **Run** on any stopped VM in the Dashboard.
* **Automated Scheduling**: In **Configuration → VM Scheduler**, set your desired daily working hours (e.g. `09:00 - 17:00`), run window duration, and interval. Click **Turn Scheduler ON** on the Dashboard.

### 4. Interacting with Running VMs
* **Screen Sharing**: Click **Screen** in the VM's action menu to open native macOS Screen Sharing (`vnc://admin@<vm-ip>`) directly from your browser.
* **Ad-Hoc SSH Commands**: Use the **SSH command** bar on the Dashboard to execute commands across running VMs with optional `sudo` support.

---

## 4. WebUI Tabs Overview

- **Dashboard** — Scheduler master switch, **Refresh VM status** button, separated Local VMs and OCI Images tables with search and filter controls, and per-VM action buttons.
- **Performance** — Live host health cards (CPU, RAM, Kernel Pressure, Disks, Uptime) and interactive 24-hour historical telemetry charts.
- **VM Management** — VM creation (IPSW install or OCI/template cloning), hardware editing (`tart set`), renaming, and deletion. Background task output is streamed live to the **Activity** panel.
- **Configuration** — Centralized settings for the VM Scheduler, Tart runtime & storage paths, SSH timeouts, network listen address, light/dark theme, and auto-start LaunchAgent.
- **Logs** — Rolling system logs, background Activity task output, and searchable VM run history.
- **Helper Guide** — Interactive, in-app documentation and user guide.

---

## 5. Host Performance & Memory Safeguards

Tart Oven monitors the host Mac's resources in real time to prevent runaway resource exhaustion.

### Real-Time Telemetry
Tart Oven samples native host metrics every 60 seconds without invoking shell sub-processes or causing Stop-The-World (STW) runtime pauses:
- Real host CPU utilization percentage.
- Physical RAM used and Darwin kernel memory pressure level.
- Storage capacity on the system disk (`/`) and Tart VM storage volume.
- Aggregate host disk read and write I/O throughput.
- Host system uptime.

### Critical-Pressure Start Deferral
When the Darwin kernel reports **Critical** memory pressure (`kern.memorystatus_vm_pressure_level`), Tart Oven automatically defers new manual and scheduled VM starts until pressure subsides to Warning or Normal:
- Existing running VMs continue uninterrupted.
- The scheduler continues stopping VMs whose normal runtime has elapsed.
- Manual start attempts display a clear explanation: `host is under critical memory pressure`.
- As soon as host pressure drops, the start gate clears automatically.

### Suspend vs. Fast Hard Stop
- **Suspend**: Saves machine state and releases host memory. Requires `--suspendable` in **Configuration → Custom run arguments** before starting the VM. Suspended VMs are protected from accidental deletion or modification until resumed.
- **Stop (Fast Direct)**: Executes `tart stop -t 5` directly, terminating the VM within seconds and releasing host resources immediately with process kill fallback if needed.

---

## 6. Multi-Machine Network Access (LAN)

By default, Tart Oven binds to `127.0.0.1:9000` (accessible only from the host Mac).

To access the web dashboard from another Mac or PC on your local network:
1. In **Configuration → Server Settings**, change **Listen Address** to `0.0.0.0:9000`.
2. Click **Restart Server**.
3. Open `http://<host-mac-ip>:9000` in your web browser.

> [!NOTE]
> The **Screen** button connects your client Mac to the guest VM over the local network via VNC. Both the client and the VM must be on reachable subnets.

---

## 7. Installation, Upgrades & Deployment

### Install or Upgrade to 1.34
Double-click `TartOven-1.34.pkg` or run from Terminal:

```sh
sudo installer -pkg ~/Downloads/TartOven-1.34.pkg -target /
```

This updates the binary at `/Library/Application Support/Tart Oven/tart-oven`, restarts the LaunchAgent, and preserves all existing configuration in `~/.tart-oven/state.json`.

### Building from Source & Packaging

```sh
# 1. Build standalone binary
go build -o tart-oven .

# 2. Build & sign macOS installer package (.pkg)
# Automatically signs with detected Developer ID and outputs to ~/Downloads/TartOven-1.34.pkg
./packaging/build-pkg.sh
```

---

## 8. HTTP REST API Reference

| Method | Path | Payload | Description |
|---|---|---|---|
| GET | `/api/vms` | — | Full daemon state snapshot, VM metadata, and status |
| POST | `/api/run` | `{"name": "vm-name"}` | Start a VM |
| POST | `/api/stop` | `{"name": "vm-name"}` | Fast hard stop a VM (`tart stop -t 5`) |
| POST | `/api/suspend` | `{"name": "vm-name"}` | Suspend a running VM |
| POST | `/api/restart` | `{"name": "vm-name"}` | Restart a VM |
| POST | `/api/exec` | `{"name": "vm", "command": "cmd"}` | Execute remote SSH command |
| GET | `/api/info` | `?name=vm-name` | Query guest SSH status and system info |
| GET | `/api/performance` | — | Latest host telemetry sample & 24h history |
| GET | `/api/history` | — | Execution history logs |
| POST | `/api/vm/create` | Create JSON | Provision or clone a new VM |
| POST | `/api/vm/set` | Hardware JSON | Reconfigure CPU, memory, disk, or display |
| POST | `/api/vm/rename` | `{"name": "a", "newName": "b"}` | Rename a stopped VM |
| POST | `/api/vm/delete` | `{"name": "vm-name"}` | Delete a stopped VM |
| GET/POST | `/api/config` | Config JSON | Read or update daemon configuration |
| GET | `/api/changelog` | — | Read raw markdown release notes |
| GET | `/events` | — | Real-time Server-Sent Events (SSE) stream |

---

## 9. Optional Integrations: Jamf Pro MDM Base Preparation

> [!NOTE]
> This is an **optional add-on workflow** designed for lab administrators who use Jamf Pro to manage test VMs. If you do not use Jamf Pro, you can skip this section entirely.

### Overview
Tart Oven provides a one-click helper to generate an Apple MDM enrollment profile from your Jamf Pro server and upload it to a base VM over SFTP. 

```
[ 1. Clone Clean Base ] ──► [ 2. Start Base & Confirm SSH ] ──► [ 3. Copy Profile to Desktop ] ──► [ 4. Stop Base (Do NOT Enroll) ] ──► [ 5. Clone Base for Users ]
```

> [!WARNING]
> **Do not approve or enroll the base VM itself.**
> The base VM serves as a reusable template. Each cloned VM will inherit the profile on its Desktop for separate enrollment and unique Jamf device registration.

### Step-by-Step Setup
1. **Create Enrollment Invitation in Jamf Pro**:
   - In Jamf Pro, go to **Computers → Enrollment Invitations**.
   - Create an invitation with **Allow multiple uses** enabled. Copy the resulting `INVITATION_ID`.
2. **Save Settings in Tart Oven**:
   - In Tart Oven under **VM Management → Prepare base VM for Jamf**, enter your **Jamf Base URL** (e.g. `https://tenant.jamfcloud.com`), the **Invitation ID**, and guest SSH credentials.
   - Click **Save settings**.
3. **Copy Profile to Base VM**:
   - Start your base VM (e.g. `jamf-base`).
   - Select the running base VM and click **Copy profile to Desktop**.
   - Tart Oven generates `mdm_enroll.mobileconfig`, uploads it via SFTP, and cryptographically verifies the file on the guest Desktop.
4. **Stop & Clone**:
   - Stop `jamf-base`.
   - Clone `jamf-base` whenever you need a fresh test VM with the enrollment profile pre-staged on the Desktop.

---

## 10. Troubleshooting & FAQ

| Message or Symptom | Explanation & Resolution |
|---|---|
| **VM reports "No IP after 60s"** | The VM started but has not emitted DHCP traffic on the bridge interface. Verify that the configured bridge interface is active, or increase **Boot timeout** under Configuration. |
| **SSH bubble is Red / Get Info fails** | SSH connection could not be established. Ensure Remote Login is enabled in guest macOS Sharing settings and verify credentials in Configuration. |
| **"Deferred: host is under critical memory pressure"** | Host RAM is exhausted. Tart Oven paused new starts to prevent a kernel crash. Stop idle VMs or reduce guest RAM allocations in VM Management. |
| **Suspend reports VM is not suspendable** | Add `--suspendable` to **Configuration → Custom run arguments**, then restart the VM from a clean stopped state. |
