# MDM Enrollment Column — Design (v1.40)

**Status:** Approved 2026-08-24
**Scope:** Dashboard reporting only. No change to VM lifecycle, scheduling, or MDM deployment.

## Goal

Replace the uninformative `(no Jamf user)` line in the Info column with a real **MDM**
column: a green or red indicator for enrollment, and the Jamf Pro URL beneath it when
enrolled.

## Problem

The shipped `statusCommand` ends with:

```bash
defaults read /Library/Managed\ Preferences/com.jamf.usernamevariable.plist jamfProUsername 2>/dev/null || echo "(no Jamf user)"
```

That prints the literal fallback whenever the read fails for **any** reason, and it is
actively misleading on this fleet. Checked against a running VM:

```
MDM enrollment: Yes (User Approved)
MDM server:     https://emeia.jamfce.com/mdm/ServerURL
/usr/local/bin/jamf   present
```

The VM *is* enrolled and receiving profiles. What is missing is only the
`com.jamf.usernamevariable.plist` domain, which is delivered by a separate custom-settings
profile mapping the `$USERNAME` variable — not by enrollment. So `(no Jamf user)` reads as
"not managed" while the device is managed, and it answers a question nobody asked.

The useful questions are: **is this VM enrolled, and against which server?**

## Design

### Probe

A fixed, built-in probe, deliberately **not** derived from the user-editable
`statusCommand`:

```
/usr/bin/profiles status -type enrollment
```

Verified on a guest: runs as `admin` with no root, exits 0, and emits a fixed two- or
three-line shape. Enrolled hosts print an `MDM server:` line; unenrolled hosts print
`MDM enrollment: No` and omit it entirely. Parsing user-editable free text was rejected
because the parse would break the first time someone edits that box.

Parsing rules:

- **Enrolled** when a line matches `MDM enrollment:` and its value begins with `Yes`.
  The suffix (`(User Approved)`) is retained for the tooltip but does not affect the flag.
- **Server** is the value of the `MDM server:` line, if present.
- Anything else — non-zero exit, unrecognised output, probe failure — is treated as
  **unknown**, not as "not enrolled". Absence of evidence is not evidence of absence, and
  a red light on a healthy VM is worse than a grey one.

### URL displayed

`https://emeia.jamfce.com/mdm/ServerURL` is the MDM check-in endpoint, not a page anyone
opens. The column shows the base URL — `https://emeia.jamfce.com` — produced by trimming a
trailing `/mdm/ServerURL`. The full raw value is preserved in the cell's tooltip so nothing
is hidden. If the suffix is absent, the value is shown unchanged.

### Column

**MDM replaces Notes**, keeping the table at nine columns. Notes is unused across all 36
VMs on this fleet; the field and its editor are untouched, it simply stops occupying a
column. Three states:

| State | Indicator | Second line |
|---|---|---|
| Enrolled | green dot, `Enrolled` | Jamf Pro base URL, tooltip carries the raw value |
| Not enrolled | red dot, `Not enrolled` | — |
| Unknown / not probed | grey dot, `—` | — |

The existing `.ssh-dot` green/red/grey styles are reused rather than a new indicator being
introduced.

### State

Three new persisted `VM` fields, populated wherever Info already is:

```go
MDMEnrolled  bool      `json:"mdmEnrolled,omitempty"`
MDMServer    string    `json:"mdmServer,omitempty"`
MDMCheckedAt time.Time `json:"mdmCheckedAt,omitempty"`
```

`MDMCheckedAt` is what distinguishes "not enrolled" from "never probed" — without it a
fresh VM would show a red light before anything had run.

### Where it runs

Both existing Info sites, so the MDM state is always as fresh as the Info beside it:

1. The post-boot status probe in `doRun`.
2. The `/api/info` handler behind the **Get info** button.

The probe goes through `execInGuest`, so it uses the guest agent when available and SSH
otherwise, exactly like every other guest command. A stopped VM is never probed.

### Retiring the Jamf line

The shipped default `statusCommand` drops the `defaults read … || echo "(no Jamf user)"`
clause, becoming hostname, serial, and macOS version.

Existing installs are migrated on load by **exact match only**: a stored command byte-identical
to the old shipped default is replaced with the new default. Anything customised is left
alone. This is the same conservative shape as the `SSHKey` repair — repair what we recognise,
never overwrite an operator's own text.

## Testing

- Parser: enrolled with `(User Approved)`, enrolled without a suffix, `No`, empty output,
  garbage output, and a missing `MDM server:` line on an otherwise-enrolled response.
- URL trimming: with the `/mdm/ServerURL` suffix, without it, and with a trailing slash.
- Migration: the exact old default is replaced; a customised command is preserved; an
  already-migrated command is left as-is.
- Unknown state renders grey, not red, when `MDMCheckedAt` is zero.
- Dashboard: nine columns with `MDM` where `Notes` was.

## Out of scope

Reading the Jamf username variable (the thing being removed). Making the URL a clickable
link out of the dashboard. Triggering enrollment or unenrollment. Reporting DEP status,
profile inventory, or MDM push certificates. Any change to `/api/vm/mdm-profile`.
