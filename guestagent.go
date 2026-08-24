package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Upstream launchd identifiers. The guest agent moved to the OpenAI org but kept
// its original org.cirruslabs labels and Homebrew formula naming.
const (
	guestAgentFormula     = "openai/tools/tart-guest-agent"
	guestAgentDaemonLabel = "org.cirruslabs.tart-guest-daemon"
	guestAgentAgentLabel  = "org.cirruslabs.tart-guest-agent"
)

// guestAgentInstallScript installs the Tart guest agent inside a guest and loads
// its two launchd jobs: a root daemon (disk resize) and a per-session agent that
// serves the RPC endpoint `tart exec` and `tart ip --resolver agent` rely on.
//
// Every step needing root is wrapped in a SINGLE sudo invocation on purpose. The
// SSH transport feeds the sudo password once on stdin, and sudo's credential cache
// is not shared between separate calls in a non-interactive session, so a second
// `sudo` in this script would hang or fail. Homebrew must NOT run under sudo.
func guestAgentInstallScript() string {
	daemonPlist := launchdPlist(guestAgentDaemonLabel, "--run-daemon", true)
	agentPlist := launchdPlist(guestAgentAgentLabel, "--run-agent", false)
	return `
set -e
if ! command -v brew >/dev/null 2>&1; then
  echo "Homebrew is not installed in this guest; cannot install the Tart guest agent automatically." >&2
  exit 1
fi
echo "==> brew install ` + guestAgentFormula + `"
brew install ` + guestAgentFormula + `
BIN="$(command -v tart-guest-agent || true)"
if [ -z "$BIN" ]; then
  echo "tart-guest-agent is not on PATH after install." >&2
  exit 1
fi
echo "==> installing launchd jobs (binary at $BIN)"
sudo sh -c '
set -e
cat > /Library/LaunchDaemons/` + guestAgentDaemonLabel + `.plist <<'"'"'PLIST'"'"'
` + daemonPlist + `
PLIST
cat > /Library/LaunchAgents/` + guestAgentAgentLabel + `.plist <<'"'"'PLIST'"'"'
` + agentPlist + `
PLIST
chown root:wheel /Library/LaunchDaemons/` + guestAgentDaemonLabel + `.plist /Library/LaunchAgents/` + guestAgentAgentLabel + `.plist
chmod 0644 /Library/LaunchDaemons/` + guestAgentDaemonLabel + `.plist /Library/LaunchAgents/` + guestAgentAgentLabel + `.plist
launchctl load -w /Library/LaunchDaemons/` + guestAgentDaemonLabel + `.plist 2>/dev/null || true
'
launchctl load -w /Library/LaunchAgents/` + guestAgentAgentLabel + `.plist 2>/dev/null || true
echo "==> installed"
`
}

// launchdPlist renders a launchd job for the guest agent. The Homebrew prefix is
// /opt/homebrew on Apple Silicon, which is the only architecture Tart supports.
func launchdPlist(label, flag string, root bool) string {
	extra := ""
	if root {
		extra = "\t<key>WorkingDirectory</key>\n\t<string>/var/empty</string>\n"
	} else {
		extra = "\t<key>EnvironmentVariables</key>\n\t<dict>\n\t\t<key>TERM</key>\n\t\t<string>xterm-256color</string>\n\t</dict>\n"
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>/opt/homebrew/bin/tart-guest-agent</string>
		<string>%s</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
%s</dict>
</plist>`, label, flag, extra)
}

// installGuestAgent installs the Tart guest agent into a running VM over SSH, then
// verifies the result by asking the agent itself to run a command. SSH is the only
// possible transport here: the agent is precisely what is missing, so it cannot
// bootstrap itself.
func (m *Manager) installGuestAgent(name string) {
	t := m.newTask("agent-install", name)
	m.broadcast()

	m.mu.Lock()
	vm := m.vms[name]
	var vmCopy VM
	if vm != nil {
		vmCopy = *vm
	}
	cfg := m.cfg
	m.mu.Unlock()

	if vm == nil || vmCopy.State != "running" {
		m.finishTask(t, errors.New("the VM must be running to install the guest agent"))
		m.broadcast()
		return
	}
	_, password := effectiveSSHCredentials(cfg, &vmCopy)
	if strings.TrimSpace(password) == "" {
		m.finishTask(t, errors.New("a guest SSH password is required to install the agent; set one in Configuration"))
		m.broadcast()
		return
	}

	m.appendTaskOutput(t, "Installing the Tart guest agent on "+name+" over SSH.\n")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	res := m.sshExecContext(ctx, name, guestAgentInstallScript(), password)
	if out := strings.TrimSpace(res.Stdout); out != "" {
		m.appendTaskOutput(t, out+"\n")
	}
	if errText := strings.TrimSpace(res.Stderr); errText != "" {
		m.appendTaskOutput(t, errText+"\n")
	}
	if res.Error != "" {
		m.finishTask(t, errors.New(res.Error))
		m.broadcast()
		return
	}
	if res.ExitCode != 0 {
		m.finishTask(t, fmt.Errorf("install script exited %d", res.ExitCode))
		m.broadcast()
		return
	}

	// Verify through the agent itself rather than trusting the install output.
	m.appendTaskOutput(t, "==> verifying the agent responds\n")
	verifyCtx, cancelVerify := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancelVerify()
	if _, handled := m.execViaAgent(verifyCtx, name, "true", ""); !handled {
		m.setAgentOK(name, false)
		m.finishTask(t, errors.New("the agent was installed but did not respond; a reboot of the guest may be required"))
		m.broadcast()
		return
	}
	m.setAgentOK(name, true)
	m.appendTaskOutput(t, "The guest agent is responding. Commands for this VM no longer use SSH.\n")
	m.finishTask(t, nil)
	m.broadcast()
}
