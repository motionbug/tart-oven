# Tart Oven - An all-in-one VM management and orchestration server for macOS

A single Go binary that manages, monitors and schedule macOS VMs running
under [Tart](https://tart.run) on a host Mac computer and serves a live web dashboard to
control and monitor them.

This VM orchestration server fully relies on Tart and Apple's virtualization framework.

Current release: **1.29**. See [CHANGELOG.md](CHANGELOG.md) for the complete
Jamf preparation and VM boot-fix notes.

## What it does

- **Scheduler** — Run VMs for a set window following an interval and daily
  working hours. Outside the daily hours the scheduler stops all running VMs and
  starts none. The **Selection mode** is either *random* or *sequential* (cycles
  through the eligible VMs in alphabetical order).
- **SSH status & Info** — on start, after getting an IP, tart-oven runs the
  status command (Get info) over SSH: a green/red bubble shows reachability and
  the **Info** column shows the (multi-line) output. Clicking **Get info**
  refreshes both. Red usually means the key isn't set up.
- **History logs** — every vm run is captured in
  a rolling log for better visibility.
- **Per-VM actions** — Run, Stop, Restart, Send command (SSH), Get info (SSH
  status command, on demand only), and Screen (open macOS Screen Sharing).
- **VM management** — this server detects Tart installations and can automatically install Tart when missing. It also lets you create/clone/edit/delete VMs.
- **Jamf base preparation** — generate an enrollment profile from a saved Jamf
  Pro base URL and invitation ID, then copy and verify it on the Desktop of
  one running base VM over password-authenticated SFTP.

The intended workflow is to clone and start one clean base VM, copy the profile
to its Desktop, stop it without enrolling it, and then clone that prepared base.
Each clone starts with the enrollment profile ready for the operator to install.
See the complete workflow below.

<img width="1617" height="934" alt="Screenshot 2026-06-09 at 11 54 44" src="https://github.com/user-attachments/assets/d6f0a95e-23e1-4d5a-a058-f93906290b62" />

## Jamf base preparation

### What this workflow does

Tart Oven gives you a one-click way to generate a Jamf enrollment profile from
saved settings, copy it to a running VM over SFTP, and verify the remote copy.
It does **not** automate Jamf enrollment. macOS still requires an operator to
open and approve the profile inside each VM.

The prepared-base workflow is:

```text
Clone a clean base VM
→ start it and confirm SSH access
→ copy the profile to the base VM's Desktop
→ stop the base VM without installing the profile
→ clone the prepared base
→ install and approve the profile separately inside each clone
```

### Before you begin

You need:

- Access to Jamf Pro with permission to create a computer enrollment invitation.
- A reusable invitation ID. Select **Allow multiple uses** and choose an expiry
  suitable for the lab. Jamf's invitation wizard may expect an email recipient
  and SMTP configuration, but this workflow does not need to deliver or receive
  the email. You can use a clearly fake recipient and, in a lab, non-delivering
  SMTP settings, provided Jamf lets you complete the invitation.
- Your Jamf Pro base URL, including `https://`, for example
  `https://tenant.jamfcloud.com`. Do not enter `/enroll`, `/enroll/profile`, or
  the complete invitation URL in the base-URL field.
- A running macOS VM on Tart Oven's bridged network with **Remote Login** (SSH)
  enabled and a known username and password.

The published Cirrus Labs images use `admin` as both the username and password,
and those credentials work for GUI and SSH access. A custom image may use
different credentials or require you to enable **System Settings → General →
Sharing → Remote Login** first. See the
[Tart Quick Start](https://tart.run/quick-start/) for the current image list and
SSH details.

### 1. Clone a clean Cirrus base image

Use a current Cirrus Labs base image. For example:

```sh
TART_HOME="/Users/Shared/Tart" \
  tart clone ghcr.io/cirruslabs/macos-tahoe-base:latest jamf-base
```

Use the **VM storage path** configured in Tart Oven as `TART_HOME`; it may be an
external volume instead of `/Users/Shared/Tart`. Tart downloads the remote image
automatically when it is not already local. After cloning, click **Refresh VM
status** in Tart Oven if `jamf-base` is not listed yet.

You can substitute another current Cirrus image, including Sequoia, Sonoma, or
an Xcode image. The [official Tart image instructions](https://tart.run/quick-start/#vm-images)
list the available image names.

### 2. Start the base and confirm SSH

Start `jamf-base` from the Tart Oven Dashboard and wait until it shows as
running with an IP address. Confirm the saved credentials can reach the VM:

```sh
ssh admin@<vm-ip>
```

For a stock Cirrus image, the password is `admin`. Tart Oven assumes that SSH is
enabled; it cannot copy the profile if TCP port 22 is unavailable.

### 3. Create the Jamf enrollment invitation

In Jamf Pro:

1. Open **Computers → Enrollment Invitations** and create a new invitation.
2. Enter a dummy recipient, or your own address if you want the email. Email
   delivery is not used by Tart Oven.
3. Continue through the invitation message screen.
4. Set the expiry and enable **Allow multiple uses** so the invitation can be
   used by VMs cloned from the prepared base.
5. Finish the wizard and copy the invitation ID.

The traditional enrollment link looks like this:

```text
https://tenant.jamfcloud.com/enroll?invitation=INVITATION_ID
```

Enter only `INVITATION_ID` in Tart Oven—not the full URL. The
[Motionbug Jamf invitation guide](https://motionbug.com/enrolling-your-vm-into-jamf-pro-2/)
shows this Jamf Pro workflow with screenshots.

### 4. Copy and verify the profile

In **VM Management → Prepare base VM for Jamf**:

1. Enter the Jamf Pro base URL, such as `https://tenant.jamfcloud.com`.
2. Enter the invitation ID.
3. Enter the base VM's SSH username and password. The defaults for Cirrus
   images are `admin` / `admin`.
4. Click **Save settings**. Password and invitation fields become blank after
   saving; masked dots and “saved” indicate that Tart Oven retained the values.
5. Select the running base VM.
6. Click **Copy profile to Desktop** once.

Tart Oven generates `mdm_enroll.mobileconfig`, uploads it to
`~/Desktop/mdm_enroll.mobileconfig`, reads it back, and verifies both its exact
contents and generated UUID. A successful message displays the VM, path, and
UUID.

### 5. Stop and clone the prepared base

Do **not** install or enroll the base VM. The base is a reusable template whose
Desktop contains the enrollment profile.

1. Stop the base VM from the Dashboard.
2. In **VM Management → Create VMs**, choose **Clone existing VM**.
3. Select the prepared base, choose the number of VMs and their settings, and
   create the clones.
4. Start a clone and open `mdm_enroll.mobileconfig` from its Desktop.
5. Complete the macOS profile approval flow and confirm that the clone appears
   in Jamf Pro.

Repeat the final two steps for each clone. Tart Oven gets the profile onto every
VM with one copy to the base, but macOS/Jamf enrollment itself is not automated.

### What Tart Oven automates

| Tart Oven does | Operator still does |
|---|---|
| Save the base URL, invitation ID, and guest credentials | Create and manage the Jamf invitation |
| Generate and validate the enrollment profile | Ensure SSH is enabled in the guest |
| Copy the profile over embedded SSH/SFTP | Stop the prepared base and create clones |
| Read back and verify the remote file | Open and approve the profile in each clone |

### Troubleshooting

| Message or symptom | Check |
|---|---|
| No VM appears in the selector | The selector intentionally lists running VMs only. Start the base and wait for an IP. |
| `profile configuration is incomplete` | Save a base URL beginning with `https://`, the invitation ID, and SSH credentials. |
| `could not resolve VM IP` | Confirm bridged networking is active and the Dashboard shows an IP. |
| `SSH authentication failed` | Confirm Remote Login is enabled and test the same username/password with `ssh`. |
| `SFTP upload failed` | Confirm SSH/SFTP access, Desktop availability, and that the guest user can write to its Desktop. |
| `uploaded profile verification failed` | Retry once; Tart Oven rejected a remote file that did not exactly match the generated profile. |

Jamf invitation IDs and SSH passwords are stored locally in
`~/.tart-oven/state.json` with owner-only file permissions. They are write-only
in API responses and are not included in profile-transfer logs.

## WebUI tabs

- **Dashboard** — scheduler ON/OFF, a **Refresh VM status** button (force a
  re-check against tart and clear any stuck statuses), the full fleet table
  (with search/filter), and per-VM actions. Last known IP / SSH status / Info
  are retained after a VM stops.
- **VM Management** — create/clone VMs (clone a template, or create from an IPSW
  path / "latest"; pick count, CPU, RAM, disk, display, random MAC/serial), edit
  an existing VM's settings (`tart set`), rename, and delete. Long create/clone
  operations run as background tasks with live output in the **Activity** panel.
  Edit/rename/delete require the VM to be stopped. This replaces the old
  `create_vm_script.sh` Jamf workflow.
- **Configuration** — settings grouped into VM Scheduler; Tart Settings (storage
  paths, Tart binary path, custom run arguments, the storage-mount banner, and an
  **Update Tart** button showing the installed Tart version); SSH & Commands; and
  Server Settings (server label, listen address, history retention, light/dark mode,
  launch at login toggle, and Restart/Stop server buttons). Also has the SSH
  setup guide.
- **Logs** — Tart logs, Activity (create/clone task output), and Run history
  (searchable, 60-day retention by default).
- **Helper Guide** — this README, rendered in-app.

## Build

The application is built from Go source, the embedded `index.html` dashboard,
and supporting profile-transfer files.

```sh
go build -o tart-oven
```

That produces one static binary, `tart-oven`. State and configuration lives in
`~/.tart-oven/state.json` (created on first run).

## First Run (Manual mode)

```sh
cd /Path/To/tart-oven # e.g.: cd /Library/Application\ Support/Tart\ Oven 
./tart-oven                      # uses config from state.json (default 127.0.0.1:9000)
./tart-oven -listen 0.0.0.0:9000 # override the bind address
```

## Packaging as a .pkg (for sharing / Jamf deployment)

A single command builds an installer package:

```sh
./packaging/build-pkg.sh                 # → TartOven-<version>.pkg
```

The .pkg installs:
- `/Library/Application Support/Tart Oven/tart-oven` — the binary
- `/Library/LaunchAgents/com.tartoven.agent.plist` — the auto-start agent

…and its **postinstall** loads the agent in the logged-in user's GUI session and
opens `http://127.0.0.1:9000` in the default browser. Double click on the .PKG file 
or install it with:

```sh
sudo installer -pkg TartOven-<version>.pkg -target /
```

Signing (only needed for double-click installs outside Jamf — Jamf installs as
root and bypasses Gatekeeper):

```sh
SIGN_IDENTITY="Developer ID Installer: Your Name (TEAMID)" ./packaging/build-pkg.sh
# then notarize:  xcrun notarytool submit … && xcrun stapler staple TartOven-*.pkg
```

You can also sign the produced .pkg in **Jamf Composer** (Build As → choose your
signing certificate) instead of using `SIGN_IDENTITY`.

## Access from another machine on the LAN

It binds to `127.0.0.1:9000` by default (localhost only). To reach it from
another machine, set **Listen** to `0.0.0.0:9000` in the Configuration tab (or
`-listen 0.0.0.0:9000`) and restart, then from your MacBook:

```
http://<host-mac-ip>:9000
```

### Screen Sharing

The per-VM **Screen** button opens `vnc://admin@<vm-ip>` — i.e. it launches
macOS Screen Sharing **on the computer viewing the dashboard** (your MacBook),
connecting directly to the bridged VM over the LAN. This is independent of how
the VM was started. The guest must have **Screen Sharing / Remote Management**
enabled in its Sharing settings.

### Bridged VM reports "no IP after 60s"

Version 1.29 fixes a macOS service-context issue where Tart's `arp -an`
subprocess could return empty output beneath the Go LaunchAgent. Older Tart Oven
builds treated that immediate resolver error as a completed boot timeout and
stopped an otherwise healthy VM.

Tart Oven 1.29 reads the native macOS neighbor table directly and matches it to
the MAC address in the VM's Tart configuration. If this message still appears
on 1.29, confirm that the selected bridge interface is active, the guest is
producing network traffic, and **Boot timeout** is long enough for that image.

## HTTP API

| Method | Path | Body / query | Purpose |
|---|---|---|---|
| GET  | `/api/vms`    | — | full state (VMs + config + storage) |
| POST | `/api/run`    | `{name}` | run a VM now |
| POST | `/api/stop`   | `{name}` | stop a VM now |
| POST | `/api/restart`| `{name}` | stop then run |
| POST | `/api/exec`   | `{name,command}` | SSH exec, returns stdout/stderr/exit |
| GET  | `/api/info`   | `?name=` | SSH status command output |
| GET  | `/api/history`| — | run history (newest first) + retention |
| POST | `/api/vm/create` | createReq JSON | clone from template or create from IPSW (async tasks) |
| POST | `/api/vm/set`    | `{name,cpu,memory,diskSize,display,randomMac,randomSerial}` | `tart set` (VM must be stopped) |
| POST | `/api/vm/rename` | `{name,newName}` | `tart rename` (stopped) |
| POST | `/api/vm/delete` | `{name}` | `tart delete` (stopped) |
| POST | `/api/vm/notes`  | `{name,notes,tags}` | update VM notes and tags |
| GET  | `/api/vm/get`    | `?name=` | `tart get --format json` (for the edit form) |
| POST | `/api/vm/mdm-profile` | `{name}` | generate and verify-copy the saved Jamf enrollment profile to one running VM's Desktop |
| POST | `/api/install-tart` | — | download latest tart from GitHub → /Applications |
| POST | `/api/server/restart` | — | re-exec the tart-oven process |
| POST | `/api/server/stop` | — | stop the tart-oven process (boots out the agent) |
| GET/POST | `/api/server/launchagent` | `{enabled}` | get/set launch-at-login state |
| GET/POST | `/api/config` | Config JSON | read / update config |
| GET  | `/events`     | — | SSE state stream |
| GET  | `/`           | — | the dashboard |

Jamf invitation IDs and SSH passwords are write-only settings: they are saved
for profile copying but are absent from API responses.
