package main

import (
	"context"
	"os"
	"strings"
	"testing"
)

// The SSH transport feeds the sudo password once on stdin, and sudo's credential
// cache is not shared between separate calls in a non-interactive session. A second
// sudo in the install script would therefore hang waiting for a password that is
// already consumed, so the single-invocation shape is an invariant, not a style.
func TestInstallScriptUsesExactlyOneSudoInvocation(t *testing.T) {
	rewritten := rewriteSudoForStdin(guestAgentInstallScript("admin"))
	if n := strings.Count(rewritten, "sudo -S -p ''"); n != 1 {
		t.Fatalf("rewritten sudo count = %d, want exactly 1", n)
	}
	if n := strings.Count(rewritten, "sudo "); n != 1 {
		t.Fatalf("total sudo count = %d, want exactly 1", n)
	}
}

func TestInstallScriptRefusesToRunHomebrewAsRoot(t *testing.T) {
	script := guestAgentInstallScript("admin")
	brew := strings.Index(script, "brew install")
	sudo := strings.Index(script, "sudo ")
	if brew < 0 || sudo < 0 {
		t.Fatal("script is missing its brew install or sudo stage")
	}
	if brew > sudo {
		t.Fatal("brew install must run before the sudo block; Homebrew refuses to run as root")
	}
}

func TestLaunchdPlistsMatchTheUpstreamLayout(t *testing.T) {
	daemon := launchdPlist(guestAgentDaemonLabel, "--run-daemon", "/var/empty", "/tmp/tart-guest-daemon.log", false)
	agent := launchdPlist(guestAgentAgentLabel, "--run-agent", "/Users/admin", "/tmp/tart-guest-agent.log", true)

	// Commands run through `tart exec` inherit the agent's environment, so PATH is
	// load-bearing, not cosmetic. Verified against the plists the official Cirrus
	// images actually ship.
	for _, want := range []string{
		"<string>" + guestAgentDaemonLabel + "</string>",
		"/opt/homebrew/bin/tart-guest-agent",
		"--run-daemon",
		"<key>PATH</key>",
		guestAgentPATH,
		"<string>/var/empty</string>",
		"<key>RunAtLoad</key>",
		"<key>KeepAlive</key>",
		"/tmp/tart-guest-daemon.log",
	} {
		if !strings.Contains(daemon, want) {
			t.Errorf("daemon plist missing %q", want)
		}
	}
	for _, want := range []string{
		"--run-agent",
		"<key>PATH</key>",
		guestAgentPATH,
		"<key>TERM</key>",
		"<string>/Users/admin</string>",
		"/tmp/tart-guest-agent.log",
	} {
		if !strings.Contains(agent, want) {
			t.Errorf("agent plist missing %q", want)
		}
	}
	// The per-session agent must not inherit the root daemon's working directory.
	if strings.Contains(agent, "/var/empty") {
		t.Error("agent plist must not use the root daemon working directory")
	}
	// TERM belongs only to the interactive per-session job.
	if strings.Contains(daemon, "TERM") {
		t.Error("daemon plist should not export TERM")
	}
}
func TestLoadDefaultsSSHFallbackOnForUpgradesButPreservesExplicitFalse(t *testing.T) {
	for _, tt := range []struct {
		name string
		body string
		want bool
	}{
		{"state file predating the setting", `{"config":{"listen":"127.0.0.1:9000"},"vms":{},"history":[]}`, true},
		{"explicitly turned off", `{"config":{"listen":"127.0.0.1:9000","sshFallbackEnabled":false},"vms":{},"history":[]}`, false},
		{"explicitly turned on", `{"config":{"listen":"127.0.0.1:9000","sshFallbackEnabled":true},"vms":{},"history":[]}`, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestManager(t)
			if err := os.WriteFile(m.statePath, []byte(tt.body), 0o600); err != nil {
				t.Fatal(err)
			}
			m.load()
			if m.cfg.SSHFallbackEnabled != tt.want {
				t.Fatalf("SSHFallbackEnabled = %v, want %v", m.cfg.SSHFallbackEnabled, tt.want)
			}
		})
	}
}

func TestExecInGuestReportsWhenFallbackIsOffAndAgentIsAbsent(t *testing.T) {
	m := newTestManager(t)
	m.cfg.SSHFallbackEnabled = false
	m.cfg.TartAppPath = "/nonexistent/tart" // forces the agent path to fail to launch
	m.vms["vm1"] = &VM{Name: "vm1", State: "running", IP: "10.0.0.9"}

	res := m.execInGuest(context.Background(), "vm1", "true", "")
	if !strings.Contains(res.Error, "SSH fallback is turned off") {
		t.Fatalf("error = %q, want it to name the disabled fallback", res.Error)
	}
	if m.vms["vm1"].AgentOK {
		t.Fatal("agentOk should be false when the agent did not answer")
	}
}

// A non-interactive SSH session gets PATH=/usr/bin:/bin:/usr/sbin:/sbin, which
// contains no Homebrew prefix. Verified against a real guest: without this export
// the script reports "Homebrew is not installed" on a guest that has it.
func TestInstallScriptRepairsPATHBeforeLookingForHomebrew(t *testing.T) {
	script := guestAgentInstallScript("admin")
	export := strings.Index(script, `export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"`)
	check := strings.Index(script, "command -v brew")
	if export < 0 {
		t.Fatal("script does not repair PATH for a non-interactive session")
	}
	if export > check {
		t.Fatal("PATH must be repaired before the Homebrew check, or the check is meaningless")
	}
}
