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
	rewritten := rewriteSudoForStdin(guestAgentInstallScript())
	if n := strings.Count(rewritten, "sudo -S -p ''"); n != 1 {
		t.Fatalf("rewritten sudo count = %d, want exactly 1", n)
	}
	if n := strings.Count(rewritten, "sudo "); n != 1 {
		t.Fatalf("total sudo count = %d, want exactly 1", n)
	}
}

func TestInstallScriptRefusesToRunHomebrewAsRoot(t *testing.T) {
	script := guestAgentInstallScript()
	brew := strings.Index(script, "brew install")
	sudo := strings.Index(script, "sudo ")
	if brew < 0 || sudo < 0 {
		t.Fatal("script is missing its brew install or sudo stage")
	}
	if brew > sudo {
		t.Fatal("brew install must run before the sudo block; Homebrew refuses to run as root")
	}
}

func TestLaunchdPlistsTargetTheAgentBinaryAndFlags(t *testing.T) {
	daemon := launchdPlist(guestAgentDaemonLabel, "--run-daemon", true)
	agent := launchdPlist(guestAgentAgentLabel, "--run-agent", false)
	for _, want := range []string{
		"<string>" + guestAgentDaemonLabel + "</string>",
		"/opt/homebrew/bin/tart-guest-agent",
		"--run-daemon",
		"<key>RunAtLoad</key>",
	} {
		if !strings.Contains(daemon, want) {
			t.Errorf("daemon plist missing %q", want)
		}
	}
	// --run-agent carries the RPC server that `tart exec` and the agent IP
	// resolver depend on, so it must be the per-session job.
	if !strings.Contains(agent, "--run-agent") {
		t.Error("agent plist must request --run-agent")
	}
	if strings.Contains(agent, "/var/empty") {
		t.Error("the per-session agent must not use the root daemon's working directory")
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
